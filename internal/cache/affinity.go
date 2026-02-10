// Package cache provides persistent caching for GCP resource locations using SQLite.
package cache

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/kedare/compass/internal/logger"
)

// SearchAffinityEntry represents a learned association between a search term and a project.
type SearchAffinityEntry struct {
	SearchTerm   string
	Project      string
	ResourceType string
	HitCount     int
	TotalResults int
	LastHit      time.Time
}

// RecordSearchAffinity records that a search term found results in specific projects.
// This builds up learning data for future search prioritization.
// The resourceType can be empty string for general associations.
func (c *Cache) RecordSearchAffinity(searchTerm string, projectResults map[string]int, resourceType string) error {
	if c.isNoOp() {
		return nil
	}

	searchTerm = normalizeSearchTerm(searchTerm)
	if searchTerm == "" {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("RecordSearchAffinity", time.Since(start))
	}()

	now := time.Now().Unix()

	// Record affinity for each project that had results
	for project, resultCount := range projectResults {
		if project == "" || resultCount <= 0 {
			continue
		}

		// Upsert the affinity record
		query := `
			INSERT INTO search_affinity (search_term, project, resource_type, hit_count, total_results, last_hit)
			VALUES (?, ?, ?, 1, ?, ?)
			ON CONFLICT(search_term, project, resource_type) DO UPDATE SET
				hit_count = hit_count + 1,
				total_results = total_results + excluded.total_results,
				last_hit = excluded.last_hit`

		logSQL(query, searchTerm, project, resourceType, resultCount, now)

		if _, err := c.db.Exec(query, searchTerm, project, resourceType, resultCount, now); err != nil {
			logger.Log.Warnf("Failed to record search affinity for %s -> %s: %v", searchTerm, project, err)
			continue
		}

		// Also record prefix affinity for learning patterns
		prefix := extractSearchPrefix(searchTerm)
		if prefix != "" && prefix != searchTerm {
			prefixQuery := `
				INSERT INTO search_affinity (search_term, project, resource_type, hit_count, total_results, last_hit)
				VALUES (?, ?, ?, 1, ?, ?)
				ON CONFLICT(search_term, project, resource_type) DO UPDATE SET
					hit_count = hit_count + 1,
					total_results = total_results + excluded.total_results,
					last_hit = excluded.last_hit`

			logSQL(prefixQuery, prefix, project, "", resultCount, now)

			if _, err := c.db.Exec(prefixQuery, prefix, project, "", resultCount, now); err != nil {
				logger.Log.Debugf("Failed to record prefix affinity for %s -> %s: %v", prefix, project, err)
			}
		}
	}

	logger.Log.Debugf("Recorded search affinity for '%s' across %d projects", searchTerm, len(projectResults))

	return nil
}

// TouchSearchAffinity updates only the last_hit timestamp for an existing affinity entry.
// This is used to reinforce affinity when a user selects or interacts with a search result
// without incrementing the hit count (which would inflate the frequency score).
func (c *Cache) TouchSearchAffinity(searchTerm, project, resourceType string) error {
	if c.isNoOp() {
		return nil
	}

	searchTerm = normalizeSearchTerm(searchTerm)
	if searchTerm == "" || project == "" {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("TouchSearchAffinity", time.Since(start))
	}()

	now := time.Now().Unix()

	// Only update last_hit if the entry exists
	query := `UPDATE search_affinity SET last_hit = ? WHERE search_term = ? AND project = ? AND resource_type = ?`
	logSQL(query, now, searchTerm, project, resourceType)

	_, err := c.db.Exec(query, now, searchTerm, project, resourceType)
	if err != nil {
		return err
	}

	// Also touch the prefix affinity if applicable
	prefix := extractSearchPrefix(searchTerm)
	if prefix != "" && prefix != searchTerm {
		prefixQuery := `UPDATE search_affinity SET last_hit = ? WHERE search_term = ? AND project = ? AND resource_type = ''`
		logSQL(prefixQuery, now, prefix, project)
		_, _ = c.db.Exec(prefixQuery, now, prefix, project)
	}

	return nil
}

