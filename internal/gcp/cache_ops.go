package gcp

import (
	"sync"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
)

var (
	cacheOnce      sync.Once
	sharedCache    *cache.Cache
	cacheInitError error
)

// getSharedCache returns the process-wide cache instance, creating it once on demand.
func getSharedCache() (*cache.Cache, error) {
	if !cache.Enabled() {
		return nil, nil
	}

	cacheOnce.Do(func() {
		if !cache.Enabled() {
			sharedCache = nil
			cacheInitError = nil

			return
		}

		sharedCache, cacheInitError = cache.New()
	})

	if !cache.Enabled() {
		return nil, nil
	}

	return sharedCache, cacheInitError
}

// LoadCache loads and returns the cache instance.
func LoadCache() (*cache.Cache, error) {
	if !cache.Enabled() {
		return nil, nil
	}

	return getSharedCache()
}

// cacheInstance stores instance location information in the cache while preserving
// any previously saved user preferences (like the IAP flag).
func (c *Client) cacheInstance(instanceName string, instance *Instance) {
	if c == nil || c.cache == nil || instance == nil {
		return
	}

	info := &cache.LocationInfo{
		Project: c.project,
		Zone:    instance.Zone,
		Type:    cache.ResourceTypeInstance,
	}

	// Use GetWithProject for precise lookup by name+project to preserve IAP preference
	if stored, found := c.cache.GetWithProject(instanceName, c.project); found && stored != nil && stored.IAP != nil {
		info.IAP = stored.IAP
	}

	if err := c.cache.Set(instanceName, info); err != nil {
		logger.Log.Warnf("Failed to cache instance location: %v", err)
	}
}

// cacheMIG stores MIG location information in the cache.
func (c *Client) cacheMIG(migName string, instance *Instance, isRegional bool) {
	info := &cache.LocationInfo{
		Project:    c.project,
		Type:       cache.ResourceTypeMIG,
		IsRegional: isRegional,
	}

	if isRegional {
		info.Region = extractRegionFromZone(instance.Zone)
	} else {
		info.Zone = instance.Zone
	}

	if err := c.cache.Set(migName, info); err != nil {
		logger.Log.Warnf("Failed to cache MIG location: %v", err)
	}
}

func (c *Client) cacheMIGFromRefs(migName string, refs []ManagedInstanceRef, isRegional bool) {
	if c.cache == nil || len(refs) == 0 {
		return
	}

	c.cacheMIG(migName, &Instance{
		Name: refs[0].Name,
		Zone: refs[0].Zone,
	}, isRegional)
}
