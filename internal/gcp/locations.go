package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/kedare/compass/internal/logger"
	"google.golang.org/api/compute/v1"
)

// ListRegions returns the available regions in the project.
func (c *Client) ListRegions(ctx context.Context) ([]string, error) {
	if c.cache != nil {
		if zones, ok := c.cache.GetZones(c.project); ok && len(zones) > 0 {
			regionSet := make(map[string]struct{})
			for _, zone := range zones {
				region := extractRegionFromZone(zone)
				if region == "" {
					continue
				}

				regionSet[region] = struct{}{}
			}

			regions := make([]string, 0, len(regionSet))
			for region := range regionSet {
				regions = append(regions, region)
			}

			sort.Strings(regions)

			logger.Log.Debugf("Derived %d regions from cached zones", len(regions))

			return regions, nil
		}
	}

	regions, err := c.listRegions(ctx)
	if err != nil {
		return nil, err
	}

	return regions, nil
}

// ListZones returns the available zones in the project.
func (c *Client) ListZones(ctx context.Context) ([]string, error) {
	logger.Log.Debug("Listing compute zones")

	if c.cache != nil {
		if zones, ok := c.cache.GetZones(c.project); ok {
			logger.Log.Debugf("Using %d zones from cache", len(zones))

			return zones, nil
		}
	}

	items, err := collectZones(func(pageToken string) ([]*compute.Zone, string, error) {
		call := c.service.Zones.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list zones: %v", err)

			return nil, "", fmt.Errorf("failed to list zones: %w", err)
		}

		return resp.Items, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(items))
	for _, zone := range items {
		if zone == nil {
			continue
		}

		if strings.ToUpper(zone.Status) != "UP" {
			continue
		}

		names = append(names, zone.Name)
	}

	sort.Strings(names)

	logger.Log.Debugf("Found %d zones", len(names))

	if c.cache != nil {
		if err := c.cache.SetZones(c.project, names); err != nil {
			logger.Log.Warnf("Failed to cache zones for project %s: %v", c.project, err)
		}
	}

	return names, nil
}

func (c *Client) listRegions(ctx context.Context) ([]string, error) {
	logger.Log.Debug("Listing available regions")

	regions, err := c.service.Regions.List(c.project).Context(ctx).Do()
	if err != nil {
		logger.Log.Errorf("Failed to list regions: %v", err)

		return nil, err
	}

	var regionNames []string

	for _, region := range regions.Items {
		if region.Status == "UP" {
			regionNames = append(regionNames, region.Name)
			logger.Log.Tracef("Available region: %s", region.Name)
		}
	}

	sort.Strings(regionNames)

	logger.Log.Debugf("Found %d available regions", len(regionNames))

	return regionNames, nil
}

func (c *Client) isRegion(location string) bool {
	// Simple heuristic: regions typically have format like "us-central1", "europe-west2"
	// zones have format like "us-central1-a", "europe-west2-b"
	parts := strings.Split(location, "-")

	return len(parts) == 2 || (len(parts) == 3 && len(parts[2]) > 1)
}
