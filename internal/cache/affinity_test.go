package cache

import (
	"testing"
	"time"
)

func TestRecordSearchAffinity(t *testing.T) {
	c := newTestCache(t)

	// Record affinity for a search using a short term (no prefix)
	projectResults := map[string]int{
		"project-a": 5,
		"project-b": 3,
	}

	// Use a short term without separator to avoid prefix learning
	err := c.RecordSearchAffinity("abc", projectResults, "")
	if err != nil {
		t.Fatalf("RecordSearchAffinity failed: %v", err)
	}

	// Verify stats
	entries, uniqueTerms, uniqueProjects, err := c.GetSearchAffinityStats()
	if err != nil {
		t.Fatalf("GetSearchAffinityStats failed: %v", err)
	}

	if entries != 2 {
		t.Errorf("expected 2 entries, got %d", entries)
	}
	if uniqueTerms != 1 {
		t.Errorf("expected 1 unique term, got %d", uniqueTerms)
	}
	if uniqueProjects != 2 {
		t.Errorf("expected 2 unique projects, got %d", uniqueProjects)
	}
}

func TestRecordSearchAffinityAccumulates(t *testing.T) {
	c := newTestCache(t)

	// Record first search with short term (no prefix)
	err := c.RecordSearchAffinity("abc", map[string]int{"project-a": 3}, "")
	if err != nil {
		t.Fatalf("first RecordSearchAffinity failed: %v", err)
	}

	// Record same search again
	err = c.RecordSearchAffinity("abc", map[string]int{"project-a": 2}, "")
	if err != nil {
		t.Fatalf("second RecordSearchAffinity failed: %v", err)
	}

	// Should still be 1 entry but with accumulated counts
	entries, _, _, err := c.GetSearchAffinityStats()
	if err != nil {
		t.Fatalf("GetSearchAffinityStats failed: %v", err)
	}

	if entries != 1 {
		t.Errorf("expected 1 entry (accumulated), got %d", entries)
	}
}

func TestRecordSearchAffinityWithPrefixLearning(t *testing.T) {
	c := newTestCache(t)

	// Record a search with a prefix-able term (e.g., "netmgt-dt40" -> "netmgt")
	err := c.RecordSearchAffinity("netmgt-dt40", map[string]int{"project-a": 1}, "")
	if err != nil {
		t.Fatalf("RecordSearchAffinity failed: %v", err)
	}

	// Should have 2 entries: one for "netmgt-dt40" and one for "netmgt"
	entries, uniqueTerms, _, err := c.GetSearchAffinityStats()
	if err != nil {
		t.Fatalf("GetSearchAffinityStats failed: %v", err)
	}

	if entries != 2 {
		t.Errorf("expected 2 entries (term + prefix), got %d", entries)
	}
	if uniqueTerms != 2 {
		t.Errorf("expected 2 unique terms, got %d", uniqueTerms)
	}
}

func TestGetProjectsForSearchWithAffinity(t *testing.T) {
	c := newTestCache(t)

	// Add some projects to the cache first
	for _, p := range []string{"project-a", "project-b", "project-c"} {
		if err := c.AddProject(p); err != nil {
			t.Fatalf("AddProject failed: %v", err)
		}
	}

	// Record affinity for project-b with the search term
	err := c.RecordSearchAffinity("myquery", map[string]int{"project-b": 10}, "")
	if err != nil {
		t.Fatalf("RecordSearchAffinity failed: %v", err)
	}

	// Get projects for search - project-b should be first
	projects := c.GetProjectsForSearch("myquery", nil)

	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(projects))
	}

	if projects[0] != "project-b" {
		t.Errorf("expected project-b first due to affinity, got %s", projects[0])
	}
}

