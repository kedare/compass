// Package cache provides persistent caching for GCP resource locations using SQLite.
package cache

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kedare/compass/internal/cache/migrations"
	"github.com/kedare/compass/internal/logger"
)

// SchemaVersion is the current database schema version.
// This should match migrations.LatestVersion().
var SchemaVersion = migrations.LatestVersion()

// Base schema SQL statements (v1 compatible).
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
			UNIQUE(project, network, name)
		)`

	createSubnetsIndexes = `
		CREATE INDEX IF NOT EXISTS idx_subnets_project ON subnets(project);
		CREATE INDEX IF NOT EXISTS idx_subnets_region ON subnets(region);
		CREATE INDEX IF NOT EXISTS idx_subnets_self_link ON subnets(self_link);
		CREATE INDEX IF NOT EXISTS idx_subnets_timestamp ON subnets(timestamp)`
)

// initSchema creates the base database tables and indexes (v1 schema).
func initSchema(db *sql.DB) error {
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

	logger.Log.Debug("Base database schema initialized")

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

// migrateSchema handles schema migrations between versions using the registry.
func migrateSchema(db *sql.DB, currentVersion int, dbPath string) error {
	pending := migrations.GetPending(currentVersion)
	if len(pending) == 0 {
		return nil
	}

	targetVersion := migrations.LatestVersion()
	logger.Log.Debugf("Migrating database schema from version %d to %d", currentVersion, targetVersion)

	// Create backup before migration
	if err := backupDatabase(dbPath); err != nil {
		logger.Log.Warnf("Failed to create backup before migration: %v", err)
		// Continue with migration anyway
	}

	// Run each pending migration
	for _, m := range pending {
		logger.Log.Debugf("Applying migration v%d: %s", m.Version(), m.Description())

		if err := m.Up(db); err != nil {
			return fmt.Errorf("migration v%d failed: %w", m.Version(), err)
		}

		logger.Log.Debugf("Completed migration v%d", m.Version())
	}

	// Update schema version
	if err := setSchemaVersion(db, targetVersion); err != nil {
		return err
	}

	// Remove backup after successful migration
	removeBackup(dbPath)

	logger.Log.Debugf("Successfully migrated to schema version %d", targetVersion)

	return nil
}

// initNewDatabase applies all migrations to a fresh database.
func initNewDatabase(db *sql.DB) error {
	allMigrations := migrations.All()

	for _, m := range allMigrations {
		if err := m.Up(db); err != nil {
			logger.Log.Debugf("Migration v%d during init (may be expected): %v", m.Version(), err)
			// Continue - errors during fresh init are often expected (columns already exist from base schema)
		}
	}

	return setSchemaVersion(db, migrations.LatestVersion())
}

// ensureCacheDir creates the cache directory with proper permissions.
func ensureCacheDir(homeDir string) (string, error) {
	cacheDir := filepath.Join(homeDir, CacheDir)

	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	return cacheDir, nil
}
