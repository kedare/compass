package cache

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_cache.db")

	cache, err := newWithPath(dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cache.Close()
	})

	return cache
}

func TestNew(t *testing.T) {
	cache, err := New()
	require.NoError(t, err)
	require.NotNil(t, cache)
	require.NotNil(t, cache.db)

	_ = cache.Close()
}

func TestSetAndGet(t *testing.T) {
	cache := newTestCache(t)

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
	cache := newTestCache(t)

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
	cache := newTestCache(t)

	_, found := cache.Get("non-existent")
	require.False(t, found)
}

func TestCacheExpiry(t *testing.T) {
	cache := newTestCache(t)

	// Add an entry
	err := cache.Set("expired-instance", &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	// Manually set timestamp to expired
	expiryTime := time.Now().Add(-DefaultCacheExpiry - time.Hour).Unix()
	_, err = cache.db.Exec(`UPDATE instances SET timestamp = ? WHERE name = ?`, expiryTime, "expired-instance")
	require.NoError(t, err)

	_, found := cache.Get("expired-instance")
	require.False(t, found)
}

func TestDelete(t *testing.T) {
	cache := newTestCache(t)

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
	cache := newTestCache(t)

	// Add multiple entries
	require.NoError(t, cache.Set("instance1", &LocationInfo{Project: "p1", Zone: "z1", Type: ResourceTypeInstance}))
	require.NoError(t, cache.Set("instance2", &LocationInfo{Project: "p2", Zone: "z2", Type: ResourceTypeInstance}))
	require.NoError(t, cache.Set("mig1", &LocationInfo{Project: "p1", Region: "r1", Type: ResourceTypeMIG, IsRegional: true}))

	err := cache.Clear()
	require.NoError(t, err)

	// Verify all cleared
	_, found := cache.Get("instance1")
	require.False(t, found)
	_, found = cache.Get("instance2")
	require.False(t, found)
	_, found = cache.Get("mig1")
	require.False(t, found)
}

func TestPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_cache.db")

	// Create first cache instance and add data
	cache1, err := newWithPath(dbPath)
	require.NoError(t, err)

	info := &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	}

	err = cache1.Set("test-instance", info)
	require.NoError(t, err)
	_ = cache1.Close()

	// Create second cache instance and verify data persisted
	cache2, err := newWithPath(dbPath)
	require.NoError(t, err)
	defer func() { _ = cache2.Close() }()

	retrieved, found := cache2.Get("test-instance")
	require.True(t, found)
	require.Equal(t, "test-project", retrieved.Project)
}

func TestGetRefreshesTimestamp(t *testing.T) {
	cache := newTestCache(t)

	err := cache.Set("stale-instance", &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	// Set timestamp to 1 hour ago
	staleTimestamp := time.Now().Add(-time.Hour).Unix()
	_, err = cache.db.Exec(`UPDATE instances SET timestamp = ? WHERE name = ?`, staleTimestamp, "stale-instance")
	require.NoError(t, err)

	retrieved, found := cache.Get("stale-instance")
	require.True(t, found)
	require.True(t, retrieved.Timestamp.After(time.Unix(staleTimestamp, 0)))

	// Verify timestamp was updated in database
	var newTimestamp int64
	err = cache.db.QueryRow(`SELECT timestamp FROM instances WHERE name = ?`, "stale-instance").Scan(&newTimestamp)
	require.NoError(t, err)
	require.Greater(t, newTimestamp, staleTimestamp)
}

func TestZonesCacheSetAndGet(t *testing.T) {
	cache := newTestCache(t)

	zones := []string{"us-central1-a", "us-central1-b"}
	err := cache.SetZones("project", zones)
	require.NoError(t, err)

	retrieved, ok := cache.GetZones("project")
	require.True(t, ok)
	require.Len(t, retrieved, len(zones))

	for i, zone := range zones {
		require.Equal(t, zone, retrieved[i])
	}

	// Test that returned slice is a copy (mutation doesn't affect cache)
	retrieved[0] = "modified"

	again, ok := cache.GetZones("project")
	require.True(t, ok)
	require.Equal(t, zones[0], again[0])
}

func TestZonesCacheExpiry(t *testing.T) {
	cache := newTestCache(t)

	err := cache.SetZones("project", []string{"us-central1-a"})
	require.NoError(t, err)

	// Set timestamp to expired
	expiryTime := time.Now().Add(-DefaultCacheExpiry - time.Hour).Unix()
	_, err = cache.db.Exec(`UPDATE zones SET timestamp = ? WHERE project = ?`, expiryTime, "project")
	require.NoError(t, err)

	_, ok := cache.GetZones("project")
	require.False(t, ok)
}

func TestZonesCacheRefreshTimestamp(t *testing.T) {
	cache := newTestCache(t)

	err := cache.SetZones("project", []string{"us-central1-a"})
	require.NoError(t, err)

	// Set timestamp to 1 hour ago
	staleTimestamp := time.Now().Add(-time.Hour).Unix()
	_, err = cache.db.Exec(`UPDATE zones SET timestamp = ? WHERE project = ?`, staleTimestamp, "project")
	require.NoError(t, err)

	retrieved, ok := cache.GetZones("project")
	require.True(t, ok)
	require.Len(t, retrieved, 1)
	require.Equal(t, "us-central1-a", retrieved[0])

	// Verify timestamp was refreshed
	var newTimestamp int64
	err = cache.db.QueryRow(`SELECT timestamp FROM zones WHERE project = ?`, "project").Scan(&newTimestamp)
	require.NoError(t, err)
	require.Greater(t, newTimestamp, staleTimestamp)
}

func TestZonesPersistAcrossLoad(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "zones_cache.db")

	cache1, err := newWithPath(dbPath)
	require.NoError(t, err)

	err = cache1.SetZones("project", []string{"us-central1-a"})
	require.NoError(t, err)
	_ = cache1.Close()

	cache2, err := newWithPath(dbPath)
	require.NoError(t, err)
	defer func() { _ = cache2.Close() }()

	retrieved, ok := cache2.GetZones("project")
	require.True(t, ok)
	require.Len(t, retrieved, 1)
	require.Equal(t, "us-central1-a", retrieved[0])
}

