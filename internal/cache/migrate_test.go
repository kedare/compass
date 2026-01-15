package cache

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func TestImportFromJSON_ValidData(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, OldCacheFileName)
	dbPath := filepath.Join(tmpDir, CacheFileName)

	// Create valid JSON cache file
	jsonData := legacyCacheFile{
		GCP: legacyGCPSection{
			Instances: map[string]*legacyLocationInfo{
				"test-instance": {
					Timestamp: time.Now(),
					Project:   "test-project",
					Zone:      "us-central1-a",
					Type:      "instance",
				},
				"test-mig": {
					Timestamp:  time.Now(),
					Project:    "test-project",
					Region:     "us-central1",
					Type:       "mig",
					IsRegional: true,
				},
			},
			Zones: map[string]*legacyZoneListing{
				"test-project": {
					Timestamp: time.Now(),
					Zones:     []string{"us-central1-a", "us-central1-b"},
				},
			},
			Projects: map[string]*legacyProjectEntry{
				"test-project": {
					Timestamp: time.Now(),
				},
			},
			Subnets: map[string]*legacySubnetEntry{
				"test-project|test-network|test-subnet": {
					Timestamp:   time.Now(),
					Project:     "test-project",
					Network:     "test-network",
					Region:      "us-central1",
					Name:        "test-subnet",
					PrimaryCIDR: "10.0.0.0/24",
					SecondaryRanges: []legacySubnetSecondaryRange{
						{Name: "secondary", CIDR: "10.1.0.0/24"},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(jsonData, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(jsonPath, data, 0o600)
	require.NoError(t, err)

	// Open database
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = initSchema(db)
	require.NoError(t, err)

	// Run migration
	migrated, err := importFromJSON(db, jsonPath)
	require.NoError(t, err)
	require.True(t, migrated)

	// Verify instances were imported
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM instances`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Verify specific instance
	var project, zone, resourceType string
	err = db.QueryRow(`SELECT project, zone, type FROM instances WHERE name = ?`, "test-instance").
		Scan(&project, &zone, &resourceType)
	require.NoError(t, err)
	require.Equal(t, "test-project", project)
	require.Equal(t, "us-central1-a", zone)
	require.Equal(t, "instance", resourceType)

	// Verify zones were imported
	err = db.QueryRow(`SELECT COUNT(*) FROM zones`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify projects were imported
	err = db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify subnets were imported
	err = db.QueryRow(`SELECT COUNT(*) FROM subnets`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify JSON file was deleted
	_, err = os.Stat(jsonPath)
	require.True(t, os.IsNotExist(err))
}

func TestImportFromJSON_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "non_existent.json")
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = initSchema(db)
	require.NoError(t, err)

	migrated, err := importFromJSON(db, jsonPath)
	require.NoError(t, err)
	require.False(t, migrated)
}

func TestImportFromJSON_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, OldCacheFileName)
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create empty file
	err := os.WriteFile(jsonPath, []byte(""), 0o600)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = initSchema(db)
	require.NoError(t, err)

	migrated, err := importFromJSON(db, jsonPath)
	require.NoError(t, err)
	require.False(t, migrated)
}

func TestImportFromJSON_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, OldCacheFileName)
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create invalid JSON file
	err := os.WriteFile(jsonPath, []byte("{invalid json}"), 0o600)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = initSchema(db)
	require.NoError(t, err)

	migrated, err := importFromJSON(db, jsonPath)
	require.Error(t, err)
	require.False(t, migrated)
	require.Contains(t, err.Error(), "failed to parse JSON cache file")
}

func TestImportFromJSON_IAPPreservation(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, OldCacheFileName)
	dbPath := filepath.Join(tmpDir, CacheFileName)

	iapTrue := true
	iapFalse := false

	jsonData := legacyCacheFile{
		GCP: legacyGCPSection{
			Instances: map[string]*legacyLocationInfo{
				"iap-true": {
					Timestamp: time.Now(),
					Project:   "test-project",
					Zone:      "us-central1-a",
					Type:      "instance",
					IAP:       &iapTrue,
				},
				"iap-false": {
					Timestamp: time.Now(),
					Project:   "test-project",
					Zone:      "us-central1-b",
					Type:      "instance",
					IAP:       &iapFalse,
				},
				"iap-nil": {
					Timestamp: time.Now(),
					Project:   "test-project",
					Zone:      "us-central1-c",
					Type:      "instance",
					IAP:       nil,
				},
			},
		},
	}

	data, err := json.MarshalIndent(jsonData, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(jsonPath, data, 0o600)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = initSchema(db)
	require.NoError(t, err)

	migrated, err := importFromJSON(db, jsonPath)
	require.NoError(t, err)
	require.True(t, migrated)

	// Verify IAP values
	var iap *int

	err = db.QueryRow(`SELECT iap FROM instances WHERE name = ?`, "iap-true").Scan(&iap)
	require.NoError(t, err)
	require.NotNil(t, iap)
	require.Equal(t, 1, *iap)

	err = db.QueryRow(`SELECT iap FROM instances WHERE name = ?`, "iap-false").Scan(&iap)
	require.NoError(t, err)
	require.NotNil(t, iap)
	require.Equal(t, 0, *iap)

	err = db.QueryRow(`SELECT iap FROM instances WHERE name = ?`, "iap-nil").Scan(&iap)
	require.NoError(t, err)
	require.Nil(t, iap)
}

func TestImportFromJSON_DeduplicatesSubnets(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, OldCacheFileName)
	dbPath := filepath.Join(tmpDir, CacheFileName)

	// The old JSON format had multiple keys pointing to the same subnet
	// We need to deduplicate them
	jsonData := legacyCacheFile{
		GCP: legacyGCPSection{
			Subnets: map[string]*legacySubnetEntry{
				"proj|net|subnet": {
					Timestamp:   time.Now(),
					Project:     "proj",
					Network:     "net",
					Region:      "us-central1",
					Name:        "subnet",
					PrimaryCIDR: "10.0.0.0/24",
				},
				"proj|us-central1|subnet": { // Same subnet, different key
					Timestamp:   time.Now(),
					Project:     "proj",
					Network:     "net",
					Region:      "us-central1",
					Name:        "subnet",
					PrimaryCIDR: "10.0.0.0/24",
				},
			},
		},
	}

	data, err := json.MarshalIndent(jsonData, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(jsonPath, data, 0o600)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = initSchema(db)
	require.NoError(t, err)

	migrated, err := importFromJSON(db, jsonPath)
	require.NoError(t, err)
	require.True(t, migrated)

	// Should only have 1 subnet (deduplicated)
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM subnets`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestFullMigrationFlow(t *testing.T) {
	tmpDir := t.TempDir()

	// Temporarily override home directory
	originalHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { _ = os.Setenv("HOME", originalHome) }()

	// Create JSON cache file in the temp "home" directory
	jsonPath := filepath.Join(tmpDir, OldCacheFileName)

	jsonData := legacyCacheFile{
		GCP: legacyGCPSection{
			Instances: map[string]*legacyLocationInfo{
				"test-instance": {
					Timestamp: time.Now(),
					Project:   "test-project",
					Zone:      "us-central1-a",
					Type:      "instance",
				},
			},
			Projects: map[string]*legacyProjectEntry{
				"test-project": {
					Timestamp: time.Now(),
				},
			},
		},
	}

	data, err := json.MarshalIndent(jsonData, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(jsonPath, data, 0o600)
	require.NoError(t, err)

	// Create cache using New() - should auto-migrate
	cache, err := New()
	require.NoError(t, err)
	defer func() { _ = cache.Close() }()

	// Verify data was migrated
	retrieved, found := cache.Get("test-instance")
	require.True(t, found)
	require.Equal(t, "test-project", retrieved.Project)

	// Verify JSON file was deleted
	_, err = os.Stat(jsonPath)
	require.True(t, os.IsNotExist(err))

	// Verify SQLite file was created in cache directory
	dbPath := filepath.Join(tmpDir, CacheDir, CacheFileName)
	_, err = os.Stat(dbPath)
	require.NoError(t, err)
}
