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
	// Use a temporary directory as HOME to avoid polluting the real cache
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

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

	err = cache.Delete("test-instance", "test-project")
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

	err := cache.AddProject("beta")
	require.NoError(t, err)

	err = cache.AddProject("alpha")
	require.NoError(t, err)

	// GetProjects() should return alphabetically sorted
	projects := cache.GetProjects()
	require.Len(t, projects, 2)
	require.Equal(t, "alpha", projects[0]) // Alphabetically first
	require.Equal(t, "beta", projects[1])
}

func TestProjectsByUsageOrdering(t *testing.T) {
	cache := newTestCache(t)

	// Add projects with explicit timestamps to control ordering
	now := time.Now()

	err := cache.AddProject("alpha")
	require.NoError(t, err)

	err = cache.AddProject("beta")
	require.NoError(t, err)

	// Set explicit last_used timestamps: beta more recent than alpha
	_, err = cache.db.Exec(`UPDATE projects SET last_used = ? WHERE name = ?`, now.Add(-time.Hour).Unix(), "alpha")
	require.NoError(t, err)
	_, err = cache.db.Exec(`UPDATE projects SET last_used = ? WHERE name = ?`, now.Unix(), "beta")
	require.NoError(t, err)

	// GetProjectsByUsage() should return by most recent usage
	projects := cache.GetProjectsByUsage()
	require.Len(t, projects, 2)
	require.Equal(t, "beta", projects[0]) // Most recently used first
	require.Equal(t, "alpha", projects[1])

	// Mark alpha as used - set its last_used to even more recent
	_, err = cache.db.Exec(`UPDATE projects SET last_used = ? WHERE name = ?`, now.Add(time.Minute).Unix(), "alpha")
	require.NoError(t, err)

	// Now alpha should be first
	projects = cache.GetProjectsByUsage()
	require.Equal(t, "alpha", projects[0])
	require.Equal(t, "beta", projects[1])
}