func TestProjectsCacheAddAndRetrieve(t *testing.T) {
	cache := newTestCache(t)

	err := cache.AddProject("alpha")
	require.NoError(t, err)

	// Manually set alpha's timestamp to 1 hour ago to ensure different timestamps
	alphaTimestamp := time.Now().Add(-time.Hour).Unix()
	_, err = cache.db.Exec(`UPDATE projects SET timestamp = ? WHERE name = ?`, alphaTimestamp, "alpha")
	require.NoError(t, err)

	err = cache.AddProject("beta")
	require.NoError(t, err)

	projects := cache.GetProjects()
	require.Len(t, projects, 2)
	require.Equal(t, "beta", projects[0]) // Most recent first

	// Update alpha's timestamp to be newer than beta
	_, err = cache.db.Exec(`UPDATE projects SET timestamp = ? WHERE name = ?`, time.Now().Unix()+1, "alpha")
	require.NoError(t, err)

	projects = cache.GetProjects()
	require.Equal(t, "alpha", projects[0])
}

func TestProjectsCacheExpiry(t *testing.T) {
	cache := newTestCache(t)

	err := cache.AddProject("stale")
	require.NoError(t, err)

	// Set timestamp to expired
	expiryTime := time.Now().Add(-DefaultCacheExpiry - time.Hour).Unix()
	_, err = cache.db.Exec(`UPDATE projects SET timestamp = ? WHERE name = ?`, expiryTime, "stale")
	require.NoError(t, err)

	projects := cache.GetProjects()
	require.Empty(t, projects)
}

func TestGetLocationsByProject(t *testing.T) {
	cache := newTestCache(t)

	require.NoError(t, cache.Set("inst-1", &LocationInfo{Project: "proj", Zone: "us-central1-a", Type: ResourceTypeInstance}))
	require.NoError(t, cache.Set("mig-1", &LocationInfo{Project: "proj", Region: "us-central1", Type: ResourceTypeMIG, IsRegional: true}))
	require.NoError(t, cache.Set("other", &LocationInfo{Project: "other", Zone: "us-east1-b", Type: ResourceTypeInstance}))

	locations, ok := cache.GetLocationsByProject("proj")
	require.True(t, ok)
	require.Len(t, locations, 2)
	require.NotEqual(t, locations[0].Name, locations[1].Name)

	// Expire entries and ensure they are purged
	expiryTime := time.Now().Add(-DefaultCacheExpiry - time.Hour).Unix()
	_, err := cache.db.Exec(`UPDATE instances SET timestamp = ? WHERE project = ?`, expiryTime, "proj")
	require.NoError(t, err)

	_, ok = cache.GetLocationsByProject("proj")
	require.False(t, ok)
}

func TestIAPPreservation(t *testing.T) {
	cache := newTestCache(t)

	// Set initial info with IAP preference
	iapTrue := true
	info := &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
		IAP:     &iapTrue,
	}

	err := cache.Set("test-instance", info)
	require.NoError(t, err)

	// Verify IAP was stored
	retrieved, found := cache.Get("test-instance")
	require.True(t, found)
	require.NotNil(t, retrieved.IAP)
	require.True(t, *retrieved.IAP)

	// Update without IAP preference - should preserve existing
	updateInfo := &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-b", // Changed zone
		Type:    ResourceTypeInstance,
		IAP:     nil, // Not setting IAP
	}

	err = cache.Set("test-instance", updateInfo)
	require.NoError(t, err)

	// Verify IAP was preserved
	retrieved, found = cache.Get("test-instance")
	require.True(t, found)
	require.NotNil(t, retrieved.IAP)
	require.True(t, *retrieved.IAP)
	require.Equal(t, "us-central1-b", retrieved.Zone)
}

func TestSetAddsProjectAutomatically(t *testing.T) {
	cache := newTestCache(t)

	// Set an instance
	err := cache.Set("test-instance", &LocationInfo{
		Project: "auto-added-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	// Project should be in the list
	projects := cache.GetProjects()
	require.Contains(t, projects, "auto-added-project")
}
