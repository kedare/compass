// Package cache provides persistent caching for GCP resource locations using SQLite.
package cache

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kedare/compass/internal/logger"

	_ "modernc.org/sqlite" // SQLite driver
)

const (
	// CacheDir is the directory under $HOME/.cache where the cache database is stored.
	CacheDir = ".cache/compass"
	// CacheFileName is the name of the SQLite cache database file.
	CacheFileName = "cache.db"
	// OldCacheFileName is the name of the legacy JSON cache file for migration (in home directory).
	OldCacheFileName = ".compass.cache.json"
	// DefaultCacheExpiry defines the default TTL for cache entries (30 days).
	DefaultCacheExpiry = 30 * 24 * time.Hour
	// CacheFilePermissions defines the file permissions for the cache file (owner read/write only).
	CacheFilePermissions = 0o600
)

// Errors.
var (
	ErrCacheDisabled = errors.New("cache is disabled or not initialized")
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
	// MIGName is set if this instance belongs to a managed instance group.
	// Instances with MIGName set should typically be accessed via their MIG.
	MIGName string `json:"mig_name,omitempty"`
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
	MIGName  string // Set if this instance belongs to a MIG
}

// preparedStatements holds pre-compiled SQL statements for better performance.
type preparedStatements struct {
	getInstance         *sql.Stmt
	setInstance         *sql.Stmt
	updateInstanceTime  *sql.Stmt
	markInstanceUsed    *sql.Stmt
	getExistingIAP      *sql.Stmt
	deleteInstance      *sql.Stmt
	getZones            *sql.Stmt
	setZones            *sql.Stmt
	updateZonesTime     *sql.Stmt
	getProject          *sql.Stmt
	setProject          *sql.Stmt
	markProjectUsed     *sql.Stmt
	getSubnetsByIP      *sql.Stmt
	setSubnet           *sql.Stmt
	markSubnetUsed      *sql.Stmt
	getProjectsByUsage  *sql.Stmt
	getInstancesByUsage *sql.Stmt
}

// Cache manages persistent storage of resource location information using SQLite.
type Cache struct {
	db     *sql.DB
	dbPath string
	stmts  *preparedStatements
	stats  *Stats
}

// logSQL logs a SQL query and its arguments at debug level.
func logSQL(query string, args ...any) {
	if len(args) == 0 {
		logger.Log.Debugf("[SQL] %s", strings.TrimSpace(query))
	} else {
		logger.Log.Debugf("[SQL] %s | args: %v", strings.TrimSpace(query), args)
	}
}

// exec executes a SQL statement with logging.
func (c *Cache) exec(query string, args ...any) (sql.Result, error) {
	logSQL(query, args...)

	return c.db.Exec(query, args...)
}

// queryRow executes a query that returns a single row with logging.
func (c *Cache) queryRow(query string, args ...any) *sql.Row {
	logSQL(query, args...)

	return c.db.QueryRow(query, args...)
}

// query executes a query that returns multiple rows with logging.
func (c *Cache) query(query string, args ...any) (*sql.Rows, error) {
	logSQL(query, args...)

	return c.db.Query(query, args...)
}

// New creates a new cache instance.
// If initialization fails, returns a no-op cache that silently ignores operations.
func New() (*Cache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Log.Warnf("Failed to get user home directory: %v", err)

		return newNoOpCache(), nil
	}

	// Create cache directory with proper permissions
	cacheDir, err := ensureCacheDir(homeDir)
	if err != nil {
		logger.Log.Warnf("Failed to create cache directory: %v", err)

		return newNoOpCache(), nil
	}

	dbPath := filepath.Join(cacheDir, CacheFileName)
	logger.Log.Debugf("Cache database path: %s", dbPath)

	cache, err := newWithPath(dbPath)
	if err != nil {
		logger.Log.Warnf("Failed to initialize cache, using no-op cache: %v", err)

		return newNoOpCache(), nil
	}

	// Check for legacy JSON cache and migrate if needed
	jsonPath := filepath.Join(homeDir, OldCacheFileName)

	migrated, err := importFromJSON(cache.db, jsonPath)
	if err != nil {
		logger.Log.Warnf("Failed to migrate JSON cache: %v", err)
		// Continue with empty/existing SQLite cache
	} else if migrated {
		logger.Log.Debug("Successfully migrated cache from JSON to SQLite")
	}

	// Run automatic optimization if needed
	cache.maybeAutoOptimize()

	return cache, nil
}

// newNoOpCache creates a cache that silently ignores all operations.
func newNoOpCache() *Cache {
	return &Cache{
		db:     nil,
		dbPath: "",
		stmts:  nil,
		stats:  newStats(),
	}
}

