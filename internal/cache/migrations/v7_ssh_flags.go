package migrations

import (
	"database/sql"
)

func init() {
	Register(&v7SSHFlags{})
}

// v7SSHFlags adds an ssh_flags column to the instances table
// to persist user-defined SSH flags per instance.
type v7SSHFlags struct{}

func (m *v7SSHFlags) Version() int {
	return 7
}

func (m *v7SSHFlags) Description() string {
	return "Add ssh_flags column to instances table"
}

func (m *v7SSHFlags) Up(db *sql.DB) error {
	return ExecStatements(db, []string{
		`ALTER TABLE instances ADD COLUMN ssh_flags TEXT`,
	})
}
