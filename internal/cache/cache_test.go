package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	cache, err := New()
	require.NoError(t, err)
	require.NotNil(t, cache)
	require.NotNil(t, cache.instances)
}

func TestSetAndGet(t *testing.T) {
	// Create temporary cache file
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	// Test instance
	instanceInfo := &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	}

	err := cache.Set("test-instance", instanceInfo)
	require.NoError(t, err)

	// Get the entry
	retrieved, found := cache.Get("test-instance")
	require.True(t, found)
	require.Equal(t, "test-project", retrieved.Project)
	require.Equal(t, "us-central1-a", retrieved.Zone)
	require.Equal(t, ResourceTypeInstance, retrieved.Type)
}

func TestMIGCache(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	// Test zonal MIG
	zonalMIG := &LocationInfo{
		Project:    "test-project",
		Zone:       "us-central1-a",
		Type:       ResourceTypeMIG,
		IsRegional: false,
	}

	err := cache.Set("test-mig", zonalMIG)
	require.NoError(t, err)

	retrieved, found := cache.Get("test-mig")
	require.True(t, found)
	require.False(t, retrieved.IsRegional)

	// Test regional MIG
	regionalMIG := &LocationInfo{
		Project:    "test-project",
		Region:     "us-central1",
		Type:       ResourceTypeMIG,
		IsRegional: true,
	}

	err = cache.Set("test-regional-mig", regionalMIG)
	require.NoError(t, err)

	retrieved, found = cache.Get("test-regional-mig")
	require.True(t, found)
	require.True(t, retrieved.IsRegional)
	require.Equal(t, "us-central1", retrieved.Region)
}

func TestCacheMiss(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	_, found := cache.Get("non-existent")
	require.False(t, found)
}

func TestCacheExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	// Add entry with expired timestamp
	cache.instances["expired-instance"] = &LocationInfo{
		Project:   "test-project",
		Zone:      "us-central1-a",
		Type:      ResourceTypeInstance,
		Timestamp: time.Now().Add(-CacheExpiry - time.Hour), // Expired
	}

	_, found := cache.Get("expired-instance")
	require.False(t, found)
}

func TestDelete(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	info := &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	}

	err := cache.Set("test-instance", info)
	require.NoError(t, err)

	err = cache.Delete("test-instance")
	require.NoError(t, err)

	_, found := cache.Get("test-instance")
	require.False(t, found)
}

func TestClear(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	// Add multiple entries
	require.NoError(t, cache.Set("instance1", &LocationInfo{Project: "p1", Zone: "z1", Type: ResourceTypeInstance}))
	require.NoError(t, cache.Set("instance2", &LocationInfo{Project: "p2", Zone: "z2", Type: ResourceTypeInstance}))
	require.NoError(t, cache.Set("mig1", &LocationInfo{Project: "p1", Region: "r1", Type: ResourceTypeMIG, IsRegional: true}))

	err := cache.Clear()
	require.NoError(t, err)
	require.Empty(t, cache.instances)
}

func TestPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test_cache.json")

	// Create first cache instance and add data
	cache1 := &Cache{
		filePath:  cacheFile,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	info := &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	}

	err := cache1.Set("test-instance", info)
	require.NoError(t, err)

	// Create second cache instance and load from file
	cache2 := &Cache{
		filePath:  cacheFile,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err = cache2.load()
	require.NoError(t, err)

	retrieved, found := cache2.Get("test-instance")
	require.True(t, found)
	require.Equal(t, "test-project", retrieved.Project)
}

func TestLoadNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "non_existent.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err := cache.load()
	require.NoError(t, err)
	require.Empty(t, cache.instances)
}

func TestLoadEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "empty.json")

	// Create empty file
	err := os.WriteFile(cacheFile, []byte(""), 0o600)
	require.NoError(t, err)

	cache := &Cache{
		filePath:  cacheFile,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err = cache.load()
	require.NoError(t, err)
	require.Empty(t, cache.instances)
}

func TestCleanExpired(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	// Add valid and expired entries
	cache.instances["valid"] = &LocationInfo{
		Project:   "test-project",
		Zone:      "us-central1-a",
		Type:      ResourceTypeInstance,
		Timestamp: time.Now(),
	}

	cache.instances["expired"] = &LocationInfo{
		Project:   "test-project",
		Zone:      "us-central1-b",
		Type:      ResourceTypeInstance,
		Timestamp: time.Now().Add(-CacheExpiry - time.Hour),
	}

	cache.cleanExpired()

	_, found := cache.instances["valid"]
	require.True(t, found)

	_, found = cache.instances["expired"]
	require.False(t, found)
}

func TestGetRefreshesTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
	}

	err := cache.Set("stale-instance", &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	cache.mu.Lock()
	staleTimestamp := time.Now().Add(-CacheExpiry + time.Hour)
	cache.instances["stale-instance"].Timestamp = staleTimestamp
	err = cache.save()
	cache.mu.Unlock()
	require.NoError(t, err)

	retrieved, found := cache.Get("stale-instance")
	require.True(t, found)
	require.True(t, retrieved.Timestamp.After(staleTimestamp))

	data, err := os.ReadFile(cache.filePath)
	require.NoError(t, err)

	var stored cacheFile
	err = json.Unmarshal(data, &stored)
	require.NoError(t, err)

	entry, ok := stored.GCP.Instances["stale-instance"]
	require.True(t, ok)
	require.True(t, entry.Timestamp.After(staleTimestamp))
}

