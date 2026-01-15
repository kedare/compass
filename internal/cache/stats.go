package cache

import (
	"fmt"
	"sync"
	"time"

	"github.com/kedare/compass/internal/logger"
)

// Stats tracks cache statistics.
type Stats struct {
	mu sync.RWMutex

	// Hit/miss counters
	hits   int64
	misses int64

	// Operation counters by type
	operations map[string]int64

	// Operation timing
	totalTime    map[string]time.Duration
	operationMin map[string]time.Duration
	operationMax map[string]time.Duration

	// Start time for uptime calculation
	startTime time.Time
}

// newStats creates a new Stats instance.
func newStats() *Stats {
	return &Stats{
		operations:   make(map[string]int64),
		totalTime:    make(map[string]time.Duration),
		operationMin: make(map[string]time.Duration),
		operationMax: make(map[string]time.Duration),
		startTime:    time.Now(),
	}
}

// recordHit increments the hit counter.
func (s *Stats) recordHit() {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
}

// recordMiss increments the miss counter.
func (s *Stats) recordMiss() {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.misses++
	s.mu.Unlock()
}

// recordOperation records an operation with its duration.
func (s *Stats) recordOperation(op string, duration time.Duration) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.operations[op]++
	s.totalTime[op] += duration

	if min, ok := s.operationMin[op]; !ok || duration < min {
		s.operationMin[op] = duration
	}

	if duration > s.operationMax[op] {
		s.operationMax[op] = duration
	}
}

// Snapshot returns a copy of the current statistics.
type StatsSnapshot struct {
	Hits       int64
	Misses     int64
	HitRate    float64
	Operations map[string]int64
	AvgTime    map[string]time.Duration
	MinTime    map[string]time.Duration
	MaxTime    map[string]time.Duration
	Uptime     time.Duration
}

// Snapshot returns a snapshot of current statistics.
func (s *Stats) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{
			Operations: make(map[string]int64),
			AvgTime:    make(map[string]time.Duration),
			MinTime:    make(map[string]time.Duration),
			MaxTime:    make(map[string]time.Duration),
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := StatsSnapshot{
		Hits:       s.hits,
		Misses:     s.misses,
		Operations: make(map[string]int64, len(s.operations)),
		AvgTime:    make(map[string]time.Duration, len(s.totalTime)),
		MinTime:    make(map[string]time.Duration, len(s.operationMin)),
		MaxTime:    make(map[string]time.Duration, len(s.operationMax)),
		Uptime:     time.Since(s.startTime),
	}

	total := s.hits + s.misses
	if total > 0 {
		snapshot.HitRate = float64(s.hits) / float64(total)
	}

	for k, v := range s.operations {
		snapshot.Operations[k] = v
	}

	for k, v := range s.totalTime {
		if count := s.operations[k]; count > 0 {
			snapshot.AvgTime[k] = v / time.Duration(count)
		}
	}

	for k, v := range s.operationMin {
		snapshot.MinTime[k] = v
	}

	for k, v := range s.operationMax {
		snapshot.MaxTime[k] = v
	}

	return snapshot
}

// String returns a human-readable summary of the statistics.
func (s StatsSnapshot) String() string {
	if s.Hits == 0 && s.Misses == 0 {
		return "Cache stats: no operations"
	}

	return fmt.Sprintf(
		"Cache stats: hits=%d misses=%d hit_rate=%.1f%% uptime=%v",
		s.Hits, s.Misses, s.HitRate*100, s.Uptime.Round(time.Second),
	)
}

// LogStats logs the current statistics at debug level.
func (c *Cache) LogStats() {
	if c == nil || c.stats == nil {
		return
	}

	snapshot := c.stats.Snapshot()

	// Skip if no operations occurred
	if snapshot.Hits == 0 && snapshot.Misses == 0 && len(snapshot.Operations) == 0 {
		return
	}

	logger.Log.Debug(snapshot.String())

	// Log detailed operation stats if any
	if len(snapshot.Operations) > 0 {
		for op, count := range snapshot.Operations {
			avg := snapshot.AvgTime[op]
			min := snapshot.MinTime[op]
			max := snapshot.MaxTime[op]

			if avg > 0 {
				logger.Log.Debugf("  %s: count=%d avg=%v min=%v max=%v",
					op, count, avg.Round(time.Microsecond), min.Round(time.Microsecond), max.Round(time.Microsecond))
			}
		}
	}
}

// Stats returns the current statistics snapshot.
func (c *Cache) Stats() StatsSnapshot {
	if c == nil || c.stats == nil {
		return StatsSnapshot{
			Operations: make(map[string]int64),
			AvgTime:    make(map[string]time.Duration),
			MinTime:    make(map[string]time.Duration),
			MaxTime:    make(map[string]time.Duration),
		}
	}

	return c.stats.Snapshot()
}

// CacheInfo contains detailed information about the cache.
type CacheInfo struct {
	Path          string
	SizeBytes     int64
	SchemaVersion int
	InstanceCount int64
	ZoneCount     int64
	ProjectCount  int64
	SubnetCount   int64
	LastOptimized time.Time
	Stats         StatsSnapshot
	TTLs          map[TTLType]time.Duration
	DefaultTTL    time.Duration
}

// Info returns detailed information about the cache.
func (c *Cache) Info() (*CacheInfo, error) {
	if c == nil || c.db == nil {
		return nil, ErrCacheDisabled
	}

	info := &CacheInfo{
		Path:       c.dbPath,
		Stats:      c.Stats(),
		TTLs:       c.GetAllTTLs(),
		DefaultTTL: DefaultCacheExpiry,
	}

	// Get file size
	if fi, err := getFileInfo(c.dbPath); err == nil {
		info.SizeBytes = fi.Size()
	}

	// Get schema version
	if v, err := getSchemaVersion(c.db); err == nil {
		info.SchemaVersion = v
	}

	// Get counts (errors are ignored as counts default to 0)
	_ = c.queryRow(`SELECT COUNT(*) FROM instances`).Scan(&info.InstanceCount)
	_ = c.queryRow(`SELECT COUNT(*) FROM zones`).Scan(&info.ZoneCount)
	_ = c.queryRow(`SELECT COUNT(*) FROM projects`).Scan(&info.ProjectCount)
	_ = c.queryRow(`SELECT COUNT(*) FROM subnets`).Scan(&info.SubnetCount)

	// Get last optimized time
	var lastOptStr string
	if err := c.queryRow(`SELECT value FROM metadata WHERE key = 'last_optimized'`).Scan(&lastOptStr); err == nil {
		if ts, err := time.Parse(time.RFC3339, lastOptStr); err == nil {
			info.LastOptimized = ts
		}
	}

	return info, nil
}
