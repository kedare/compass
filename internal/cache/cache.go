// Package cache provides persistent caching for GCP resource locations
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"compass/internal/logger"
)

const (
	// CacheFileName is the name of the cache file.
	CacheFileName = ".compass.cache.json"
	// CacheExpiry defines how long cache entries are valid (30 days).
	CacheExpiry = 30 * 24 * time.Hour
	// CacheFilePermissions defines the file permissions for the cache file (owner read/write only).
	CacheFilePermissions = 0o600
)

// ResourceType represents the type of GCP resource.
type ResourceType string

const (
	ResourceTypeInstance ResourceType = "instance"
	ResourceTypeMIG      ResourceType = "mig"
)

// LocationInfo stores location information for a GCP resource.
type LocationInfo struct {
	Timestamp  time.Time    `json:"timestamp"`
	Project    string       `json:"project"`
	Zone       string       `json:"zone,omitempty"`
	Region     string       `json:"region,omitempty"`
	Type       ResourceType `json:"type"`
	IsRegional bool         `json:"is_regional,omitempty"`
}

// Cache manages persistent storage of resource location information.
type Cache struct {
	data     map[string]*LocationInfo
	filePath string
	mu       sync.RWMutex
}

// New creates a new cache instance.
func New() (*Cache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Log.Errorf("Failed to get user home directory: %v", err)

		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	filePath := filepath.Join(homeDir, CacheFileName)
	logger.Log.Debugf("Cache file path: %s", filePath)

	cache := &Cache{
		filePath: filePath,
		data:     make(map[string]*LocationInfo),
	}

	// Load existing cache if it exists
	if err := cache.load(); err != nil {
		logger.Log.Warnf("Failed to load cache, starting with empty cache: %v", err)
		// Continue with empty cache instead of returning error
	}

	return cache, nil
}

// Get retrieves location information from the cache.
func (c *Cache) Get(resourceName string) (*LocationInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, exists := c.data[resourceName]
	if !exists {
		logger.Log.Debugf("Cache miss for resource: %s", resourceName)

		return nil, false
	}

	// Check if cache entry has expired
	if time.Since(info.Timestamp) > CacheExpiry {
		logger.Log.Debugf("Cache entry expired for resource: %s (age: %v)", resourceName, time.Since(info.Timestamp))

		return nil, false
	}

	logger.Log.Debugf("Cache hit for resource: %s (project: %s, zone: %s, region: %s)",
		resourceName, info.Project, info.Zone, info.Region)

	return info, true
}

// Set stores location information in the cache and persists it.
func (c *Cache) Set(resourceName string, info *LocationInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	info.Timestamp = time.Now()
	c.data[resourceName] = info

	logger.Log.Debugf("Caching location for resource: %s (project: %s, zone: %s, region: %s)",
		resourceName, info.Project, info.Zone, info.Region)

	// Persist to disk
	return c.save()
}

// Delete removes a resource from the cache.
func (c *Cache) Delete(resourceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, resourceName)
	logger.Log.Debugf("Deleted resource from cache: %s", resourceName)

	return c.save()
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]*LocationInfo)

	logger.Log.Debug("Cleared all cache entries")

	return c.save()
}

// load reads the cache from disk.
func (c *Cache) load() error {
	data, err := os.ReadFile(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Log.Debug("Cache file does not exist, starting with empty cache")

			return nil
		}

		return fmt.Errorf("failed to read cache file: %w", err)
	}

	if len(data) == 0 {
		logger.Log.Debug("Cache file is empty, starting with empty cache")

		return nil
	}

	if err := json.Unmarshal(data, &c.data); err != nil {
		return fmt.Errorf("failed to unmarshal cache data: %w", err)
	}

	logger.Log.Debugf("Loaded cache with %d entries", len(c.data))

	return nil
}

// save writes the cache to disk.
func (c *Cache) save() error {
	// Clean expired entries before saving
	c.cleanExpired()

	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	if err := os.WriteFile(c.filePath, data, CacheFilePermissions); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	logger.Log.Debugf("Saved cache with %d entries to %s", len(c.data), c.filePath)

	return nil
}

// cleanExpired removes expired entries from the cache (must be called with lock held).
func (c *Cache) cleanExpired() {
	count := 0

	for name, info := range c.data {
		if time.Since(info.Timestamp) > CacheExpiry {
			delete(c.data, name)

			count++
		}
	}

	if count > 0 {
		logger.Log.Debugf("Cleaned %d expired cache entries", count)
	}
}