// newWithPath creates a new cache instance with a specific database path.
// This is used internally and for testing.
func newWithPath(dbPath string) (*Cache, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open cache database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite works best with single connection
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Don't close idle connections

	// Initialize schema
	if err := initSchema(db); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("failed to initialize database schema: %w", err)
	}

	// Check and run migrations if needed
	version, err := getSchemaVersion(db)
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("failed to get schema version: %w", err)
	}

	if version == 0 {
		// New database - apply all migrations silently
		if err := initNewDatabase(db); err != nil {
			_ = db.Close()

			return nil, fmt.Errorf("failed to initialize new database: %w", err)
		}
	} else if version < SchemaVersion {
		// Existing database - run incremental migrations
		if err := migrateSchema(db, version, dbPath); err != nil {
			_ = db.Close()

			return nil, fmt.Errorf("failed to migrate schema: %w", err)
		}
	}

	cache := &Cache{
		db:     db,
		dbPath: dbPath,
		stats:  newStats(),
	}

	// Prepare statements for better performance
	if err := cache.prepareStatements(); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	// Set file permissions
	if err := os.Chmod(dbPath, CacheFilePermissions); err != nil {
		logger.Log.Debugf("Failed to set cache file permissions: %v", err)
	}

	// Clean expired entries on startup
	cache.cleanExpired()

	logger.Log.Debug("Cache database initialized")

	return cache, nil
}