func TestSaveUsesGCPSection(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err := cache.Set("instance", &LocationInfo{
		Project: "project",
		Zone:    "zone",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	raw, err := os.ReadFile(cache.filePath)
	require.NoError(t, err)

	var fileContents map[string]json.RawMessage
	err = json.Unmarshal(raw, &fileContents)
	require.NoError(t, err)

	gcpPayload, ok := fileContents["gcp"]
	require.True(t, ok)

	var gcpSection gcpCacheSection
	err = json.Unmarshal(gcpPayload, &gcpSection)
	require.NoError(t, err)

	require.NotNil(t, gcpSection.Instances)
	require.NotNil(t, gcpSection.Zones)
	require.NotNil(t, gcpSection.Projects)

	_, ok = gcpSection.Projects["project"]
	require.True(t, ok)

	_, ok = gcpSection.Instances["instance"]
	require.True(t, ok)
}

func TestZonesCacheSetAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	zones := []string{"us-central1-a", "us-central1-b"}
	err := cache.SetZones("project", zones)
	require.NoError(t, err)

	retrieved, ok := cache.GetZones("project")
	require.True(t, ok)
	require.Len(t, retrieved, len(zones))

	for i, zone := range zones {
		require.Equal(t, zone, retrieved[i])
	}

	retrieved[0] = "modified"

	again, ok := cache.GetZones("project")
	require.True(t, ok)
	require.Equal(t, zones[0], again[0])
}

func TestZonesCacheExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones: map[string]*ZoneListing{
			"project": {
				Timestamp: time.Now().Add(-ZoneCacheTTL - time.Hour),
				Zones:     []string{"us-central1-a"},
			},
		},
		projects: make(map[string]*ProjectEntry),
	}

	_, ok := cache.GetZones("project")
	require.False(t, ok)
}

func TestZonesCacheRefreshTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err := cache.SetZones("project", []string{"us-central1-a"})
	require.NoError(t, err)

	cache.mu.Lock()
	if listing, ok := cache.zones["project"]; ok {
		listing.Timestamp = time.Now().Add(-time.Hour)
	}
	cache.mu.Unlock()

	retrieved, ok := cache.GetZones("project")
	require.True(t, ok)
	require.Len(t, retrieved, 1)
	require.Equal(t, "us-central1-a", retrieved[0])

	cache.mu.Lock()
	defer cache.mu.Unlock()

	updated := cache.zones["project"].Timestamp

	require.False(t, updated.Before(time.Now().Add(-time.Minute)), "Expected refreshed timestamp, got %v", updated)
}

func TestZonesPersistAcrossLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "zones_cache.json")

	cache1 := &Cache{
		filePath:  cacheFile,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err := cache1.SetZones("project", []string{"us-central1-a"})
	require.NoError(t, err)

	cache2 := &Cache{
		filePath:  cacheFile,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err = cache2.load()
	require.NoError(t, err)

	retrieved, ok := cache2.GetZones("project")
	require.True(t, ok)
	require.Len(t, retrieved, 1)
	require.Equal(t, "us-central1-a", retrieved[0])
}

func TestProjectsCacheAddAndRetrieve(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "projects_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err := cache.AddProject("alpha")
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	err = cache.AddProject("beta")
	require.NoError(t, err)

	projects := cache.GetProjects()
	require.Len(t, projects, 2)
	require.Equal(t, "beta", projects[0])

	cache.mu.Lock()
	cache.projects["alpha"].Timestamp = time.Now()
	cache.mu.Unlock()

	projects = cache.GetProjects()
	require.Equal(t, "alpha", projects[0])
}

func TestProjectsCacheExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "projects_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects: map[string]*ProjectEntry{
			"stale": {
				Timestamp: time.Now().Add(-CacheExpiry - time.Hour),
			},
		},
	}

	projects := cache.GetProjects()
	require.Empty(t, projects)
}

func TestGetLocationsByProject(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "loc_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	require.NoError(t, cache.Set("inst-1", &LocationInfo{Project: "proj", Zone: "us-central1-a", Type: ResourceTypeInstance}))
	require.NoError(t, cache.Set("mig-1", &LocationInfo{Project: "proj", Region: "us-central1", Type: ResourceTypeMIG, IsRegional: true}))
	require.NoError(t, cache.Set("other", &LocationInfo{Project: "other", Zone: "us-east1-b", Type: ResourceTypeInstance}))

	locations, ok := cache.GetLocationsByProject("proj")
	require.True(t, ok)
	require.Len(t, locations, 2)
	require.NotEqual(t, locations[0].Name, locations[1].Name)

	// Expire entries and ensure they are purged.
	cache.mu.Lock()
	for _, info := range cache.instances {
		info.Timestamp = time.Now().Add(-CacheExpiry - time.Hour)
	}
	cache.mu.Unlock()

	_, ok = cache.GetLocationsByProject("proj")
	require.False(t, ok)
}
