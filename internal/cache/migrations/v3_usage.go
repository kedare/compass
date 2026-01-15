package migrations

import (
	"database/sql"
)

func init() {
	Register(&v3Usage{})
}

// v3Usage adds last_used columns for usage-based ordering.
type v3Usage struct{}

func (m *v3Usage) Version() int {
	return 3
}

func (m *v3Usage) Description() string {
	return "Add last_used columns for usage-based ordering"
}

func (m *v3Usage) Up(db *sql.DB) error {
	// Add columns and indexes
	statements := []string{
		`ALTER TABLE instances ADD COLUMN last_used INTEGER`,
		`ALTER TABLE projects ADD COLUMN last_used INTEGER`,
		`ALTER TABLE subnets ADD COLUMN last_used INTEGER`,
		`CREATE INDEX IF NOT EXISTS idx_instances_last_used ON instances(last_used)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_last_used ON projects(last_used)`,
		`CREATE INDEX IF NOT EXISTS idx_subnets_last_used ON subnets(last_used)`,
	}

	if err := ExecStatements(db, statements); err != nil {
		return err
	}

	// Initialize last_used to timestamp for existing rows (best effort)
	initStatements := []string{
		`UPDATE instances SET last_used = timestamp WHERE last_used IS NULL`,
		`UPDATE projects SET last_used = timestamp WHERE last_used IS NULL`,
		`UPDATE subnets SET last_used = timestamp WHERE last_used IS NULL`,
	}

	// These are best-effort, don't fail if they error
	for _, stmt := range initStatements {
		_, _ = db.Exec(stmt)
	}

	return nil
}