// GetProjectsForSearch returns projects ordered by likelihood of containing results
// for the given search term. It combines learned affinity data with recent usage.
// If no affinity data exists, falls back to GetProjectsByUsage().
func (c *Cache) GetProjectsForSearch(searchTerm string, resourceTypes []string) []string {
	if c.isNoOp() {
		return nil
	}

	searchTerm = normalizeSearchTerm(searchTerm)
	if searchTerm == "" {
		return c.GetProjectsByUsage()
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("GetProjectsForSearch", time.Since(start))
	}()

	// Get all projects as the base set
	allProjects := c.GetProjectsByUsage()
	if len(allProjects) == 0 {
		return nil
	}

	// Build a map for quick lookup
	projectSet := make(map[string]bool, len(allProjects))
	for _, p := range allProjects {
		projectSet[p] = true
	}

	// Get affinity scores for this search term
	scores := c.getAffinityScores(searchTerm, resourceTypes)

	// Also check prefix matches
	prefix := extractSearchPrefix(searchTerm)
	if prefix != "" && prefix != searchTerm {
		prefixScores := c.getAffinityScores(prefix, nil)
		for project, score := range prefixScores {
			// Prefix matches get 50% weight compared to exact matches
			if existing, ok := scores[project]; ok {
				scores[project] = existing + score*0.5
			} else {
				scores[project] = score * 0.5
			}
		}
	}

	// If no affinity data, return by usage
	if len(scores) == 0 {
		logger.Log.Debugf("No search affinity data for '%s', using usage-based ordering", searchTerm)
		return allProjects
	}

	// Sort projects by affinity score, then keep the rest in usage order
	type projectScore struct {
		project string
		score   float64
	}

	var scored []projectScore
	var unscored []string

	for _, project := range allProjects {
		if score, ok := scores[project]; ok {
			scored = append(scored, projectScore{project: project, score: score})
		} else {
			unscored = append(unscored, project)
		}
	}

	// Sort scored projects by score (descending)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Build final result: scored projects first, then unscored
	result := make([]string, 0, len(allProjects))
	for _, ps := range scored {
		result = append(result, ps.project)
	}
	result = append(result, unscored...)

	logger.Log.Debugf("Search affinity prioritized %d projects for '%s'", len(scored), searchTerm)

	return result
}

// getAffinityScores retrieves and scores affinity entries for a search term.
func (c *Cache) getAffinityScores(searchTerm string, resourceTypes []string) map[string]float64 {
	scores := make(map[string]float64)

	var query string
	var args []any

	if len(resourceTypes) > 0 {
		// Filter by resource types
		placeholders := make([]string, len(resourceTypes)+1)
		args = make([]any, len(resourceTypes)+2)
		args[0] = searchTerm
		placeholders[0] = "?"
		for i, rt := range resourceTypes {
			placeholders[i+1] = "?"
			args[i+1] = rt
		}
		args[len(args)-1] = searchTerm

		query = `
			SELECT project, resource_type, hit_count, total_results, last_hit
			FROM search_affinity
			WHERE (search_term = ? AND resource_type IN (` + strings.Join(placeholders, ",") + `))
			   OR (search_term = ? AND resource_type = '')`
	} else {
		query = `
			SELECT project, resource_type, hit_count, total_results, last_hit
			FROM search_affinity
			WHERE search_term = ?`
		args = []any{searchTerm}
	}

	logSQL(query, args...)

	rows, err := c.db.Query(query, args...)
	if err != nil {
		logger.Log.Warnf("Failed to query search affinity: %v", err)
		return scores
	}
	defer func() { _ = rows.Close() }()

	now := time.Now()

	for rows.Next() {
		var project, resourceType string
		var hitCount, totalResults int
		var lastHit int64

		if err := rows.Scan(&project, &resourceType, &hitCount, &totalResults, &lastHit); err != nil {
			logger.Log.Warnf("Failed to scan affinity row: %v", err)
			continue
		}

		score := calculateAffinityScore(hitCount, totalResults, time.Unix(lastHit, 0), now)

		// Accumulate scores for the same project (in case of multiple resource types)
		scores[project] += score
	}

	if err := rows.Err(); err != nil {
		logger.Log.Warnf("Error iterating affinity rows: %v", err)
	}

	return scores
}

// calculateAffinityScore computes a score based on hit frequency, result volume, and recency.
func calculateAffinityScore(hitCount, totalResults int, lastHit, now time.Time) float64 {
	// Recency factor: more recent = higher score
	// Half-life of ~7 days
	daysSinceHit := now.Sub(lastHit).Hours() / 24
	recencyFactor := 1.0 / (1.0 + daysSinceHit/7)

	// Frequency factor: more hits = higher score (with diminishing returns)
	frequencyFactor := math.Log(float64(hitCount) + 1)

	// Result volume factor: more results = slightly higher score
	volumeFactor := 1.0 + math.Log(float64(totalResults)+1)*0.1

	return frequencyFactor * recencyFactor * volumeFactor
}

