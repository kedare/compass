// Package cache provides persistent caching for GCP resource locations using SQLite.
package cache

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kedare/compass/internal/logger"
)

// SchemaVersion is the current database schema version.
const SchemaVersion = 2

// SQL statements for creating the database schema.
const (
	createMetadataTable = `
		CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`

	createSettingsTable = `
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`

	createInstancesTable = `
		CREATE TABLE IF NOT EXISTS instances (
			name TEXT PRIMARY KEY,
			timestamp INTEGER NOT NULL,
			project TEXT NOT NULL,
			zone TEXT,
			region TEXT,
			type TEXT NOT NULL,
			is_regional INTEGER DEFAULT 0,
			iap INTEGER
		)`

	createInstancesIndexes = `
		CREATE INDEX IF NOT EXISTS idx_instances_project ON instances(project);
		CREATE INDEX IF NOT EXISTS idx_instances_timestamp ON instances(timestamp)`

	createZonesTable = `
		CREATE TABLE IF NOT EXISTS zones (
			project TEXT PRIMARY KEY,
			timestamp INTEGER NOT NULL,
			zones_json TEXT NOT NULL
		)`

	createProjectsTable = `
		CREATE TABLE IF NOT EXISTS projects (
			name TEXT PRIMARY KEY,
			timestamp INTEGER NOT NULL
		)`

	createProjectsIndexes = `
		CREATE INDEX IF NOT EXISTS idx_projects_timestamp ON projects(timestamp)`

	createSubnetsTable = `
		CREATE TABLE IF NOT EXISTS subnets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			project TEXT NOT NULL,
			network TEXT NOT NULL,
			region TEXT NOT NULL,
			name TEXT NOT NULL,
			self_link TEXT,
			primary_cidr TEXT,
			secondary_ranges_json TEXT,
			ipv6_cidr TEXT,
			gateway TEXT,
			cidr_start_int INTEGER,
			cidr_end_int INTEGER,
			UNIQUE(project, network, name)
		)`

	createSubnetsIndexes = `
		CREATE INDEX IF NOT EXISTS idx_subnets_project ON subnets(project);
		CREATE INDEX IF NOT EXISTS idx_subnets_region ON subnets(region);
		CREATE INDEX IF NOT EXISTS idx_subnets_self_link ON subnets(self_link);
		CREATE INDEX IF NOT EXISTS idx_subnets_timestamp ON subnets(timestamp)`

	// createSubnetsCIDRIndex is created during migration for v2+ databases
	createSubnetsCIDRIndex = `CREATE INDEX IF NOT EXISTS idx_subnets_cidr_range ON subnets(cidr_start_int, cidr_end_int)`
)

// initSchema creates all database tables and indexes.
func initSchema(db *sql.DB) error {
	// Base schema statements (compatible with v1)
	statements := []string{
		createMetadataTable,
		createSettingsTable,
		createInstancesTable,
		createInstancesIndexes,
		createZonesTable,
		createProjectsTable,
		createProjectsIndexes,
		createSubnetsTable,
		createSubnetsIndexes,
	}

	for _, stmt := range statements {
		logSQL(stmt)

		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to execute schema statement: %w", err)
		}
	}

	// Check if this is a new database (no schema version set)
	// For new databases, we can add v2 indexes directly
	version, _ := getSchemaVersion(db)
	if version == 0 {
		// New database - add v2 indexes
		logSQL(createSubnetsCIDRIndex)

		if _, err := db.Exec(createSubnetsCIDRIndex); err != nil {
			logger.Log.Debugf("Failed to create CIDR index (will be added during migration): %v", err)
		}

		// Set schema version
		if err := setSchemaVersion(db, SchemaVersion); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	}

	logger.Log.Debug("Database schema initialized")

	return nil
}

// getSchemaVersion returns the current schema version from the database.
// Returns 0 if no version is set (new database).
func getSchemaVersion(db *sql.DB) (int, error) {
	var version int

	query := "SELECT value FROM metadata WHERE key = 'schema_version'"
	logSQL(query)

	err := db.QueryRow(query).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("failed to get schema version: %w", err)
	}

	return version, nil
}

