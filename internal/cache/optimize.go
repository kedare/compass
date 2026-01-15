package cache

import (
	"fmt"
	"os"
	"time"

	"github.com/kedare/compass/internal/logger"
)

const (
	// OptimizationInterval is how often automatic optimization runs.
	OptimizationInterval = 30 * 24 * time.Hour // 30 days
)

// OptimizationResult contains the results of a database optimization.
type OptimizationResult struct {
	SizeBefore int64
	SizeAfter  int64
	Duration   time.Duration
}

// SpaceSaved returns the number of bytes saved by optimization.
func (r OptimizationResult) SpaceSaved() int64 {
	return r.SizeBefore - r.SizeAfter
}

// Optimize runs database maintenance (VACUUM and ANALYZE).
func (c *Cache) Optimize() (*OptimizationResult, error) {
	if c == nil || c.db == nil {
		return nil, ErrCacheDisabled
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("Optimize", time.Since(start))
	}()

	result := &OptimizationResult{}

	// Get size before optimization
	if fi, err := os.Stat(c.dbPath); err == nil {
		result.SizeBefore = fi.Size()
	}

	logger.Log.Debug("Starting database optimization...")

	// Run VACUUM to reclaim space
	if _, err := c.exec("VACUUM"); err != nil {
		return nil, fmt.Errorf("failed to vacuum database: %w", err)
	}

	// Run ANALYZE to update query planner statistics
	if _, err := c.exec("ANALYZE"); err != nil {
		return nil, fmt.Errorf("failed to analyze database: %w", err)
	}

	// Record optimization time
	now := time.Now()
	if _, err := c.exec(
		`INSERT OR REPLACE INTO metadata (key, value) VALUES ('last_optimized', ?)`,
		now.Format(time.RFC3339),
	); err != nil {
		logger.Log.Warnf("Failed to record optimization time: %v", err)
	}

	// Get size after optimization
	if fi, err := os.Stat(c.dbPath); err == nil {
		result.SizeAfter = fi.Size()
	}

	result.Duration = time.Since(start)
	logger.Log.Debugf("Database optimization completed in %v", result.Duration)

	return result, nil
}

// maybeAutoOptimize runs optimization if it hasn't been run recently.
func (c *Cache) maybeAutoOptimize() {
	if c == nil || c.db == nil {
		return
	}

	lastOpt := c.getLastOptimized()
	if lastOpt.IsZero() || time.Since(lastOpt) > OptimizationInterval {
		logger.Log.Debug("Running automatic database optimization...")

		if _, err := c.Optimize(); err != nil {
			logger.Log.Warnf("Automatic optimization failed: %v", err)
		}
	}
}

// getLastOptimized returns the time of last optimization.
func (c *Cache) getLastOptimized() time.Time {
	var lastOptStr string

	err := c.queryRow(`SELECT value FROM metadata WHERE key = 'last_optimized'`).Scan(&lastOptStr)
	if err != nil {
		return time.Time{}
	}

	ts, err := time.Parse(time.RFC3339, lastOptStr)
	if err != nil {
		return time.Time{}
	}

	return ts
}

// NeedsOptimization returns true if optimization should be run.
func (c *Cache) NeedsOptimization() bool {
	if c == nil || c.db == nil {
		return false
	}

	lastOpt := c.getLastOptimized()

	return lastOpt.IsZero() || time.Since(lastOpt) > OptimizationInterval
}

// OptimizationStatus returns information about optimization status.
type OptimizationStatus struct {
	LastOptimized     time.Time
	NextOptimization  time.Time
	NeedsOptimization bool
}

// GetOptimizationStatus returns the current optimization status.
func (c *Cache) GetOptimizationStatus() OptimizationStatus {
	if c == nil || c.db == nil {
		return OptimizationStatus{}
	}

	lastOpt := c.getLastOptimized()
	status := OptimizationStatus{
		LastOptimized:     lastOpt,
		NeedsOptimization: c.NeedsOptimization(),
	}

	if !lastOpt.IsZero() {
		status.NextOptimization = lastOpt.Add(OptimizationInterval)
	}

	return status
}