// CleanSearchAffinity removes old affinity entries that haven't been hit recently.
// This prevents the table from growing indefinitely.
func (c *Cache) CleanSearchAffinity(maxAge time.Duration) (int64, error) {
	if c.isNoOp() {
		return 0, nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("CleanSearchAffinity", time.Since(start))
	}()

	cutoff := time.Now().Add(-maxAge).Unix()

	query := `DELETE FROM search_affinity WHERE last_hit < ?`
	logSQL(query, cutoff)

	result, err := c.db.Exec(query, cutoff)
	if err != nil {
		return 0, err
	}

	deleted, _ := result.RowsAffected()

	if deleted > 0 {
		logger.Log.Debugf("Cleaned %d old search affinity entries", deleted)
	}

	return deleted, nil
}

// GetSearchAffinityStats returns statistics about the search affinity data.
func (c *Cache) GetSearchAffinityStats() (entries int, uniqueTerms int, uniqueProjects int, err error) {
	if c.isNoOp() {
		return 0, 0, 0, nil
	}

	query := `SELECT COUNT(*), COUNT(DISTINCT search_term), COUNT(DISTINCT project) FROM search_affinity`
	logSQL(query)

	err = c.db.QueryRow(query).Scan(&entries, &uniqueTerms, &uniqueProjects)

	return
}

// normalizeSearchTerm normalizes a search term for consistent matching.
func normalizeSearchTerm(term string) string {
	return strings.ToLower(strings.TrimSpace(term))
}

// extractSearchPrefix extracts a meaningful prefix from a search term.
// For example: "netmgt-dt40" -> "netmgt", "prod_web_server" -> "prod"
func extractSearchPrefix(term string) string {
	term = normalizeSearchTerm(term)
	if term == "" {
		return ""
	}

	// Find the first separator (dash, underscore, or dot)
	for i, ch := range term {
		if ch == '-' || ch == '_' || ch == '.' {
			if i >= 3 { // Only if prefix is at least 3 chars
				return term[:i]
			}
			break
		}
	}

	// If no separator found but term is long, use first 4 chars as prefix
	if len(term) >= 6 {
		return term[:4]
	}

	return ""
}

// AddSearchHistory adds a search term to the search history.
func (c *Cache) AddSearchHistory(searchTerm string) error {
	if c.isNoOp() {
		return nil
	}

	searchTerm = normalizeSearchTerm(searchTerm)
	if searchTerm == "" {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("AddSearchHistory", time.Since(start))
	}()

	now := time.Now().Unix()

	// Insert or update the search history
	query := `
		INSERT INTO search_history (search_term, last_used, use_count)
		VALUES (?, ?, 1)
		ON CONFLICT(search_term) DO UPDATE SET
			last_used = excluded.last_used,
			use_count = use_count + 1`

	logSQL(query, searchTerm, now)

	_, err := c.db.Exec(query, searchTerm, now)
	return err
}

// GetSearchHistory returns recent search terms, ordered by most recent first.
// Limited to the specified count (default 10).
func (c *Cache) GetSearchHistory(limit int) ([]string, error) {
	if c.isNoOp() {
		return nil, nil
	}

	if limit <= 0 {
		limit = 10
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("GetSearchHistory", time.Since(start))
	}()

	query := `SELECT search_term FROM search_history ORDER BY last_used DESC LIMIT ?`
	logSQL(query, limit)

	rows, err := c.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var history []string
	for rows.Next() {
		var term string
		if err := rows.Scan(&term); err != nil {
			logger.Log.Warnf("Failed to scan search history: %v", err)
			continue
		}
		history = append(history, term)
	}

	return history, rows.Err()
}

// CleanSearchHistory removes old search history entries.
func (c *Cache) CleanSearchHistory(maxAge time.Duration) (int64, error) {
	if c.isNoOp() {
		return 0, nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("CleanSearchHistory", time.Since(start))
	}()

	cutoff := time.Now().Add(-maxAge).Unix()

	query := `DELETE FROM search_history WHERE last_used < ?`
	logSQL(query, cutoff)

	result, err := c.db.Exec(query, cutoff)
	if err != nil {
		return 0, err
	}

	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		logger.Log.Debugf("Cleaned %d old search history entries", deleted)
	}

	return deleted, nil
}