// setSchemaVersion updates the schema version in the database.
func setSchemaVersion(db *sql.DB, version int) error {
	query := "INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', ?)"
	logSQL(query, version)

	_, err := db.Exec(query, version)
	if err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}

	return nil
}

// backupDatabase creates a backup copy of the database before migration.
func backupDatabase(dbPath string) error {
	backupPath := dbPath + ".bak"

	src, err := os.Open(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No database to backup
		}

		return fmt.Errorf("failed to open database for backup: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(backupPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, CacheFilePermissions)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy database to backup: %w", err)
	}

	logger.Log.Debugf("Created database backup at %s", backupPath)

	return nil
}

// removeBackup removes the backup file after successful migration.
func removeBackup(dbPath string) {
	backupPath := dbPath + ".bak"
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		logger.Log.Debugf("Failed to remove backup file: %v", err)
	}
}

// migrateSchema handles schema migrations between versions.
func migrateSchema(db *sql.DB, currentVersion int, dbPath string) error {
	if currentVersion >= SchemaVersion {
		return nil
	}

	logger.Log.Debugf("Migrating database schema from version %d to %d", currentVersion, SchemaVersion)

	// Create backup before migration
	if err := backupDatabase(dbPath); err != nil {
		logger.Log.Warnf("Failed to create backup before migration: %v", err)
		// Continue with migration anyway
	}

	// Migration from v1 to v2
	if currentVersion < 2 {
		if err := migrateV1ToV2(db); err != nil {
			return fmt.Errorf("failed to migrate from v1 to v2: %w", err)
		}
	}

	if err := setSchemaVersion(db, SchemaVersion); err != nil {
		return err
	}

	// Remove backup after successful migration
	removeBackup(dbPath)

	return nil
}

// migrateV1ToV2 adds settings table and CIDR bounds columns.
func migrateV1ToV2(db *sql.DB) error {
	migrations := []string{
		// Create settings table
		createSettingsTable,
		// Add CIDR bounds columns to subnets
		`ALTER TABLE subnets ADD COLUMN cidr_start_int INTEGER`,
		`ALTER TABLE subnets ADD COLUMN cidr_end_int INTEGER`,
		// Create index for CIDR range queries
		createSubnetsCIDRIndex,
	}

	for _, stmt := range migrations {
		logSQL(stmt)

		if _, err := db.Exec(stmt); err != nil {
			// Ignore "duplicate column" errors for idempotent migrations
			if !isColumnExistsError(err) {
				return fmt.Errorf("failed to execute migration: %w", err)
			}
		}
	}

	// Update existing subnets with CIDR bounds
	if err := updateExistingSubnetCIDRBounds(db); err != nil {
		logger.Log.Warnf("Failed to update existing subnet CIDR bounds: %v", err)
		// Non-fatal, bounds will be updated on next access
	}

	logger.Log.Debug("Migrated schema from v1 to v2")

	return nil
}

// isColumnExistsError checks if the error is about a duplicate column.
func isColumnExistsError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	return contains(errStr, "duplicate column") || contains(errStr, "already exists")
}

// contains checks if s contains substr (case-insensitive would need strings package).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

// updateExistingSubnetCIDRBounds updates CIDR bounds for existing subnets.
func updateExistingSubnetCIDRBounds(db *sql.DB) error {
	query := `SELECT id, primary_cidr FROM subnets WHERE cidr_start_int IS NULL AND primary_cidr IS NOT NULL AND primary_cidr != ''`
	logSQL(query)

	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	updateQuery := `UPDATE subnets SET cidr_start_int = ?, cidr_end_int = ? WHERE id = ?`

	for rows.Next() {
		var id int64
		var cidr string

		if err := rows.Scan(&id, &cidr); err != nil {
			continue
		}

		start, end := cidrToIntRange(cidr)
		if start == 0 && end == 0 {
			continue
		}

		logSQL(updateQuery, start, end, id)

		if _, err := db.Exec(updateQuery, start, end, id); err != nil {
			logger.Log.Debugf("Failed to update CIDR bounds for subnet %d: %v", id, err)
		}
	}

	return rows.Err()
}

// ensureCacheDir creates the cache directory with proper permissions.
func ensureCacheDir(homeDir string) (string, error) {
	cacheDir := filepath.Join(homeDir, CacheDir)

	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	return cacheDir, nil
}
