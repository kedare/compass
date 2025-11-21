// Package cache provides persistent caching for GCP resource locations
package cache

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kedare/compass/internal/logger"
)

const (
	// CacheFileName is the name of the cache file.
	CacheFileName = ".compass.cache.json"
	// CacheExpiry defines how long cache entries are valid (30 days).
	CacheExpiry = 30 * 24 * time.Hour
	// CacheFilePermissions defines the file permissions for the cache file (owner read/write only).
	CacheFilePermissions = 0o600
	// ZoneCacheTTL defines how long zone listings stay valid (30 days).
	ZoneCacheTTL = CacheExpiry
	// SubnetCacheTTL defines how long subnet entries stay valid.
	SubnetCacheTTL = CacheExpiry
)

// ResourceType represents the type of GCP resource.
type ResourceType string

const (
	ResourceTypeInstance ResourceType = "instance"
	ResourceTypeMIG      ResourceType = "mig"
)

// LocationInfo stores the last access details for a GCP resource location.
type LocationInfo struct {
	Timestamp  time.Time    `json:"timestamp"`
	Project    string       `json:"project"`
	Zone       string       `json:"zone,omitempty"`
	Region     string       `json:"region,omitempty"`
	Type       ResourceType `json:"type"`
	IsRegional bool         `json:"is_regional,omitempty"`
	// IAP stores the persisted user preference for using IAP tunneling with this instance.
	IAP *bool `json:"iap,omitempty"`
}

// SubnetSecondaryRange captures details about a secondary IP range attached to a subnet.
type SubnetSecondaryRange struct {
	Name string `json:"name,omitempty"`
	CIDR string `json:"cidr"`
}

// SubnetEntry stores metadata about a VPC subnet for quick IP matching.
type SubnetEntry struct {
	Timestamp       time.Time              `json:"timestamp"`
	Project         string                 `json:"project"`
	Network         string                 `json:"network"`
	Region          string                 `json:"region"`
	Name            string                 `json:"name"`
	SelfLink        string                 `json:"self_link,omitempty"`
	PrimaryCIDR     string                 `json:"primary_cidr,omitempty"`
	SecondaryRanges []SubnetSecondaryRange `json:"secondary_ranges,omitempty"`
	IPv6CIDR        string                 `json:"ipv6_cidr,omitempty"`
	Gateway         string                 `json:"gateway,omitempty"`
}

// clone returns a deep copy of the subnet entry to avoid sharing mutable slices.
func (s *SubnetEntry) clone() *SubnetEntry {
	if s == nil {
		return nil
	}

	clone := *s
	if len(s.SecondaryRanges) > 0 {
		clone.SecondaryRanges = make([]SubnetSecondaryRange, len(s.SecondaryRanges))
		copy(clone.SecondaryRanges, s.SecondaryRanges)
	}

	return &clone
}

// containsIP reports whether the provided IP falls within any of the subnet's CIDRs.
func (s *SubnetEntry) containsIP(ip net.IP) bool {
	if s == nil || ip == nil {
		return false
	}

	if cidrContainsIP(s.PrimaryCIDR, ip) {
		return true
	}

	for _, sr := range s.SecondaryRanges {
		if cidrContainsIP(sr.CIDR, ip) {
			return true
		}
	}

	return cidrContainsIP(s.IPv6CIDR, ip)
}

type cacheFile struct {
	GCP gcpCacheSection `json:"gcp"`
}

type gcpCacheSection struct {
	Instances map[string]*LocationInfo `json:"instances"`
	Zones     map[string]*ZoneListing  `json:"zones"`
	Projects  map[string]*ProjectEntry `json:"projects"`
	Subnets   map[string]*SubnetEntry  `json:"subnets"`
}

// Cache manages persistent storage of resource location information.
type Cache struct {
	instances map[string]*LocationInfo
	zones     map[string]*ZoneListing
	projects  map[string]*ProjectEntry
	subnets   map[string]*SubnetEntry
	filePath  string
	mu        sync.RWMutex
}

// ZoneListing holds cached zones for a project.
type ZoneListing struct {
	Timestamp time.Time `json:"timestamp"`
	Zones     []string  `json:"zones"`
}

// ProjectEntry tracks when a project was last used.
type ProjectEntry struct {
	Timestamp time.Time `json:"timestamp"`
}

