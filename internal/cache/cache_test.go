package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	cache, err := New()
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	if cache == nil {
		t.Fatal("Cache should not be nil")
	}

	if cache.instances == nil {
		t.Fatal("Cache instances map should not be nil")
	}
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
	if err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	// Get the entry
	retrieved, found := cache.Get("test-instance")
	if !found {
		t.Fatal("Expected to find cached entry")
	}

	if retrieved.Project != "test-project" {
		t.Errorf("Expected project 'test-project', got '%s'", retrieved.Project)
	}

	if retrieved.Zone != "us-central1-a" {
		t.Errorf("Expected zone 'us-central1-a', got '%s'", retrieved.Zone)
	}

	if retrieved.Type != ResourceTypeInstance {
		t.Errorf("Expected type 'instance', got '%s'", retrieved.Type)
	}
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
	if err != nil {
		t.Fatalf("Failed to set zonal MIG cache entry: %v", err)
	}

	retrieved, found := cache.Get("test-mig")
	if !found {
		t.Fatal("Expected to find cached zonal MIG entry")
	}

	if retrieved.IsRegional {
		t.Error("Expected zonal MIG, got regional")
	}

	// Test regional MIG
	regionalMIG := &LocationInfo{
		Project:    "test-project",
		Region:     "us-central1",
		Type:       ResourceTypeMIG,
		IsRegional: true,
	}

	err = cache.Set("test-regional-mig", regionalMIG)
	if err != nil {
		t.Fatalf("Failed to set regional MIG cache entry: %v", err)
	}

	retrieved, found = cache.Get("test-regional-mig")
	if !found {
		t.Fatal("Expected to find cached regional MIG entry")
	}

	if !retrieved.IsRegional {
		t.Error("Expected regional MIG, got zonal")
	}

	if retrieved.Region != "us-central1" {
		t.Errorf("Expected region 'us-central1', got '%s'", retrieved.Region)
	}
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
	if found {
		t.Error("Expected cache miss for non-existent entry")
	}
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
	if found {
		t.Error("Expected cache miss for expired entry")
	}
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
	if err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	err = cache.Delete("test-instance")
	if err != nil {
		t.Fatalf("Failed to delete cache entry: %v", err)
	}

	_, found := cache.Get("test-instance")
	if found {
		t.Error("Expected entry to be deleted")
	}
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
	if err := cache.Set("instance1", &LocationInfo{Project: "p1", Zone: "z1", Type: ResourceTypeInstance}); err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	if err := cache.Set("instance2", &LocationInfo{Project: "p2", Zone: "z2", Type: ResourceTypeInstance}); err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	if err := cache.Set("mig1", &LocationInfo{Project: "p1", Region: "r1", Type: ResourceTypeMIG, IsRegional: true}); err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	err := cache.Clear()
	if err != nil {
		t.Fatalf("Failed to clear cache: %v", err)
	}

	if len(cache.instances) != 0 {
		t.Errorf("Expected cache to be empty, got %d entries", len(cache.instances))
	}
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
	if err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	// Create second cache instance and load from file
	cache2 := &Cache{
		filePath:  cacheFile,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err = cache2.load()
	if err != nil {
		t.Fatalf("Failed to load cache: %v", err)
	}

	retrieved, found := cache2.Get("test-instance")
	if !found {
		t.Fatal("Expected to find cached entry after reload")
	}

	if retrieved.Project != "test-project" {
		t.Errorf("Expected project 'test-project', got '%s'", retrieved.Project)
	}
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
	if err != nil {
		t.Fatalf("Expected no error when loading non-existent file, got: %v", err)
	}

	if len(cache.instances) != 0 {
		t.Error("Expected empty cache when loading non-existent file")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "empty.json")

	// Create empty file
	err := os.WriteFile(cacheFile, []byte(""), 0o600)
	if err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	cache := &Cache{
		filePath:  cacheFile,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	err = cache.load()
	if err != nil {
		t.Fatalf("Expected no error when loading empty file, got: %v", err)
	}

	if len(cache.instances) != 0 {
		t.Error("Expected empty cache when loading empty file")
	}
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

	if _, found := cache.instances["valid"]; !found {
		t.Error("Valid entry should not be removed")
	}

	if _, found := cache.instances["expired"]; found {
		t.Error("Expired entry should be removed")
	}
}

func TestGetRefreshesTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
	}

	if err := cache.Set("stale-instance", &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	}); err != nil {
		t.Fatalf("Failed to seed cache entry: %v", err)
	}

	cache.mu.Lock()
	staleTimestamp := time.Now().Add(-CacheExpiry + time.Hour)
	cache.instances["stale-instance"].Timestamp = staleTimestamp
	if err := cache.save(); err != nil {
		cache.mu.Unlock()
		t.Fatalf("Failed to persist stale timestamp: %v", err)
	}
	cache.mu.Unlock()

	retrieved, found := cache.Get("stale-instance")
	if !found {
		t.Fatal("Expected to find stale cache entry")
	}

	if !retrieved.Timestamp.After(staleTimestamp) {
		t.Errorf("Expected timestamp to be refreshed, still %v", retrieved.Timestamp)
	}

	data, err := os.ReadFile(cache.filePath)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}

	var stored cacheFile
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("Failed to unmarshal cache file: %v", err)
	}

	entry, ok := stored.Instances["stale-instance"]
	if !ok {
		t.Fatal("Expected refreshed entry in persisted cache")
	}

	if !entry.Timestamp.After(staleTimestamp) {
		t.Error("Expected persisted timestamp to be refreshed")
	}
}

