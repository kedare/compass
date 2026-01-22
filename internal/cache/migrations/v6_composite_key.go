package migrations

import (
	"database/sql"
)

func init() {
	Register(&v6CompositeKey{})
}

// v6CompositeKey changes instances table to use composite primary key (name, project).
// This allows the same instance name to exist in multiple projects.
type v6CompositeKey struct{}

func (m *v6CompositeKey) Version() int {
	return 6
}

func (m *v6CompositeKey) Description() string {
	return "Change instances table to use composite primary key (name, project)"
}

func (m *v6CompositeKey) Up(db *sql.DB) error {
	// SQLite doesn't support ALTER TABLE to change primary keys,
	// so we need to recreate the table.

	statements := []string{
		// Rename the old table
		`ALTER TABLE instances RENAME TO instances_old`,

		// Create new table with composite primary key
		`CREATE TABLE IF NOT EXISTS instances (
			name TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			project TEXT NOT NULL,
			zone TEXT,
			region TEXT,
			type TEXT NOT NULL,
			is_regional INTEGER DEFAULT 0,
			iap INTEGER,
			last_used INTEGER,
			mig_name TEXT,
			PRIMARY KEY (name, project)
		)`,

		// Copy data from old table to new table
		// Use INSERT OR IGNORE to handle any duplicates that might exist
		`INSERT OR IGNORE INTO instances (name, timestamp, project, zone, region, type, is_regional, iap, last_used, mig_name)
		 SELECT name, timestamp, project, zone, region, type, is_regional, iap, last_used, mig_name
		 FROM instances_old`,

		// Drop the old table
		`DROP TABLE instances_old`,

		// Create index on name for efficient lookups when searching by name only
		`CREATE INDEX IF NOT EXISTS idx_instances_name ON instances(name)`,

		// Recreate the project index
		`CREATE INDEX IF NOT EXISTS idx_instances_project ON instances(project)`,

		// Recreate the timestamp index
		`CREATE INDEX IF NOT EXISTS idx_instances_timestamp ON instances(timestamp)`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			// If table rename fails (table doesn't exist yet), skip migration
			if stmt == `ALTER TABLE instances RENAME TO instances_old` {
				if isTableMissingError(err) {
					return nil
				}
			}
			return err
		}
	}

	return nil
}

// isTableMissingError checks if the error indicates a missing table.
func isTableMissingError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsSubstring(errStr, "no such table")
}
