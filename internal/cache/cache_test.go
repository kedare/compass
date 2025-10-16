package cache

import (
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

	if cache.data == nil {
		t.Fatal("Cache data map should not be nil")
	}
}

func TestSetAndGet(t *testing.T) {
	// Create temporary cache file
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath: filepath.Join(tmpDir, "test_cache.json"),
		data:     make(map[string]*LocationInfo),
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
		filePath: filepath.Join(tmpDir, "test_cache.json"),
		data:     make(map[string]*LocationInfo),
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
		filePath: filepath.Join(tmpDir, "test_cache.json"),
		data:     make(map[string]*LocationInfo),
	}

	_, found := cache.Get("non-existent")
	if found {
		t.Error("Expected cache miss for non-existent entry")
	}
}

func TestCacheExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath: filepath.Join(tmpDir, "test_cache.json"),
		data:     make(map[string]*LocationInfo),
	}

	// Add entry with expired timestamp
	cache.data["expired-instance"] = &LocationInfo{
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
		filePath: filepath.Join(tmpDir, "test_cache.json"),
		data:     make(map[string]*LocationInfo),
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
		filePath: filepath.Join(tmpDir, "test_cache.json"),
		data:     make(map[string]*LocationInfo),
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

	if len(cache.data) != 0 {
		t.Errorf("Expected cache to be empty, got %d entries", len(cache.data))
	}
}

func TestPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test_cache.json")

	// Create first cache instance and add data
	cache1 := &Cache{
		filePath: cacheFile,
		data:     make(map[string]*LocationInfo),
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
		filePath: cacheFile,
		data:     make(map[string]*LocationInfo),
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
		filePath: filepath.Join(tmpDir, "non_existent.json"),
		data:     make(map[string]*LocationInfo),
	}

	err := cache.load()
	if err != nil {
		t.Fatalf("Expected no error when loading non-existent file, got: %v", err)
	}

	if len(cache.data) != 0 {
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
		filePath: cacheFile,
		data:     make(map[string]*LocationInfo),
	}

	err = cache.load()
	if err != nil {
		t.Fatalf("Expected no error when loading empty file, got: %v", err)
	}

	if len(cache.data) != 0 {
		t.Error("Expected empty cache when loading empty file")
	}
}

func TestCleanExpired(t *testing.T) {
	tmpDir := t.TempDir()
	cache := &Cache{
		filePath: filepath.Join(tmpDir, "test_cache.json"),
		data:     make(map[string]*LocationInfo),
	}

	// Add valid and expired entries
	cache.data["valid"] = &LocationInfo{
		Project:   "test-project",
		Zone:      "us-central1-a",
		Type:      ResourceTypeInstance,
		Timestamp: time.Now(),
	}

	cache.data["expired"] = &LocationInfo{
		Project:   "test-project",
		Zone:      "us-central1-b",
		Type:      ResourceTypeInstance,
		Timestamp: time.Now().Add(-CacheExpiry - time.Hour),
	}

	cache.cleanExpired()

	if _, found := cache.data["valid"]; !found {
		t.Error("Valid entry should not be removed")
	}

	if _, found := cache.data["expired"]; found {
		t.Error("Expired entry should be removed")
	}
}