func TestSaveUsesInstancesKey(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	if err := cache.Set("instance", &LocationInfo{
		Project: "project",
		Zone:    "zone",
		Type:    ResourceTypeInstance,
	}); err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	raw, err := os.ReadFile(cache.filePath)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}

	var fileContents map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fileContents); err != nil {
		t.Fatalf("Failed to unmarshal cache file: %v", err)
	}

	locationPayload, ok := fileContents["instances"]
	if !ok {
		t.Fatal("Expected 'instances' key in cache file")
	}

	if _, ok := fileContents["zones"]; !ok {
		t.Fatal("Expected 'zones' key in cache file")
	}

	projectsPayload, ok := fileContents["projects"]
	if !ok {
		t.Fatal("Expected 'projects' key in cache file")
	}

	var projectEntries map[string]*ProjectEntry
	if err := json.Unmarshal(projectsPayload, &projectEntries); err != nil {
		t.Fatalf("Failed to unmarshal project entries: %v", err)
	}

	if _, ok := projectEntries["project"]; !ok {
		t.Fatal("Expected project entry under 'projects' key")
	}

	var locationEntries map[string]*LocationInfo
	if err := json.Unmarshal(locationPayload, &locationEntries); err != nil {
		t.Fatalf("Failed to unmarshal location entries: %v", err)
	}

	if _, ok := locationEntries["instance"]; !ok {
		t.Fatal("Expected instance entry under 'location' key")
	}
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
	if err := cache.SetZones("project", zones); err != nil {
		t.Fatalf("Failed to cache zones: %v", err)
	}

	retrieved, ok := cache.GetZones("project")
	if !ok {
		t.Fatal("Expected cached zones for project")
	}

	if len(retrieved) != len(zones) {
		t.Fatalf("Expected %d zones, got %d", len(zones), len(retrieved))
	}

	for i, zone := range zones {
		if retrieved[i] != zone {
			t.Fatalf("Expected zone %q at index %d, got %q", zone, i, retrieved[i])
		}
	}

	retrieved[0] = "modified"

	again, ok := cache.GetZones("project")
	if !ok {
		t.Fatal("Expected cached zones after mutation")
	}

	if again[0] != zones[0] {
		t.Fatalf("Cache should not be affected by caller mutation, got %q", again[0])
	}
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

	if _, ok := cache.GetZones("project"); ok {
		t.Fatal("Expected expired zone cache to miss")
	}
}

func TestZonesCacheRefreshTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "test_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	if err := cache.SetZones("project", []string{"us-central1-a"}); err != nil {
		t.Fatalf("Failed to cache zones: %v", err)
	}

	cache.mu.Lock()
	if listing, ok := cache.zones["project"]; ok {
		listing.Timestamp = time.Now().Add(-time.Hour)
	}
	cache.mu.Unlock()

	retrieved, ok := cache.GetZones("project")
	if !ok {
		t.Fatal("Expected zone cache hit")
	}

	if len(retrieved) != 1 || retrieved[0] != "us-central1-a" {
		t.Fatalf("Unexpected zones retrieved: %#v", retrieved)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	updated := cache.zones["project"].Timestamp

	if updated.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("Expected refreshed timestamp, got %v", updated)
	}
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

	if err := cache1.SetZones("project", []string{"us-central1-a"}); err != nil {
		t.Fatalf("Failed to seed zone cache: %v", err)
	}

	cache2 := &Cache{
		filePath:  cacheFile,
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	if err := cache2.load(); err != nil {
		t.Fatalf("Failed to load cache: %v", err)
	}

	retrieved, ok := cache2.GetZones("project")
	if !ok {
		t.Fatal("Expected cached zones after load")
	}

	if len(retrieved) != 1 || retrieved[0] != "us-central1-a" {
		t.Fatalf("Unexpected zones retrieved after load: %#v", retrieved)
	}
}

func TestProjectsCacheAddAndRetrieve(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "projects_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	if err := cache.AddProject("alpha"); err != nil {
		t.Fatalf("Failed to add project: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if err := cache.AddProject("beta"); err != nil {
		t.Fatalf("Failed to add second project: %v", err)
	}

	projects := cache.GetProjects()
	if len(projects) != 2 {
		t.Fatalf("Expected 2 projects, got %d", len(projects))
	}

	if projects[0] != "beta" {
		t.Fatalf("Expected most recent project first, got %q", projects[0])
	}

	cache.mu.Lock()
	cache.projects["alpha"].Timestamp = time.Now()
	cache.mu.Unlock()

	projects = cache.GetProjects()
	if projects[0] != "alpha" {
		t.Fatalf("Expected project order to update after timestamp refresh, got %q", projects[0])
	}
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
	if len(projects) != 0 {
		t.Fatalf("Expected expired project to be purged, got %v", projects)
	}
}

func TestGetLocationsByProject(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath:  filepath.Join(tmpDir, "loc_cache.json"),
		instances: make(map[string]*LocationInfo),
		zones:     make(map[string]*ZoneListing),
		projects:  make(map[string]*ProjectEntry),
	}

	if err := cache.Set("inst-1", &LocationInfo{Project: "proj", Zone: "us-central1-a", Type: ResourceTypeInstance}); err != nil {
		t.Fatalf("Failed to cache instance: %v", err)
	}

	if err := cache.Set("mig-1", &LocationInfo{Project: "proj", Region: "us-central1", Type: ResourceTypeMIG, IsRegional: true}); err != nil {
		t.Fatalf("Failed to cache MIG: %v", err)
	}

	if err := cache.Set("other", &LocationInfo{Project: "other", Zone: "us-east1-b", Type: ResourceTypeInstance}); err != nil {
		t.Fatalf("Failed to cache other instance: %v", err)
	}

	locations, ok := cache.GetLocationsByProject("proj")
	if !ok {
		t.Fatal("Expected cached locations for project")
	}

	if len(locations) != 2 {
		t.Fatalf("Expected 2 locations, got %d", len(locations))
	}

	if locations[0].Name == locations[1].Name {
		t.Fatal("Expected distinct location entries")
	}

	// Expire entries and ensure they are purged.
	cache.mu.Lock()
	for _, info := range cache.instances {
		info.Timestamp = time.Now().Add(-CacheExpiry - time.Hour)
	}
	cache.mu.Unlock()

	if _, ok := cache.GetLocationsByProject("proj"); ok {
		t.Fatal("Expected expired locations to be removed")
	}
}