// CachedLocation represents a resource cached for a project.
type CachedLocation struct {
	Name     string
	Type     ResourceType
	Location string
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
		filePath:  filePath,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
		subnets:   make(map[string]*SubnetEntry),
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
	c.mu.Lock()
	defer c.mu.Unlock()

	info, exists := c.instances[resourceName]
	if !exists {
		logger.Log.Debugf("Cache miss for resource: %s", resourceName)

		return nil, false
	}

	// Check if cache entry has expired
	if time.Since(info.Timestamp) > CacheExpiry {
		logger.Log.Debugf("Cache entry expired for resource: %s (age: %v)", resourceName, time.Since(info.Timestamp))

		delete(c.instances, resourceName)
		if err := c.save(); err != nil {
			logger.Log.Warnf("Failed to persist cache eviction for resource %s: %v", resourceName, err)
		}

		return nil, false
	}

	// Refresh last access timestamp for LRU semantics.
	info.Timestamp = time.Now()

	logger.Log.Debugf("Cache hit for resource: %s (project: %s, zone: %s, region: %s)",
		resourceName, info.Project, info.Zone, info.Region)

	if err := c.save(); err != nil {
		logger.Log.Warnf("Failed to persist cache access for resource %s: %v", resourceName, err)
	}

	return info, true
}

// Set stores location information in the cache and persists it.
func (c *Cache) Set(resourceName string, info *LocationInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	info.Timestamp = time.Now()
	c.instances[resourceName] = info
	if info.Project != "" {
		c.addProjectLocked(info.Project)
	}

	logger.Log.Debugf("Caching location for resource: %s (project: %s, zone: %s, region: %s)",
		resourceName, info.Project, info.Zone, info.Region)

	// Persist to disk
	return c.save()
}

// GetZones retrieves cached zones for a project.
func (c *Cache) GetZones(project string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.zones == nil {
		c.zones = make(map[string]*ZoneListing)
	}

	listing, exists := c.zones[project]
	if !exists || listing == nil {
		logger.Log.Debugf("Zone cache miss for project: %s", project)

		return nil, false
	}

	if time.Since(listing.Timestamp) > ZoneCacheTTL {
		logger.Log.Debugf("Zone cache expired for project: %s (age: %v)", project, time.Since(listing.Timestamp))
		delete(c.zones, project)
		if err := c.save(); err != nil {
			logger.Log.Warnf("Failed to persist zone cache eviction for project %s: %v", project, err)
		}

		return nil, false
	}

	listing.Timestamp = time.Now()
	zones := append([]string(nil), listing.Zones...)

	if err := c.save(); err != nil {
		logger.Log.Warnf("Failed to persist zone cache access for project %s: %v", project, err)
	}

	return zones, true
}

// SetZones caches zones for a project with TTL enforcement.
func (c *Cache) SetZones(project string, zones []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.zones == nil {
		c.zones = make(map[string]*ZoneListing)
	}

	c.zones[project] = &ZoneListing{
		Timestamp: time.Now(),
		Zones:     append([]string(nil), zones...),
	}

	logger.Log.Debugf("Cached %d zones for project: %s", len(zones), project)

	return c.save()
}

// GetLocationsByProject returns cached resources for a project.
func (c *Cache) GetLocationsByProject(project string) ([]CachedLocation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanExpired()

	results := make([]CachedLocation, 0)

	for name, info := range c.instances {
		if info == nil || info.Project != project {
			continue
		}

		location := info.Zone
		if location == "" {
			location = info.Region
		}

		results = append(results, CachedLocation{
			Name:     name,
			Type:     info.Type,
			Location: location,
		})
	}

	if len(results) == 0 {
		return nil, false
	}

	return results, true
}

// GetProjects returns cached projects ordered by most recent use.
func (c *Cache) GetProjects() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.projects == nil {
		c.projects = make(map[string]*ProjectEntry)
	}

	c.cleanExpiredProjects()

	if len(c.projects) == 0 {
		return nil
	}

	type projectRecord struct {
		name string
		ts   time.Time
	}

	records := make([]projectRecord, 0, len(c.projects))
	for name, entry := range c.projects {
		if entry == nil {
			continue
		}

		records = append(records, projectRecord{name: name, ts: entry.Timestamp})
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].ts.Equal(records[j].ts) {
			return records[i].name < records[j].name
		}

		return records[i].ts.After(records[j].ts)
	})

	projects := make([]string, 0, len(records))
	for _, record := range records {
		projects = append(projects, record.name)
	}

	return projects
}

