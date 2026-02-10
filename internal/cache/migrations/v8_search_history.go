package migrations

import (
	"database/sql"
)

func init() {
	Register(&v8SearchHistory{})
}

// v8SearchHistory adds search history table for storing recent searches.
type v8SearchHistory struct{}

func (m *v8SearchHistory) Version() int {
	return 8
}

func (m *v8SearchHistory) Description() string {
	return "Add search history table for recent searches"
}

func (m *v8SearchHistory) Up(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS search_history (
			search_term TEXT PRIMARY KEY,
			last_used INTEGER NOT NULL,
			use_count INTEGER DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_search_history_last_used ON search_history(last_used DESC)`,
	}

	return ExecStatements(db, statements)
}
