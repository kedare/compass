package migrations

import (
	"database/sql"
)

func init() {
	Register(&v5MIGName{})
}

// v5MIGName adds mig_name column to instances table to track MIG membership.
type v5MIGName struct{}

func (m *v5MIGName) Version() int {
	return 5
}

func (m *v5MIGName) Description() string {
	return "Add mig_name column to instances table for MIG membership tracking"
}

func (m *v5MIGName) Up(db *sql.DB) error {
	statements := []string{
		`ALTER TABLE instances ADD COLUMN mig_name TEXT`,
	}

	return ExecStatements(db, statements)
}