// AddProject remembers a project for future autocompletion.
func (c *Cache) AddProject(project string) error {
	if project == "" {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.addProjectLocked(project)

	logger.Log.Debugf("Cached project: %s", project)

	return c.save()
}

func (c *Cache) addProjectLocked(project string) {
	if project == "" {
		return
	}

	if c.projects == nil {
		c.projects = make(map[string]*ProjectEntry)
	}

	entry, exists := c.projects[project]
	if !exists || entry == nil {
		entry = &ProjectEntry{}
		c.projects[project] = entry
	}

	entry.Timestamp = time.Now()
}

// Delete removes a resource from the cache.
func (c *Cache) Delete(resourceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.instances, resourceName)
	logger.Log.Debugf("Deleted resource from cache: %s", resourceName)

	return c.save()
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.instances = make(map[string]*LocationInfo)
	c.zones = make(map[string]*ZoneListing)
	c.projects = make(map[string]*ProjectEntry)
	c.subnets = make(map[string]*SubnetEntry)

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

	fileData := cacheFile{}
	if err := json.Unmarshal(data, &fileData); err != nil {
		return fmt.Errorf("failed to unmarshal cache data: %w", err)
	}

	if fileData.GCP.Instances == nil {
		fileData.GCP.Instances = make(map[string]*LocationInfo)
	}

	if fileData.GCP.Zones == nil {
		fileData.GCP.Zones = make(map[string]*ZoneListing)
	}

	if fileData.GCP.Projects == nil {
		fileData.GCP.Projects = make(map[string]*ProjectEntry)
	}

	if fileData.GCP.Subnets == nil {
		fileData.GCP.Subnets = make(map[string]*SubnetEntry)
	}

	c.instances = fileData.GCP.Instances
	c.zones = fileData.GCP.Zones
	c.projects = fileData.GCP.Projects
	c.subnets = fileData.GCP.Subnets
	c.cleanExpired()
	c.cleanExpiredZones()
	c.cleanExpiredProjects()
	c.cleanExpiredSubnets()

	logger.Log.Debugf("Loaded cache with %d entries", len(c.instances))

	return nil
}

