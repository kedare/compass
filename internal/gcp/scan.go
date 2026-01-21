package gcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"google.golang.org/api/compute/v1"
)

// ScanStats holds statistics about a resource scan operation.
type ScanStats struct {
	Zones     int
	Instances int
	MIGs      int
	Subnets   int
}

// ScanProgress is called to report progress during scanning.
type ScanProgress func(resource string, count int)

// ScanProjectResources scans all resources in a project and caches them.
// This includes zones, instances, and subnets.
func (c *Client) ScanProjectResources(ctx context.Context, progress ScanProgress) (*ScanStats, error) {
	if c == nil || c.service == nil {
		return nil, fmt.Errorf("client is not initialized")
	}

	stats := &ScanStats{}

	// Scan zones
	zones, err := c.scanZones(ctx)
	if err != nil {
		logger.Log.Warnf("Failed to scan zones for project %s: %v", c.project, err)
	} else {
		stats.Zones = len(zones)
		if progress != nil {
			progress("zones", stats.Zones)
		}
	}

	// Scan instances
	instances, err := c.scanInstances(ctx)
	if err != nil {
		logger.Log.Warnf("Failed to scan instances for project %s: %v", c.project, err)
	} else {
		stats.Instances = instances
		if progress != nil {
			progress("instances", stats.Instances)
		}
	}

	// Scan MIGs
	migs, err := c.scanMIGs(ctx)
	if err != nil {
		logger.Log.Warnf("Failed to scan MIGs for project %s: %v", c.project, err)
	} else {
		stats.MIGs = migs
		if progress != nil {
			progress("migs", stats.MIGs)
		}
	}

	// Scan subnets
	subnets, err := c.scanSubnets(ctx)
	if err != nil {
		logger.Log.Warnf("Failed to scan subnets for project %s: %v", c.project, err)
	} else {
		stats.Subnets = subnets
		if progress != nil {
			progress("subnets", stats.Subnets)
		}
	}

	return stats, nil
}

// scanZones lists all zones and caches them.
func (c *Client) scanZones(ctx context.Context) ([]string, error) {
	// ListZones already handles caching
	return c.ListZones(ctx)
}

// scanInstances lists all instances and caches them.
func (c *Client) scanInstances(ctx context.Context) (int, error) {
	if c.cache == nil {
		return 0, nil
	}

	instances, err := c.ListInstances(ctx, "")
	if err != nil {
		return 0, err
	}

	// Batch cache all instances
	entries := make(map[string]*cache.LocationInfo, len(instances))
	for _, inst := range instances {
		if inst == nil {
			continue
		}

		entries[inst.Name] = &cache.LocationInfo{
			Project: c.project,
			Zone:    inst.Zone,
			Type:    cache.ResourceTypeInstance,
		}
	}

	if len(entries) > 0 {
		if err := c.cache.SetBatch(entries); err != nil {
			logger.Log.Warnf("Failed to batch cache instances: %v", err)
		}
	}

	return len(instances), nil
}

// scanMIGs lists all managed instance groups and caches them.
func (c *Client) scanMIGs(ctx context.Context) (int, error) {
	if c.cache == nil {
		return 0, nil
	}

	migs, err := c.ListManagedInstanceGroups(ctx, "")
	if err != nil {
		return 0, err
	}

	// Batch cache all MIGs
	entries := make(map[string]*cache.LocationInfo, len(migs))
	for _, mig := range migs {
		// Use region for regional MIGs, zone for zonal MIGs
		location := mig.Location
		entries[mig.Name] = &cache.LocationInfo{
			Project:    c.project,
			Zone:       location, // Zone field is used for both zone and region
			Region:     location,
			Type:       cache.ResourceTypeMIG,
			IsRegional: mig.IsRegional,
		}
	}

	if len(entries) > 0 {
		if err := c.cache.SetBatch(entries); err != nil {
			logger.Log.Warnf("Failed to batch cache MIGs: %v", err)
		}
	}

	return len(migs), nil
}

// scanSubnets lists all subnets and caches them.
func (c *Client) scanSubnets(ctx context.Context) (int, error) {
	if c.cache == nil {
		return 0, nil
	}

	call := c.service.Subnetworks.AggregatedList(c.project).Context(ctx)

	var entries []*cache.SubnetEntry
	err := call.Pages(ctx, func(page *SubnetworkAggregatedList) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		for _, scopedList := range page.Items {
			if scopedList.Subnetworks == nil {
				continue
			}

			for _, subnet := range scopedList.Subnetworks {
				if subnet == nil {
					continue
				}

				secondary := make([]cache.SubnetSecondaryRange, 0, len(subnet.SecondaryIpRanges))
				for _, sr := range subnet.SecondaryIpRanges {
					if sr == nil {
						continue
					}

					cidr := strings.TrimSpace(sr.IpCidrRange)
					if cidr == "" {
						continue
					}

					secondary = append(secondary, cache.SubnetSecondaryRange{
						Name: strings.TrimSpace(sr.RangeName),
						CIDR: cidr,
					})
				}

				entries = append(entries, &cache.SubnetEntry{
					Project:         c.project,
					Network:         lastComponent(subnet.Network),
					Region:          lastComponent(subnet.Region),
					Name:            subnet.Name,
					SelfLink:        strings.TrimSpace(subnet.SelfLink),
					PrimaryCIDR:     strings.TrimSpace(subnet.IpCidrRange),
					SecondaryRanges: secondary,
					IPv6CIDR:        strings.TrimSpace(subnet.Ipv6CidrRange),
					Gateway:         strings.TrimSpace(subnet.GatewayAddress),
				})
			}
		}

		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to list subnets: %w", err)
	}

	if len(entries) > 0 {
		if err := c.cache.RememberSubnetBatch(entries); err != nil {
			logger.Log.Warnf("Failed to batch cache subnets: %v", err)
		}
	}

	return len(entries), nil
}

// SubnetworkAggregatedList is an alias for the compute API type.
type SubnetworkAggregatedList = compute.SubnetworkAggregatedList