func TestMarkInstanceAndProjectUsed(t *testing.T) {
	cache := newTestCache(t)

	// Add an instance
	err := cache.Set("test-instance", &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	// Set old last_used
	oldTime := time.Now().Add(-time.Hour).Unix()
	_, err = cache.db.Exec(`UPDATE instances SET last_used = ? WHERE name = ? AND project = ?`, oldTime, "test-instance", "test-project")
	require.NoError(t, err)
	_, err = cache.db.Exec(`UPDATE projects SET last_used = ? WHERE name = ?`, oldTime, "test-project")
	require.NoError(t, err)

	// Verify old last_used
	var instanceLastUsed, projectLastUsed int64
	err = cache.db.QueryRow(`SELECT last_used FROM instances WHERE name = ? AND project = ?`, "test-instance", "test-project").Scan(&instanceLastUsed)
	require.NoError(t, err)
	require.Equal(t, oldTime, instanceLastUsed)

	err = cache.db.QueryRow(`SELECT last_used FROM projects WHERE name = ?`, "test-project").Scan(&projectLastUsed)
	require.NoError(t, err)
	require.Equal(t, oldTime, projectLastUsed)

	// Mark as used (now requires project parameter)
	err = cache.MarkInstanceUsed("test-instance", "test-project")
	require.NoError(t, err)
	err = cache.MarkProjectUsed("test-project")
	require.NoError(t, err)

	// Verify new last_used is more recent
	err = cache.db.QueryRow(`SELECT last_used FROM instances WHERE name = ? AND project = ?`, "test-instance", "test-project").Scan(&instanceLastUsed)
	require.NoError(t, err)
	require.Greater(t, instanceLastUsed, oldTime)

	err = cache.db.QueryRow(`SELECT last_used FROM projects WHERE name = ?`, "test-project").Scan(&projectLastUsed)
	require.NoError(t, err)
	require.Greater(t, projectLastUsed, oldTime)
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

func TestHasProject(t *testing.T) {
	cache := newTestCache(t)

	// Project doesn't exist yet
	require.False(t, cache.HasProject("test-project"))

	// Add project
	err := cache.AddProject("test-project")
	require.NoError(t, err)

	// Now it exists
	require.True(t, cache.HasProject("test-project"))

	// Non-existent project
	require.False(t, cache.HasProject("non-existent"))
}

func TestHasProject_EmptyName(t *testing.T) {
	cache := newTestCache(t)

	// Empty name should return false
	require.False(t, cache.HasProject(""))
}

func TestHasProject_NoOpCache(t *testing.T) {
	cache := newNoOpCache()

	// No-op cache always returns false
	require.False(t, cache.HasProject("any-project"))
}

func TestDeleteProject(t *testing.T) {
	cache := newTestCache(t)

	// Setup: Add a project with instances, zones, and subnets
	err := cache.AddProject("test-project")
	require.NoError(t, err)

	// Add instances
	err = cache.Set("instance-1", &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	err = cache.Set("instance-2", &LocationInfo{
		Project: "test-project",
		Zone:    "us-central1-b",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	// Add zones
	err = cache.SetZones("test-project", []string{"us-central1-a", "us-central1-b"})
	require.NoError(t, err)

	// Add subnets
	err = cache.RememberSubnet(&SubnetEntry{
		Project:     "test-project",
		Network:     "default",
		Region:      "us-central1",
		Name:        "subnet-1",
		PrimaryCIDR: "10.0.0.0/24",
	})
	require.NoError(t, err)

	// Verify everything exists
	require.True(t, cache.HasProject("test-project"))
	_, found := cache.Get("instance-1")
	require.True(t, found)
	_, found = cache.Get("instance-2")
	require.True(t, found)
	zones, ok := cache.GetZones("test-project")
	require.True(t, ok)
	require.Len(t, zones, 2)

	// Delete the project
	err = cache.DeleteProject("test-project")
	require.NoError(t, err)

	// Verify everything is gone
	require.False(t, cache.HasProject("test-project"))
	_, found = cache.Get("instance-1")
	require.False(t, found)
	_, found = cache.Get("instance-2")
	require.False(t, found)
	_, ok = cache.GetZones("test-project")
	require.False(t, ok)

	// Verify subnets are deleted (check via direct DB query)
	var subnetCount int
	err = cache.db.QueryRow(`SELECT COUNT(*) FROM subnets WHERE project = ?`, "test-project").Scan(&subnetCount)
	require.NoError(t, err)
	require.Equal(t, 0, subnetCount)
}

func TestDeleteProject_NonExistent(t *testing.T) {
	cache := newTestCache(t)

	// Deleting a non-existent project should not error
	err := cache.DeleteProject("non-existent-project")
	require.NoError(t, err)
}

func TestDeleteProject_EmptyName(t *testing.T) {
	cache := newTestCache(t)

	// Empty name should be a no-op
	err := cache.DeleteProject("")
	require.NoError(t, err)
}

func TestDeleteProject_NoOpCache(t *testing.T) {
	cache := newNoOpCache()

	// No-op cache should not error
	err := cache.DeleteProject("any-project")
	require.NoError(t, err)
}

func TestDeleteProject_PreservesOtherProjects(t *testing.T) {
	cache := newTestCache(t)

	// Setup: Add two projects with instances
	err := cache.AddProject("project-a")
	require.NoError(t, err)
	err = cache.AddProject("project-b")
	require.NoError(t, err)

	err = cache.Set("instance-a", &LocationInfo{
		Project: "project-a",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	err = cache.Set("instance-b", &LocationInfo{
		Project: "project-b",
		Zone:    "us-east1-b",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	err = cache.SetZones("project-a", []string{"us-central1-a"})
	require.NoError(t, err)
	err = cache.SetZones("project-b", []string{"us-east1-b"})
	require.NoError(t, err)

	// Delete project-a
	err = cache.DeleteProject("project-a")
	require.NoError(t, err)

	// project-a should be gone
	require.False(t, cache.HasProject("project-a"))
	_, found := cache.Get("instance-a")
	require.False(t, found)
	_, ok := cache.GetZones("project-a")
	require.False(t, ok)

	// project-b should still exist
	require.True(t, cache.HasProject("project-b"))
	_, found = cache.Get("instance-b")
	require.True(t, found)
	zones, ok := cache.GetZones("project-b")
	require.True(t, ok)
	require.Len(t, zones, 1)
}

func TestDeleteProject_ThenAddAgain(t *testing.T) {
	cache := newTestCache(t)

	// Add project
	err := cache.AddProject("test-project")
	require.NoError(t, err)
	require.True(t, cache.HasProject("test-project"))

	// Delete project
	err = cache.DeleteProject("test-project")
	require.NoError(t, err)
	require.False(t, cache.HasProject("test-project"))

	// Add project again
	err = cache.AddProject("test-project")
	require.NoError(t, err)
	require.True(t, cache.HasProject("test-project"))
}

// TestSameNameDifferentProjects verifies that the same instance name can exist in multiple projects.
func TestSameNameDifferentProjects(t *testing.T) {
	cache := newTestCache(t)

	// Add instance with same name in two different projects
	infoA := &LocationInfo{
		Project: "project-a",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	}
	infoB := &LocationInfo{
		Project: "project-b",
		Zone:    "us-east1-b",
		Type:    ResourceTypeInstance,
	}

	err := cache.Set("shared-instance", infoA)
	require.NoError(t, err)

	err = cache.Set("shared-instance", infoB)
	require.NoError(t, err)

	// Both should be retrievable with GetWithProject
	retrievedA, found := cache.GetWithProject("shared-instance", "project-a")
	require.True(t, found)
	require.Equal(t, "project-a", retrievedA.Project)
	require.Equal(t, "us-central1-a", retrievedA.Zone)

	retrievedB, found := cache.GetWithProject("shared-instance", "project-b")
	require.True(t, found)
	require.Equal(t, "project-b", retrievedB.Project)
	require.Equal(t, "us-east1-b", retrievedB.Zone)

	// Get (without project) should return one of them (most recently used)
	retrieved, found := cache.Get("shared-instance")
	require.True(t, found)
	require.NotEmpty(t, retrieved.Project)
}

// TestGetAllByName verifies that GetAllByName returns all matches across projects.
func TestGetAllByName(t *testing.T) {
	cache := newTestCache(t)

	// Add instance with same name in three different projects
	projects := []string{"project-a", "project-b", "project-c"}
	zones := []string{"us-central1-a", "us-east1-b", "europe-west1-c"}

	for i, project := range projects {
		info := &LocationInfo{
			Project: project,
			Zone:    zones[i],
			Type:    ResourceTypeInstance,
		}
		err := cache.Set("multi-project-instance", info)
		require.NoError(t, err)
	}

	// GetAllByName should return all three
	matches := cache.GetAllByName("multi-project-instance")
	require.Len(t, matches, 3)

	// Verify all projects are present
	foundProjects := make(map[string]bool)
	for _, match := range matches {
		foundProjects[match.Project] = true
		require.Equal(t, "multi-project-instance", match.Name)
		require.NotNil(t, match.Info)
	}
	require.True(t, foundProjects["project-a"])
	require.True(t, foundProjects["project-b"])
	require.True(t, foundProjects["project-c"])

	// Non-existent instance should return empty slice
	noMatches := cache.GetAllByName("non-existent")
	require.Empty(t, noMatches)
}

// TestDeleteWithProject verifies that Delete only removes the specific (name, project) entry.
func TestDeleteWithProject(t *testing.T) {
	cache := newTestCache(t)

	// Add instance with same name in two projects
	err := cache.Set("delete-test", &LocationInfo{
		Project: "project-a",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	err = cache.Set("delete-test", &LocationInfo{
		Project: "project-b",
		Zone:    "us-east1-b",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	// Verify both exist
	_, foundA := cache.GetWithProject("delete-test", "project-a")
	require.True(t, foundA)
	_, foundB := cache.GetWithProject("delete-test", "project-b")
	require.True(t, foundB)

	// Delete only from project-a
	err = cache.Delete("delete-test", "project-a")
	require.NoError(t, err)

	// project-a should be gone, project-b should remain
	_, foundA = cache.GetWithProject("delete-test", "project-a")
	require.False(t, foundA)
	_, foundB = cache.GetWithProject("delete-test", "project-b")
	require.True(t, foundB)
}

// TestGetWithProjectMiss verifies that GetWithProject returns false for non-existent entries.
func TestGetWithProjectMiss(t *testing.T) {
	cache := newTestCache(t)

	// Add instance in project-a
	err := cache.Set("test-instance", &LocationInfo{
		Project: "project-a",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	// GetWithProject for different project should miss
	_, found := cache.GetWithProject("test-instance", "project-b")
	require.False(t, found)

	// GetWithProject for non-existent instance should miss
	_, found = cache.GetWithProject("non-existent", "project-a")
	require.False(t, found)
}

// TestMarkInstanceUsedWithProject verifies that MarkInstanceUsed works with the composite key.
func TestMarkInstanceUsedWithProject(t *testing.T) {
	cache := newTestCache(t)

	// Add instances with same name in two projects
	err := cache.Set("mark-test", &LocationInfo{
		Project: "project-a",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	err = cache.Set("mark-test", &LocationInfo{
		Project: "project-b",
		Zone:    "us-east1-b",
		Type:    ResourceTypeInstance,
	})
	require.NoError(t, err)

	// Set old last_used for both
	oldTime := time.Now().Add(-time.Hour).Unix()
	_, err = cache.db.Exec(`UPDATE instances SET last_used = ? WHERE name = ?`, oldTime, "mark-test")
	require.NoError(t, err)

	// Mark only project-a as used
	err = cache.MarkInstanceUsed("mark-test", "project-a")
	require.NoError(t, err)

	// Verify project-a was updated
	var lastUsedA, lastUsedB int64
	err = cache.db.QueryRow(`SELECT last_used FROM instances WHERE name = ? AND project = ?`, "mark-test", "project-a").Scan(&lastUsedA)
	require.NoError(t, err)
	require.Greater(t, lastUsedA, oldTime)

	// Verify project-b was NOT updated
	err = cache.db.QueryRow(`SELECT last_used FROM instances WHERE name = ? AND project = ?`, "mark-test", "project-b").Scan(&lastUsedB)
	require.NoError(t, err)
	require.Equal(t, oldTime, lastUsedB)
}

// TestIAPPreservationWithCompositeKey verifies IAP preference is preserved per (name, project).
func TestIAPPreservationWithCompositeKey(t *testing.T) {
	cache := newTestCache(t)

	// Add instance with IAP=true in project-a
	iapTrue := true
	err := cache.Set("iap-test", &LocationInfo{
		Project: "project-a",
		Zone:    "us-central1-a",
		Type:    ResourceTypeInstance,
		IAP:     &iapTrue,
	})
	require.NoError(t, err)

	// Add instance with IAP=false in project-b
	iapFalse := false
	err = cache.Set("iap-test", &LocationInfo{
		Project: "project-b",
		Zone:    "us-east1-b",
		Type:    ResourceTypeInstance,
		IAP:     &iapFalse,
	})
	require.NoError(t, err)

	// Verify each project has its own IAP preference
	retrievedA, found := cache.GetWithProject("iap-test", "project-a")
	require.True(t, found)
	require.NotNil(t, retrievedA.IAP)
	require.True(t, *retrievedA.IAP)

	retrievedB, found := cache.GetWithProject("iap-test", "project-b")
	require.True(t, found)
	require.NotNil(t, retrievedB.IAP)
	require.False(t, *retrievedB.IAP)

	// Update project-a without IAP - should preserve existing
	err = cache.Set("iap-test", &LocationInfo{
		Project: "project-a",
		Zone:    "us-west1-a", // Changed zone
		Type:    ResourceTypeInstance,
		IAP:     nil, // Not setting IAP
	})
	require.NoError(t, err)

	// Verify IAP was preserved for project-a
	retrievedA, found = cache.GetWithProject("iap-test", "project-a")
	require.True(t, found)
	require.NotNil(t, retrievedA.IAP)
	require.True(t, *retrievedA.IAP)
	require.Equal(t, "us-west1-a", retrievedA.Zone)
}
