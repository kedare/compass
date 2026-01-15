// Package migrations provides database schema migrations for the cache system.
package migrations

import (
	"database/sql"
	"fmt"
	"sort"
)

// Migration defines a database schema migration.
type Migration interface {
	// Version returns the target schema version after this migration is applied.
	// Migrations are applied in version order (2, 3, 4, etc.).
	Version() int

	// Description returns a human-readable description of what this migration does.
	Description() string

	// Up applies the migration to the database.
	// It should be idempotent (safe to run multiple times).
	Up(db *sql.DB) error
}

// registry holds all registered migrations.
var registry []Migration

// Register adds a migration to the registry.
// This is typically called from init() functions in migration files.
func Register(m Migration) {
	registry = append(registry, m)
}

// All returns all registered migrations sorted by version.
func All() []Migration {
	sorted := make([]Migration, len(registry))
	copy(sorted, registry)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Version() < sorted[j].Version()
	})

	return sorted
}

// LatestVersion returns the highest migration version available.
func LatestVersion() int {
	if len(registry) == 0 {
		return 1 // Base schema version
	}

	maxVersion := 1
	for _, m := range registry {
		if m.Version() > maxVersion {
			maxVersion = m.Version()
		}
	}

	return maxVersion
}

// GetPending returns migrations that need to be applied given the current version.
func GetPending(currentVersion int) []Migration {
	var pending []Migration

	for _, m := range All() {
		if m.Version() > currentVersion {
			pending = append(pending, m)
		}
	}

	return pending
}

// Helper functions for migrations to use

// ExecStatements executes multiple SQL statements, ignoring "already exists" errors.
func ExecStatements(db *sql.DB, statements []string) error {
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			if !isIgnorableError(err) {
				return fmt.Errorf("failed to execute statement: %w", err)
			}
		}
	}

	return nil
}

// isIgnorableError checks if the error can be safely ignored (idempotent migrations).
func isIgnorableError(err error) bool {
	if err == nil {
		return true
	}

	errStr := err.Error()

	return containsSubstring(errStr, "duplicate column") ||
		containsSubstring(errStr, "already exists")
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