func TestGetProjectsForSearchFallsBackToUsage(t *testing.T) {
	c := newTestCache(t)

	now := time.Now()

	// Add projects
	for _, p := range []string{"project-a", "project-b", "project-c"} {
		if err := c.AddProject(p); err != nil {
			t.Fatalf("AddProject failed: %v", err)
		}
	}

	// Set explicit last_used timestamps: project-c most recent
	_, _ = c.db.Exec(`UPDATE projects SET last_used = ? WHERE name = ?`, now.Add(-2*time.Hour).Unix(), "project-a")
	_, _ = c.db.Exec(`UPDATE projects SET last_used = ? WHERE name = ?`, now.Add(-time.Hour).Unix(), "project-b")
	_, _ = c.db.Exec(`UPDATE projects SET last_used = ? WHERE name = ?`, now.Unix(), "project-c")

	// Get projects for unknown search term - should fall back to usage ordering
	projects := c.GetProjectsForSearch("unknown-term", nil)

	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(projects))
	}

	// project-c should be first due to recent usage
	if projects[0] != "project-c" {
		t.Errorf("expected project-c first due to recent usage, got %s", projects[0])
	}
}

func TestCleanSearchAffinity(t *testing.T) {
	c := newTestCache(t)

	// Record affinity with short term (no prefix)
	err := c.RecordSearchAffinity("abc", map[string]int{"project-a": 1}, "")
	if err != nil {
		t.Fatalf("RecordSearchAffinity failed: %v", err)
	}

	// Manually set the last_hit to an old time
	oldTime := time.Now().Add(-24 * time.Hour).Unix()
	_, err = c.db.Exec(`UPDATE search_affinity SET last_hit = ?`, oldTime)
	if err != nil {
		t.Fatalf("failed to set old timestamp: %v", err)
	}

	// Clean with a 1 hour max age (should remove entries older than 1 hour)
	deleted, err := c.CleanSearchAffinity(time.Hour)
	if err != nil {
		t.Fatalf("CleanSearchAffinity failed: %v", err)
	}

	if deleted < 1 {
		t.Errorf("expected at least 1 deleted entry, got %d", deleted)
	}

	// Verify entries are gone
	entries, _, _, err := c.GetSearchAffinityStats()
	if err != nil {
		t.Fatalf("GetSearchAffinityStats failed: %v", err)
	}

	if entries != 0 {
		t.Errorf("expected 0 entries after clean, got %d", entries)
	}
}

func TestExtractSearchPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"netmgt-dt40", "netmgt"},
		{"prod_web_server", "prod"},
		{"api.service.v1", "api"},
		{"short", ""},        // Too short, no separator
		{"ab-cd", ""},        // Prefix too short (< 3 chars)
		{"abc-def", "abc"},   // Exactly 3 chars prefix
		{"longterm", "long"}, // No separator but >= 6 chars -> first 4
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractSearchPrefix(tt.input)
			if result != tt.expected {
				t.Errorf("extractSearchPrefix(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCalculateAffinityScore(t *testing.T) {
	now := time.Now()

	// Recent hit with high frequency should score higher
	recentHighFreq := calculateAffinityScore(10, 50, now, now)

	// Old hit with same frequency should score lower
	oldHighFreq := calculateAffinityScore(10, 50, now.Add(-30*24*time.Hour), now)

	if oldHighFreq >= recentHighFreq {
		t.Errorf("old hit (%f) should score lower than recent hit (%f)", oldHighFreq, recentHighFreq)
	}

	// Low frequency should score lower than high frequency
	recentLowFreq := calculateAffinityScore(1, 5, now, now)

	if recentLowFreq >= recentHighFreq {
		t.Errorf("low frequency (%f) should score lower than high frequency (%f)", recentLowFreq, recentHighFreq)
	}
}

func TestNormalizeSearchTerm(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Test", "test"},
		{"  UPPER  ", "upper"},
		{"MixedCase", "mixedcase"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeSearchTerm(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeSearchTerm(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRecordSearchAffinityIgnoresEmptyInputs(t *testing.T) {
	c := newTestCache(t)

	// Empty search term should be ignored
	err := c.RecordSearchAffinity("", map[string]int{"project-a": 1}, "")
	if err != nil {
		t.Fatalf("RecordSearchAffinity with empty term failed: %v", err)
	}

	// Empty project should be ignored
	err = c.RecordSearchAffinity("query", map[string]int{"": 1}, "")
	if err != nil {
		t.Fatalf("RecordSearchAffinity with empty project failed: %v", err)
	}

	// Zero results should be ignored
	err = c.RecordSearchAffinity("query", map[string]int{"project-a": 0}, "")
	if err != nil {
		t.Fatalf("RecordSearchAffinity with zero results failed: %v", err)
	}

	// Verify no entries were recorded
	entries, _, _, _ := c.GetSearchAffinityStats()
	if entries != 0 {
		t.Errorf("expected 0 entries for empty inputs, got %d", entries)
	}
}

func TestGetProjectsForSearchWithResourceTypes(t *testing.T) {
	c := newTestCache(t)

	// Add projects
	for _, p := range []string{"project-a", "project-b"} {
		if err := c.AddProject(p); err != nil {
			t.Fatalf("AddProject failed: %v", err)
		}
	}

	// Record affinity with specific resource type
	err := c.RecordSearchAffinity("myquery", map[string]int{"project-a": 5}, "compute.instance")
	if err != nil {
		t.Fatalf("RecordSearchAffinity failed: %v", err)
	}

	// Also record general affinity for project-b
	err = c.RecordSearchAffinity("myquery", map[string]int{"project-b": 10}, "")
	if err != nil {
		t.Fatalf("RecordSearchAffinity failed: %v", err)
	}

	// When searching with specific type, project-a should be prioritized
	projects := c.GetProjectsForSearch("myquery", []string{"compute.instance"})

	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	// Both should appear (with combined scoring from general + specific)
	found := make(map[string]bool)
	for _, p := range projects {
		found[p] = true
	}
	if !found["project-a"] || !found["project-b"] {
		t.Error("expected both projects to be returned")
	}
}

func TestTouchSearchAffinity(t *testing.T) {
	c := newTestCache(t)

	// Add project
	if err := c.AddProject("project-a"); err != nil {
		t.Fatalf("AddProject failed: %v", err)
	}

	// Record initial affinity with an old timestamp by directly inserting
	oldTime := time.Now().Add(-1 * time.Hour).Unix()
	_, err := c.db.Exec(`INSERT INTO search_affinity (search_term, project, resource_type, hit_count, total_results, last_hit)
		VALUES (?, ?, ?, 1, 5, ?)`, "myquery", "project-a", "compute.instance", oldTime)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Touch the affinity (should only update last_hit, not increment hit_count)
	err = c.TouchSearchAffinity("myquery", "project-a", "compute.instance")
	if err != nil {
		t.Fatalf("TouchSearchAffinity failed: %v", err)
	}

	// Verify last_hit was updated but hit_count remains the same
	var newLastHit int64
	var hitCount int
	row := c.db.QueryRow("SELECT last_hit, hit_count FROM search_affinity WHERE search_term = ? AND project = ? AND resource_type = ?",
		"myquery", "project-a", "compute.instance")
	if err := row.Scan(&newLastHit, &hitCount); err != nil {
		t.Fatalf("Failed to get updated values: %v", err)
	}

	// Verify last_hit was updated (should be recent, not 1 hour ago)
	if newLastHit <= oldTime {
		t.Errorf("expected last_hit to be updated from %d, but got %d", oldTime, newLastHit)
	}

	// Verify the update happened recently (within last minute)
	now := time.Now().Unix()
	if newLastHit < now-60 {
		t.Errorf("expected last_hit to be recent, but got %d (now is %d)", newLastHit, now)
	}

	if hitCount != 1 {
		t.Errorf("expected hit_count to remain 1, got %d", hitCount)
	}
}

func TestTouchSearchAffinityNonExistent(t *testing.T) {
	c := newTestCache(t)

	// Touch non-existent entry should not error (just no-op)
	err := c.TouchSearchAffinity("nonexistent", "project-x", "compute.instance")
	if err != nil {
		t.Fatalf("TouchSearchAffinity should not error for non-existent entry: %v", err)
	}

	// Verify nothing was created
	var count int
	row := c.db.QueryRow("SELECT COUNT(*) FROM search_affinity")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Failed to count entries: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 entries, got %d", count)
	}
}