// save writes the cache to disk.
func (c *Cache) save() error {
	// Clean expired entries before saving
	c.cleanExpired()
	c.cleanExpiredZones()
	c.cleanExpiredProjects()
	c.cleanExpiredSubnets()

	if c.instances == nil {
		c.instances = make(map[string]*LocationInfo)
	}

	if c.zones == nil {
		c.zones = make(map[string]*ZoneListing)
	}

	if c.projects == nil {
		c.projects = make(map[string]*ProjectEntry)
	}

	if c.subnets == nil {
		c.subnets = make(map[string]*SubnetEntry)
	}

	fileData := cacheFile{
		GCP: gcpCacheSection{
			Instances: c.instances,
			Zones:     c.zones,
			Projects:  c.projects,
			Subnets:   c.subnets,
		},
	}

	data, err := json.MarshalIndent(fileData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	if err := os.WriteFile(c.filePath, data, CacheFilePermissions); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	logger.Log.Debugf("Saved cache with %d instance entries and %d subnet entries to %s", len(c.instances), len(c.subnets), c.filePath)

	return nil
}

// cleanExpired removes expired entries from the cache.
func (c *Cache) cleanExpired() {
	count := 0

	for name, info := range c.instances {
		if time.Since(info.Timestamp) > CacheExpiry {
			delete(c.instances, name)

			count++
		}
	}

	if count > 0 {
		logger.Log.Debugf("Cleaned %d expired cache entries", count)
	}
}

// cleanExpiredZones removes zone listings older than the configured TTL.
func (c *Cache) cleanExpiredZones() {
	if c.zones == nil {
		return
	}

	now := time.Now()
	count := 0

	for project, listing := range c.zones {
		if listing == nil || now.Sub(listing.Timestamp) > ZoneCacheTTL {
			delete(c.zones, project)
			count++
		}
	}

	if count > 0 {
		logger.Log.Debugf("Cleaned %d expired zone cache entries", count)
	}
}

// cleanExpiredProjects removes project entries that are past the general cache expiry.
func (c *Cache) cleanExpiredProjects() {
	if c.projects == nil {
		return
	}

	now := time.Now()
	count := 0

	for project, entry := range c.projects {
		if entry == nil || now.Sub(entry.Timestamp) > CacheExpiry {
			delete(c.projects, project)
			count++
		}
	}

	if count > 0 {
		logger.Log.Debugf("Cleaned %d expired project cache entries", count)
	}
}

func (c *Cache) cleanExpiredSubnets() {
	if c.subnets == nil {
		return
	}

	now := time.Now()
	count := 0

	for key, entry := range c.subnets {
		if entry == nil || now.Sub(entry.Timestamp) > SubnetCacheTTL {
			delete(c.subnets, key)
			count++
		}
	}

	if count > 0 {
		logger.Log.Debugf("Cleaned %d expired subnet cache entries", count)
	}
}

// RememberSubnet records subnet metadata in the cache for faster future lookups.
func (c *Cache) RememberSubnet(info *SubnetEntry) error {
	if !Enabled() {
		return nil
	}

	if info == nil {
		return nil
	}

	project := strings.TrimSpace(info.Project)
	name := strings.TrimSpace(info.Name)
	if project == "" || name == "" {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.subnets == nil {
		c.subnets = make(map[string]*SubnetEntry)
	}

	key := subnetKey(project, strings.TrimSpace(info.Network), name)
	entry := info.clone()
	entry.Project = project
	entry.Name = name
	entry.Timestamp = time.Now()

	if entry.Region != "" {
		entry.Region = strings.TrimSpace(entry.Region)
	}

	if entry.Network != "" {
		entry.Network = strings.TrimSpace(entry.Network)
	}

	entry.PrimaryCIDR = strings.TrimSpace(entry.PrimaryCIDR)
	entry.IPv6CIDR = strings.TrimSpace(entry.IPv6CIDR)
	entry.Gateway = strings.TrimSpace(entry.Gateway)
	entry.SelfLink = strings.TrimSpace(entry.SelfLink)

	existing, ok := c.subnets[key]
	if ok && existing != nil {
		updateSubnetEntry(existing, entry)
	} else {
		c.subnets[key] = entry
		existing = entry
	}

	if entry.Region != "" {
		c.subnets[subnetKey(project, entry.Region, name)] = existing
	}

	if entry.SelfLink != "" {
		c.subnets[strings.ToLower(entry.SelfLink)] = existing
	}

	if entry.Project != "" {
		c.addProjectLocked(entry.Project)
	}

	return c.save()
}

// FindSubnetsForIP returns cached subnets whose CIDR ranges contain the specified IP.
func (c *Cache) FindSubnetsForIP(ip net.IP) []*SubnetEntry {
	if !Enabled() || ip == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.subnets == nil {
		return nil
	}

	results := make([]*SubnetEntry, 0)
	seen := make(map[*SubnetEntry]struct{})
	needSave := false
	for key, entry := range c.subnets {
		if entry == nil {
			delete(c.subnets, key)
			needSave = true
			continue
		}

		if time.Since(entry.Timestamp) > SubnetCacheTTL {
			delete(c.subnets, key)
			needSave = true
			continue
		}

		if _, exists := seen[entry]; exists {
			continue
		}

		if entry.containsIP(ip) {
			entry.Timestamp = time.Now()
			seen[entry] = struct{}{}
			results = append(results, entry.clone())
			needSave = true
		}
	}

	if needSave {
		if err := c.save(); err != nil {
			logger.Log.Warnf("Failed to persist subnet cache updates: %v", err)
		}
	}

	return results
}

// updateSubnetEntry merges metadata from src into dest while preserving slice allocations.
func updateSubnetEntry(dest, src *SubnetEntry) {
	if dest == nil || src == nil {
		return
	}

	dest.Timestamp = src.Timestamp
	dest.Project = src.Project
	dest.Network = src.Network
	dest.Region = src.Region
	dest.Name = src.Name
	dest.SelfLink = src.SelfLink
	dest.PrimaryCIDR = src.PrimaryCIDR
	dest.IPv6CIDR = src.IPv6CIDR
	dest.Gateway = src.Gateway

	if len(src.SecondaryRanges) > 0 {
		dest.SecondaryRanges = make([]SubnetSecondaryRange, len(src.SecondaryRanges))
		copy(dest.SecondaryRanges, src.SecondaryRanges)
	} else {
		dest.SecondaryRanges = nil
	}
}

// subnetKey builds a normalized cache key for subnet entries.
func subnetKey(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}

	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.ToLower(strings.TrimSpace(part))
		if p != "" {
			normalized = append(normalized, p)
		}
	}

	return strings.Join(normalized, "|")
}

// cidrContainsIP reports whether the given IP is inside the provided CIDR.
func cidrContainsIP(cidr string, ip net.IP) bool {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" || ip == nil {
		return false
	}

	_, network, err := net.ParseCIDR(cidr)
	if err != nil || network == nil {
		return false
	}

	return network.Contains(ip)
}
