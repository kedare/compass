package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/kedare/compass/internal/logger"
)

// legacyCacheFile represents the old JSON cache file structure.
type legacyCacheFile struct {
	GCP legacyGCPSection `json:"gcp"`
}

type legacyGCPSection struct {
	Instances map[string]*legacyLocationInfo `json:"instances"`
	Zones     map[string]*legacyZoneListing  `json:"zones"`
	Projects  map[string]*legacyProjectEntry `json:"projects"`
	Subnets   map[string]*legacySubnetEntry  `json:"subnets"`
}

type legacyLocationInfo struct {
	Timestamp  time.Time `json:"timestamp"`
	Project    string    `json:"project"`
	Zone       string    `json:"zone,omitempty"`
	Region     string    `json:"region,omitempty"`
	Type       string    `json:"type"`
	IsRegional bool      `json:"is_regional,omitempty"`
	IAP        *bool     `json:"iap,omitempty"`
}

type legacyZoneListing struct {
	Timestamp time.Time `json:"timestamp"`
	Zones     []string  `json:"zones"`
}

type legacyProjectEntry struct {
	Timestamp time.Time `json:"timestamp"`
}

type legacySubnetEntry struct {
	Timestamp       time.Time                    `json:"timestamp"`
	Project         string                       `json:"project"`
	Network         string                       `json:"network"`
	Region          string                       `json:"region"`
	Name            string                       `json:"name"`
	SelfLink        string                       `json:"self_link,omitempty"`
	PrimaryCIDR     string                       `json:"primary_cidr,omitempty"`
	SecondaryRanges []legacySubnetSecondaryRange `json:"secondary_ranges,omitempty"`
	IPv6CIDR        string                       `json:"ipv6_cidr,omitempty"`
	Gateway         string                       `json:"gateway,omitempty"`
}

type legacySubnetSecondaryRange struct {
	Name string `json:"name,omitempty"`
	CIDR string `json:"cidr"`
}

// importFromJSON imports data from a legacy JSON cache file into SQLite.
// Returns true if migration was performed, false if no JSON file existed.
func importFromJSON(db *sql.DB, jsonPath string) (bool, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to read JSON cache file: %w", err)
	}

	if len(data) == 0 {
		logger.Log.Debug("JSON cache file is empty, nothing to migrate")

		return false, nil
	}

	var fileData legacyCacheFile
	if err := json.Unmarshal(data, &fileData); err != nil {
		return false, fmt.Errorf("failed to parse JSON cache file: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Import instances
	instanceCount := 0

	for name, info := range fileData.GCP.Instances {
		if info == nil {
			continue
		}

		var iap *int
		if info.IAP != nil {
			val := 0
			if *info.IAP {
				val = 1
			}

			iap = &val
		}

		isRegional := 0
		if info.IsRegional {
			isRegional = 1
		}

		query := `INSERT OR REPLACE INTO instances (name, timestamp, project, zone, region, type, is_regional, iap)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		logSQL(query, name, info.Timestamp.Unix(), info.Project, info.Zone, info.Region, info.Type, isRegional, iap)

		_, err = tx.Exec(query,
			name, info.Timestamp.Unix(), info.Project, info.Zone, info.Region, info.Type, isRegional, iap,
		)
		if err != nil {
			return false, fmt.Errorf("failed to import instance %s: %w", name, err)
		}

		instanceCount++
	}

	// Import zones
	zoneCount := 0

	for project, listing := range fileData.GCP.Zones {
		if listing == nil {
			continue
		}

		zonesJSON, err := json.Marshal(listing.Zones)
		if err != nil {
			return false, fmt.Errorf("failed to marshal zones for project %s: %w", project, err)
		}

		query := `INSERT OR REPLACE INTO zones (project, timestamp, zones_json) VALUES (?, ?, ?)`
		logSQL(query, project, listing.Timestamp.Unix(), string(zonesJSON))

		_, err = tx.Exec(query, project, listing.Timestamp.Unix(), string(zonesJSON))
		if err != nil {
			return false, fmt.Errorf("failed to import zones for project %s: %w", project, err)
		}

		zoneCount++
	}

	// Import projects
	projectCount := 0

	for name, entry := range fileData.GCP.Projects {
		if entry == nil {
			continue
		}

		query := `INSERT OR REPLACE INTO projects (name, timestamp) VALUES (?, ?)`
		logSQL(query, name, entry.Timestamp.Unix())

		_, err = tx.Exec(query, name, entry.Timestamp.Unix())
		if err != nil {
			return false, fmt.Errorf("failed to import project %s: %w", name, err)
		}

		projectCount++
	}

	// Import subnets (deduplicate by unique constraint)
	subnetCount := 0
	seenSubnets := make(map[string]bool)

	for _, entry := range fileData.GCP.Subnets {
		if entry == nil {
			continue
		}

		// Create unique key to avoid duplicates
		key := fmt.Sprintf("%s|%s|%s", entry.Project, entry.Network, entry.Name)
		if seenSubnets[key] {
			continue
		}

		seenSubnets[key] = true

		secondaryJSON, err := json.Marshal(entry.SecondaryRanges)
		if err != nil {
			return false, fmt.Errorf("failed to marshal secondary ranges for subnet %s: %w", entry.Name, err)
		}

		query := `INSERT OR REPLACE INTO subnets
			 (timestamp, project, network, region, name, self_link, primary_cidr, secondary_ranges_json, ipv6_cidr, gateway)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		logSQL(query, entry.Timestamp.Unix(), entry.Project, entry.Network, entry.Region, entry.Name,
			entry.SelfLink, entry.PrimaryCIDR, string(secondaryJSON), entry.IPv6CIDR, entry.Gateway)

		_, err = tx.Exec(query,
			entry.Timestamp.Unix(), entry.Project, entry.Network, entry.Region, entry.Name,
			entry.SelfLink, entry.PrimaryCIDR, string(secondaryJSON), entry.IPv6CIDR, entry.Gateway,
		)
		if err != nil {
			return false, fmt.Errorf("failed to import subnet %s: %w", entry.Name, err)
		}

		subnetCount++
	}

	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	logger.Log.Debugf("Migrated cache from JSON: %d instances, %d zone listings, %d projects, %d subnets",
		instanceCount, zoneCount, projectCount, subnetCount)

	// Remove old JSON file after successful migration
	if err := os.Remove(jsonPath); err != nil {
		logger.Log.Warnf("Failed to remove old JSON cache file: %v", err)
		// Don't return error, migration was successful
	} else {
		logger.Log.Debug("Removed old JSON cache file")
	}

	return true, nil
}