// prepareStatements pre-compiles frequently used SQL statements.
func (c *Cache) prepareStatements() error {
	var err error

	c.stmts = &preparedStatements{}

	c.stmts.getInstance, err = c.db.Prepare(
		`SELECT timestamp, project, zone, region, type, is_regional, iap
		 FROM instances WHERE name = ? AND timestamp > ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare getInstance: %w", err)
	}

	c.stmts.setInstance, err = c.db.Prepare(
		`INSERT OR REPLACE INTO instances (name, timestamp, project, zone, region, type, is_regional, iap, last_used, mig_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare setInstance: %w", err)
	}

	c.stmts.updateInstanceTime, err = c.db.Prepare(
		`UPDATE instances SET timestamp = ? WHERE name = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare updateInstanceTime: %w", err)
	}

	c.stmts.markInstanceUsed, err = c.db.Prepare(
		`UPDATE instances SET last_used = ? WHERE name = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare markInstanceUsed: %w", err)
	}

	c.stmts.getExistingIAP, err = c.db.Prepare(
		`SELECT iap FROM instances WHERE name = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare getExistingIAP: %w", err)
	}

	c.stmts.deleteInstance, err = c.db.Prepare(
		`DELETE FROM instances WHERE name = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare deleteInstance: %w", err)
	}

	c.stmts.getZones, err = c.db.Prepare(
		`SELECT timestamp, zones_json FROM zones WHERE project = ? AND timestamp > ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare getZones: %w", err)
	}

	c.stmts.setZones, err = c.db.Prepare(
		`INSERT OR REPLACE INTO zones (project, timestamp, zones_json) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare setZones: %w", err)
	}

	c.stmts.updateZonesTime, err = c.db.Prepare(
		`UPDATE zones SET timestamp = ? WHERE project = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare updateZonesTime: %w", err)
	}

	c.stmts.getProject, err = c.db.Prepare(
		`SELECT timestamp FROM projects WHERE name = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare getProject: %w", err)
	}

	c.stmts.setProject, err = c.db.Prepare(
		`INSERT OR REPLACE INTO projects (name, timestamp, last_used) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare setProject: %w", err)
	}

	c.stmts.markProjectUsed, err = c.db.Prepare(
		`UPDATE projects SET last_used = ? WHERE name = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare markProjectUsed: %w", err)
	}

	c.stmts.getProjectsByUsage, err = c.db.Prepare(
		`SELECT name FROM projects WHERE timestamp > ? ORDER BY last_used DESC NULLS LAST, timestamp DESC`)
	if err != nil {
		return fmt.Errorf("failed to prepare getProjectsByUsage: %w", err)
	}

	c.stmts.getInstancesByUsage, err = c.db.Prepare(
		`SELECT DISTINCT project FROM instances WHERE timestamp > ? ORDER BY last_used DESC NULLS LAST`)
	if err != nil {
		return fmt.Errorf("failed to prepare getInstancesByUsage: %w", err)
	}

	c.stmts.getSubnetsByIP, err = c.db.Prepare(
		`SELECT id, timestamp, project, network, region, name, self_link, primary_cidr,
		        secondary_ranges_json, ipv6_cidr, gateway, cidr_start_int, cidr_end_int
		 FROM subnets
		 WHERE timestamp > ? AND cidr_start_int IS NOT NULL
		   AND cidr_start_int <= ? AND cidr_end_int >= ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare getSubnetsByIP: %w", err)
	}

	c.stmts.setSubnet, err = c.db.Prepare(
		`INSERT OR REPLACE INTO subnets
		 (timestamp, project, network, region, name, self_link, primary_cidr,
		  secondary_ranges_json, ipv6_cidr, gateway, cidr_start_int, cidr_end_int, last_used)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare setSubnet: %w", err)
	}

	c.stmts.markSubnetUsed, err = c.db.Prepare(
		`UPDATE subnets SET last_used = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare markSubnetUsed: %w", err)
	}

	return nil
}

// closeStatements closes all prepared statements.
func (c *Cache) closeStatements() {
	if c.stmts == nil {
		return
	}

	stmts := []*sql.Stmt{
		c.stmts.getInstance,
		c.stmts.setInstance,
		c.stmts.updateInstanceTime,
		c.stmts.markInstanceUsed,
		c.stmts.getExistingIAP,
		c.stmts.deleteInstance,
		c.stmts.getZones,
		c.stmts.setZones,
		c.stmts.updateZonesTime,
		c.stmts.getProject,
		c.stmts.setProject,
		c.stmts.markProjectUsed,
		c.stmts.getProjectsByUsage,
		c.stmts.getInstancesByUsage,
		c.stmts.getSubnetsByIP,
		c.stmts.setSubnet,
		c.stmts.markSubnetUsed,
	}

	for _, stmt := range stmts {
		if stmt != nil {
			_ = stmt.Close()
		}
	}
}

// Close closes the database connection.
func (c *Cache) Close() error {
	// Log stats before closing
	c.LogStats()

	if c.db == nil {
		return nil
	}

	c.closeStatements()

	return c.db.Close()
}

// isNoOp returns true if this is a no-op cache.
func (c *Cache) isNoOp() bool {
	return c == nil || c.db == nil
}

// Get retrieves location information from the cache.
func (c *Cache) Get(resourceName string) (*LocationInfo, bool) {
	if c.isNoOp() {
		return nil, false
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("Get", time.Since(start))
	}()

	now := time.Now()
	ttl := c.getEffectiveTTL(TTLTypeInstances)
	expiryTime := now.Add(-ttl).Unix()

	var info LocationInfo
	var timestamp int64
	var isRegional int
	var iap *int

	logSQL("getInstance", resourceName, expiryTime)

	err := c.stmts.getInstance.QueryRow(resourceName, expiryTime).Scan(
		&timestamp, &info.Project, &info.Zone, &info.Region, &info.Type, &isRegional, &iap,
	)

	if err == sql.ErrNoRows {
		logger.Log.Debugf("Cache miss for resource: %s", resourceName)
		c.stats.recordMiss()

		return nil, false
	}

	if err != nil {
		logger.Log.Warnf("Failed to query cache for resource %s: %v", resourceName, err)
		c.stats.recordMiss()

		return nil, false
	}

	info.Timestamp = time.Unix(timestamp, 0)
	info.IsRegional = isRegional == 1

	if iap != nil {
		val := *iap == 1
		info.IAP = &val
	}

	// Refresh timestamp for LRU semantics
	logSQL("updateInstanceTime", now.Unix(), resourceName)

	_, err = c.stmts.updateInstanceTime.Exec(now.Unix(), resourceName)
	if err != nil {
		logger.Log.Warnf("Failed to update timestamp for resource %s: %v", resourceName, err)
	}

	info.Timestamp = now

	logger.Log.Debugf("Cache hit for resource: %s (project: %s, zone: %s, region: %s)",
		resourceName, info.Project, info.Zone, info.Region)
	c.stats.recordHit()

	return &info, true
}

// Set stores location information in the cache and persists it.
func (c *Cache) Set(resourceName string, info *LocationInfo) error {
	if c.isNoOp() {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("Set", time.Since(start))
	}()

	now := time.Now()

	var iap *int
	if info.IAP != nil {
		val := 0
		if *info.IAP {
			val = 1
		}

		iap = &val
	} else {
		// Preserve existing IAP preference if not explicitly set
		var existingIAP *int

		logSQL("getExistingIAP", resourceName)
		_ = c.stmts.getExistingIAP.QueryRow(resourceName).Scan(&existingIAP)

		iap = existingIAP
	}

	isRegional := 0
	if info.IsRegional {
		isRegional = 1
	}

	logSQL("setInstance", resourceName, now.Unix(), info.Project, info.Zone, info.Region, string(info.Type), isRegional, iap, now.Unix(), info.MIGName)

	_, err := c.stmts.setInstance.Exec(
		resourceName, now.Unix(), info.Project, info.Zone, info.Region, string(info.Type), isRegional, iap, now.Unix(), info.MIGName,
	)
	if err != nil {
		return fmt.Errorf("failed to cache instance %s: %w", resourceName, err)
	}

	// Also add project to cache
	if info.Project != "" {
		_ = c.addProjectInternal(info.Project)
	}

	logger.Log.Debugf("Caching location for resource: %s (project: %s, zone: %s, region: %s)",
		resourceName, info.Project, info.Zone, info.Region)

	return nil
}

// SetBatch stores multiple location entries in a single transaction.
func (c *Cache) SetBatch(entries map[string]*LocationInfo) error {
	if c.isNoOp() {
		return nil
	}

	if len(entries) == 0 {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("SetBatch", time.Since(start))
	}()

	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now()
	projects := make(map[string]bool)

	stmt := tx.Stmt(c.stmts.setInstance)
	defer func() { _ = stmt.Close() }()

	for name, info := range entries {
		if info == nil {
			continue
		}

		var iap *int
		if info.IAP != nil {
			val := 0
			if *info.IAP {
				val = 1
			}

			iap = &val
		}

		isRegional := 0
		if info.IsRegional {
			isRegional = 1
		}

		logSQL("setInstance (batch)", name, now.Unix(), info.Project, info.Zone, info.Region, string(info.Type), isRegional, iap, now.Unix(), info.MIGName)

		_, err = stmt.Exec(name, now.Unix(), info.Project, info.Zone, info.Region, string(info.Type), isRegional, iap, now.Unix(), info.MIGName)
		if err != nil {
			return fmt.Errorf("failed to cache instance %s: %w", name, err)
		}

		if info.Project != "" {
			projects[info.Project] = true
		}
	}

	// Add all projects
	projectStmt := tx.Stmt(c.stmts.setProject)
	defer func() { _ = projectStmt.Close() }()

	for project := range projects {
		logSQL("setProject (batch)", project, now.Unix(), now.Unix())

		_, err = projectStmt.Exec(project, now.Unix(), now.Unix())
		if err != nil {
			logger.Log.Warnf("Failed to cache project %s: %v", project, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Log.Debugf("Batch cached %d instances", len(entries))

	return nil
}

// GetZones retrieves cached zones for a project.
func (c *Cache) GetZones(project string) ([]string, bool) {
	if c.isNoOp() {
		return nil, false
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("GetZones", time.Since(start))
	}()

	now := time.Now()
	ttl := c.getEffectiveTTL(TTLTypeZones)
	expiryTime := now.Add(-ttl).Unix()

	var timestamp int64
	var zonesJSON string

	logSQL("getZones", project, expiryTime)

	err := c.stmts.getZones.QueryRow(project, expiryTime).Scan(&timestamp, &zonesJSON)

	if err == sql.ErrNoRows {
		logger.Log.Debugf("Zone cache miss for project: %s", project)
		c.stats.recordMiss()

		return nil, false
	}

	if err != nil {
		logger.Log.Warnf("Failed to query zone cache for project %s: %v", project, err)
		c.stats.recordMiss()

		return nil, false
	}

	var zones []string
	if err := json.Unmarshal([]byte(zonesJSON), &zones); err != nil {
		logger.Log.Warnf("Failed to unmarshal zones for project %s: %v", project, err)

		return nil, false
	}

	// Refresh timestamp
	logSQL("updateZonesTime", now.Unix(), project)

	_, err = c.stmts.updateZonesTime.Exec(now.Unix(), project)
	if err != nil {
		logger.Log.Warnf("Failed to update zone cache timestamp for project %s: %v", project, err)
	}

	// Return a copy to avoid mutation issues
	result := make([]string, len(zones))
	copy(result, zones)

	c.stats.recordHit()

	return result, true
}

// SetZones caches zones for a project with TTL enforcement.
func (c *Cache) SetZones(project string, zones []string) error {
	if c.isNoOp() {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("SetZones", time.Since(start))
	}()

	zonesJSON, err := json.Marshal(zones)
	if err != nil {
		return fmt.Errorf("failed to marshal zones: %w", err)
	}

	logSQL("setZones", project, time.Now().Unix(), string(zonesJSON))

	_, err = c.stmts.setZones.Exec(project, time.Now().Unix(), string(zonesJSON))
	if err != nil {
		return fmt.Errorf("failed to cache zones for project %s: %w", project, err)
	}

	logger.Log.Debugf("Cached %d zones for project: %s", len(zones), project)

	return nil
}

// GetLocationsByProject returns cached resources for a project.
func (c *Cache) GetLocationsByProject(project string) ([]CachedLocation, bool) {
	if c.isNoOp() {
		return nil, false
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("GetLocationsByProject", time.Since(start))
	}()

	ttl := c.getEffectiveTTL(TTLTypeInstances)
	expiryTime := time.Now().Add(-ttl).Unix()

	rows, err := c.query(
		`SELECT name, type, zone, region, COALESCE(mig_name, '') FROM instances WHERE project = ? AND timestamp > ?`,
		project, expiryTime,
	)
	if err != nil {
		logger.Log.Warnf("Failed to query instances for project %s: %v", project, err)

		return nil, false
	}
	defer func() { _ = rows.Close() }()

	var results []CachedLocation

	for rows.Next() {
		var name, resourceType, zone, region, migName string
		if err := rows.Scan(&name, &resourceType, &zone, &region, &migName); err != nil {
			logger.Log.Warnf("Failed to scan instance row: %v", err)

			continue
		}

		location := zone
		if location == "" {
			location = region
		}

		results = append(results, CachedLocation{
			Name:     name,
			Type:     ResourceType(resourceType),
			Location: location,
			MIGName:  migName,
		})
	}

	if err := rows.Err(); err != nil {
		logger.Log.Warnf("Error iterating instance rows: %v", err)
	}

	if len(results) == 0 {
		return nil, false
	}

	return results, true
}

// GetProjects returns cached projects ordered alphabetically.
// This is intended for display purposes. For API call ordering, use GetProjectsByUsage().
func (c *Cache) GetProjects() []string {
	if c.isNoOp() {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("GetProjects", time.Since(start))
	}()

	ttl := c.getEffectiveTTL(TTLTypeProjects)
	expiryTime := time.Now().Add(-ttl).Unix()

	rows, err := c.query(
		`SELECT name FROM projects WHERE timestamp > ? ORDER BY name ASC`,
		expiryTime,
	)
	if err != nil {
		logger.Log.Warnf("Failed to query projects: %v", err)

		return nil
	}
	defer func() { _ = rows.Close() }()

	var projects []string

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			logger.Log.Warnf("Failed to scan project row: %v", err)

			continue
		}

		projects = append(projects, name)
	}

	if err := rows.Err(); err != nil {
		logger.Log.Warnf("Error iterating project rows: %v", err)
	}

	return projects
}

// AddProject remembers a project for future autocompletion.
func (c *Cache) AddProject(project string) error {
	if c.isNoOp() || project == "" {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("AddProject", time.Since(start))
	}()

	err := c.addProjectInternal(project)
	if err != nil {
		return err
	}

	logger.Log.Debugf("Cached project: %s", project)

	return nil
}

func (c *Cache) addProjectInternal(project string) error {
	if project == "" {
		return nil
	}

	now := time.Now().Unix()

	logSQL("setProject", project, now, now)

	_, err := c.stmts.setProject.Exec(project, now, now)
	if err != nil {
		return fmt.Errorf("failed to cache project %s: %w", project, err)
	}

	return nil
}

// MarkInstanceUsed updates the last_used timestamp for an instance.
// This should be called when an instance is accessed (e.g., SSH, details view).
func (c *Cache) MarkInstanceUsed(resourceName string) error {
	if c.isNoOp() || resourceName == "" {
		return nil
	}

	now := time.Now().Unix()

	logSQL("markInstanceUsed", now, resourceName)

	_, err := c.stmts.markInstanceUsed.Exec(now, resourceName)
	if err != nil {
		return fmt.Errorf("failed to mark instance %s as used: %w", resourceName, err)
	}

	logger.Log.Debugf("Marked instance as used: %s", resourceName)

	return nil
}

// MarkProjectUsed updates the last_used timestamp for a project.
// This should be called when a project is accessed (e.g., via instance, SSH).
func (c *Cache) MarkProjectUsed(project string) error {
	if c.isNoOp() || project == "" {
		return nil
	}

	now := time.Now().Unix()

	logSQL("markProjectUsed", now, project)

	_, err := c.stmts.markProjectUsed.Exec(now, project)
	if err != nil {
		return fmt.Errorf("failed to mark project %s as used: %w", project, err)
	}

	logger.Log.Debugf("Marked project as used: %s", project)

	return nil
}

// GetProjectsByUsage returns cached projects ordered by most recently used.
// This is intended for ordering API calls to search frequently used projects first.
// For display purposes, use GetProjects() which returns alphabetical order.
func (c *Cache) GetProjectsByUsage() []string {
	if c.isNoOp() {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("GetProjectsByUsage", time.Since(start))
	}()

	ttl := c.getEffectiveTTL(TTLTypeProjects)
	expiryTime := time.Now().Add(-ttl).Unix()

	logSQL("getProjectsByUsage", expiryTime)

	rows, err := c.stmts.getProjectsByUsage.Query(expiryTime)
	if err != nil {
		logger.Log.Warnf("Failed to query projects by usage: %v", err)

		return nil
	}
	defer func() { _ = rows.Close() }()

	var projects []string

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			logger.Log.Warnf("Failed to scan project row: %v", err)

			continue
		}

		projects = append(projects, name)
	}

	if err := rows.Err(); err != nil {
		logger.Log.Warnf("Error iterating project rows: %v", err)
	}

	return projects
}

// Delete removes a resource from the cache.
func (c *Cache) Delete(resourceName string) error {
	if c.isNoOp() {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("Delete", time.Since(start))
	}()

	logSQL("deleteInstance", resourceName)

	_, err := c.stmts.deleteInstance.Exec(resourceName)
	if err != nil {
		return fmt.Errorf("failed to delete resource %s: %w", resourceName, err)
	}

	logger.Log.Debugf("Deleted resource from cache: %s", resourceName)

	return nil
}

// DeleteProject removes a project and all its associated resources from the cache.
// This includes entries from the projects, instances, zones, and subnets tables.
func (c *Cache) DeleteProject(projectName string) error {
	if c.isNoOp() {
		return nil
	}

	if projectName == "" {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("DeleteProject", time.Since(start))
	}()

	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	tables := []struct {
		name   string
		column string
	}{
		{"instances", "project"},
		{"zones", "project"},
		{"subnets", "project"},
		{"projects", "name"},
	}

	for _, table := range tables {
		query := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", table.name, table.column)
		logSQL(query, projectName)

		result, err := tx.Exec(query, projectName)
		if err != nil {
			return fmt.Errorf("failed to delete from %s: %w", table.name, err)
		}

		count, _ := result.RowsAffected()
		if count > 0 {
			logger.Log.Debugf("Deleted %d entries from %s for project %s", count, table.name, projectName)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Log.Debugf("Deleted project from cache: %s", projectName)

	return nil
}

// HasProject checks if a project exists in the cache.
func (c *Cache) HasProject(projectName string) bool {
	if c.isNoOp() || projectName == "" {
		return false
	}

	var timestamp int64
	logSQL("getProject", projectName)

	err := c.stmts.getProject.QueryRow(projectName).Scan(&timestamp)

	return err == nil
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() error {
	if c.isNoOp() {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("Clear", time.Since(start))
	}()

	tables := []string{"instances", "zones", "projects", "subnets"}

	for _, table := range tables {
		if _, err := c.exec(fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			return fmt.Errorf("failed to clear table %s: %w", table, err)
		}
	}

	logger.Log.Debug("Cleared all cache entries")

	return nil
}

// cleanExpired removes expired entries from all tables.
func (c *Cache) cleanExpired() {
	if c.isNoOp() {
		return
	}

	tables := []struct {
		name    string
		ttlType TTLType
	}{
		{"instances", TTLTypeInstances},
		{"zones", TTLTypeZones},
		{"projects", TTLTypeProjects},
		{"subnets", TTLTypeSubnets},
	}

	for _, table := range tables {
		ttl := c.getEffectiveTTL(table.ttlType)
		expiryTime := time.Now().Add(-ttl).Unix()

		stmt := fmt.Sprintf("DELETE FROM %s WHERE timestamp <= ?", table.name)

		result, err := c.exec(stmt, expiryTime)
		if err != nil {
			logger.Log.Warnf("Failed to clean expired entries from %s: %v", table.name, err)

			continue
		}

		count, _ := result.RowsAffected()
		if count > 0 {
			logger.Log.Debugf("Cleaned %d expired entries from %s", count, table.name)
		}
	}
}

// RememberSubnet records subnet metadata in the cache for faster future lookups.
func (c *Cache) RememberSubnet(info *SubnetEntry) error {
	if !Enabled() || c.isNoOp() {
		return nil
	}

	if info == nil {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("RememberSubnet", time.Since(start))
	}()

	project := strings.TrimSpace(info.Project)
	name := strings.TrimSpace(info.Name)

	if project == "" || name == "" {
		return nil
	}

	network := strings.TrimSpace(info.Network)
	region := strings.TrimSpace(info.Region)

	secondaryJSON, err := json.Marshal(info.SecondaryRanges)
	if err != nil {
		return fmt.Errorf("failed to marshal secondary ranges: %w", err)
	}

	// Calculate CIDR bounds for faster lookups
	cidrStart, cidrEnd := cidrToIntRange(strings.TrimSpace(info.PrimaryCIDR))
	now := time.Now().Unix()

	logSQL("setSubnet", now, project, network, region, name,
		strings.TrimSpace(info.SelfLink), strings.TrimSpace(info.PrimaryCIDR),
		string(secondaryJSON), strings.TrimSpace(info.IPv6CIDR), strings.TrimSpace(info.Gateway),
		cidrStart, cidrEnd, now)

	_, err = c.stmts.setSubnet.Exec(
		now, project, network, region, name,
		strings.TrimSpace(info.SelfLink),
		strings.TrimSpace(info.PrimaryCIDR),
		string(secondaryJSON),
		strings.TrimSpace(info.IPv6CIDR),
		strings.TrimSpace(info.Gateway),
		cidrStart, cidrEnd, now,
	)
	if err != nil {
		return fmt.Errorf("failed to cache subnet %s: %w", name, err)
	}

	// Also remember the project
	_ = c.addProjectInternal(project)

	return nil
}

// RememberSubnetBatch records multiple subnets in a single transaction.
func (c *Cache) RememberSubnetBatch(entries []*SubnetEntry) error {
	if !Enabled() || c.isNoOp() {
		return nil
	}

	if len(entries) == 0 {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("RememberSubnetBatch", time.Since(start))
	}()

	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now()
	projects := make(map[string]bool)

	stmt := tx.Stmt(c.stmts.setSubnet)
	defer func() { _ = stmt.Close() }()

	for _, info := range entries {
		if info == nil {
			continue
		}

		project := strings.TrimSpace(info.Project)
		name := strings.TrimSpace(info.Name)

		if project == "" || name == "" {
			continue
		}

		secondaryJSON, err := json.Marshal(info.SecondaryRanges)
		if err != nil {
			logger.Log.Warnf("Failed to marshal secondary ranges for subnet %s: %v", name, err)

			continue
		}

		cidrStart, cidrEnd := cidrToIntRange(strings.TrimSpace(info.PrimaryCIDR))

		_, err = stmt.Exec(
			now.Unix(), project, strings.TrimSpace(info.Network), strings.TrimSpace(info.Region), name,
			strings.TrimSpace(info.SelfLink),
			strings.TrimSpace(info.PrimaryCIDR),
			string(secondaryJSON),
			strings.TrimSpace(info.IPv6CIDR),
			strings.TrimSpace(info.Gateway),
			cidrStart, cidrEnd, now.Unix(),
		)
		if err != nil {
			logger.Log.Warnf("Failed to cache subnet %s: %v", name, err)

			continue
		}

		projects[project] = true
	}

	// Add all projects
	projectStmt := tx.Stmt(c.stmts.setProject)
	defer func() { _ = projectStmt.Close() }()

	for project := range projects {
		_, err = projectStmt.Exec(project, now.Unix(), now.Unix())
		if err != nil {
			logger.Log.Warnf("Failed to cache project %s: %v", project, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Log.Debugf("Batch cached %d subnets", len(entries))

	return nil
}

// FindSubnetsForIP returns cached subnets whose CIDR ranges contain the specified IP.
func (c *Cache) FindSubnetsForIP(ip net.IP) []*SubnetEntry {
	if !Enabled() || c.isNoOp() || ip == nil {
		return nil
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("FindSubnetsForIP", time.Since(start))
	}()

	ttl := c.getEffectiveTTL(TTLTypeSubnets)
	expiryTime := time.Now().Add(-ttl).Unix()

	// Convert IP to integer for indexed lookup
	ipInt := ipToInt(ip)

	var rows *sql.Rows
	var err error

	// Try indexed lookup first for IPv4
	if ip.To4() != nil && ipInt > 0 {
		logSQL("getSubnetsByIP (indexed)", expiryTime, ipInt, ipInt)
		rows, err = c.stmts.getSubnetsByIP.Query(expiryTime, ipInt, ipInt)
	} else {
		// Fall back to full scan for IPv6 or when indexed lookup not available
		rows, err = c.query(
			`SELECT id, timestamp, project, network, region, name, self_link, primary_cidr,
			        secondary_ranges_json, ipv6_cidr, gateway, cidr_start_int, cidr_end_int
			 FROM subnets WHERE timestamp > ?`,
			expiryTime,
		)
	}

	if err != nil {
		logger.Log.Warnf("Failed to query subnets: %v", err)

		return nil
	}
	defer func() { _ = rows.Close() }()

	var results []*SubnetEntry
	var matchedIDs []int64

	for rows.Next() {
		var entry SubnetEntry
		var id, timestamp int64
		var secondaryJSON string
		var cidrStart, cidrEnd *int64

		err := rows.Scan(
			&id, &timestamp, &entry.Project, &entry.Network, &entry.Region, &entry.Name,
			&entry.SelfLink, &entry.PrimaryCIDR, &secondaryJSON, &entry.IPv6CIDR, &entry.Gateway,
			&cidrStart, &cidrEnd,
		)
		if err != nil {
			logger.Log.Warnf("Failed to scan subnet row: %v", err)

			continue
		}

		entry.Timestamp = time.Unix(timestamp, 0)

		if secondaryJSON != "" {
			if err := json.Unmarshal([]byte(secondaryJSON), &entry.SecondaryRanges); err != nil {
				logger.Log.Warnf("Failed to unmarshal secondary ranges: %v", err)
			}
		}

		// For indexed queries, we already filtered. For full scan, check containment.
		if ip.To4() == nil || ipInt == 0 {
			if !entry.containsIP(ip) {
				continue
			}
		}

		results = append(results, entry.clone())
		matchedIDs = append(matchedIDs, id)
	}

	if err := rows.Err(); err != nil {
		logger.Log.Warnf("Error iterating subnet rows: %v", err)
	}

	// Update timestamps for matched subnets
	if len(matchedIDs) > 0 {
		now := time.Now().Unix()

		for _, id := range matchedIDs {
			_, err := c.exec(`UPDATE subnets SET timestamp = ? WHERE id = ?`, now, id)
			if err != nil {
				logger.Log.Warnf("Failed to update subnet timestamp: %v", err)
			}
		}

		c.stats.recordHit()
	} else {
		c.stats.recordMiss()
	}

	return results
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

// cidrToIntRange converts a CIDR string to start and end IP integers.
// Returns 0, 0 for invalid CIDRs or IPv6.
func cidrToIntRange(cidr string) (int64, int64) {
	if cidr == "" {
		return 0, 0
	}

	_, network, err := net.ParseCIDR(cidr)
	if err != nil || network == nil {
		return 0, 0
	}

	// Only handle IPv4 for now
	ip4 := network.IP.To4()
	if ip4 == nil {
		return 0, 0
	}

	start := int64(binary.BigEndian.Uint32(ip4))

	// Calculate end IP from mask
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return 0, 0
	}

	hostBits := uint32(bits - ones)
	end := start | int64((1<<hostBits)-1)

	return start, end
}

// ipToInt converts an IP address to an integer.
// Returns 0 for IPv6 or invalid IPs.
func ipToInt(ip net.IP) int64 {
	if ip == nil {
		return 0
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}

	return int64(binary.BigEndian.Uint32(ip4))
}

// getFileInfo returns file info for the given path.
func getFileInfo(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// SQLResult holds the result of an arbitrary SQL query.
type SQLResult struct {
	Columns      []string
	Rows         [][]string
	RowsAffected int64
	IsQuery      bool // true for SELECT, false for INSERT/UPDATE/DELETE
}

// ExecuteSQL runs an arbitrary SQL statement and returns the results.
// For SELECT queries, it returns the column names and row data.
// For INSERT/UPDATE/DELETE, it returns the number of rows affected.
func (c *Cache) ExecuteSQL(query string) (*SQLResult, error) {
	if c == nil || c.db == nil {
		return nil, ErrCacheDisabled
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("ExecuteSQL", time.Since(start))
	}()

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty query")
	}

	logSQL("ExecuteSQL", query)

	// Determine if this is a query (SELECT) or a statement (INSERT/UPDATE/DELETE)
	upperQuery := strings.ToUpper(query)
	isQuery := strings.HasPrefix(upperQuery, "SELECT") ||
		strings.HasPrefix(upperQuery, "PRAGMA") ||
		strings.HasPrefix(upperQuery, "EXPLAIN")

	if isQuery {
		return c.executeQuery(query)
	}

	return c.executeStatement(query)
}

// executeQuery handles SELECT-style queries.
func (c *Cache) executeQuery(query string) (*SQLResult, error) {
	rows, err := c.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	result := &SQLResult{
		Columns: columns,
		IsQuery: true,
	}

	// Prepare value holders
	values := make([]any, len(columns))
	valuePtrs := make([]any, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make([]string, len(columns))
		for i, val := range values {
			row[i] = formatSQLValue(val)
		}

		result.Rows = append(result.Rows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// executeStatement handles INSERT/UPDATE/DELETE statements.
func (c *Cache) executeStatement(query string) (*SQLResult, error) {
	res, err := c.db.Exec(query)
	if err != nil {
		return nil, fmt.Errorf("statement failed: %w", err)
	}

	affected, _ := res.RowsAffected()

	return &SQLResult{
		RowsAffected: affected,
		IsQuery:      false,
	}, nil
}

// formatSQLValue converts a database value to a string representation.
func formatSQLValue(val any) string {
	if val == nil {
		return "NULL"
	}

	switch v := val.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}
