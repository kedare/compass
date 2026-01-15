package migrations

import (
	"database/sql"
)

func init() {
	Register(&v4Affinity{})
}

// v4Affinity adds search affinity table for learning search patterns.
type v4Affinity struct{}

func (m *v4Affinity) Version() int {
	return 4
}

func (m *v4Affinity) Description() string {
	return "Add search affinity table for learning search patterns"
}

func (m *v4Affinity) Up(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS search_affinity (
			search_term TEXT NOT NULL,
			project TEXT NOT NULL,
			resource_type TEXT NOT NULL DEFAULT '',
			hit_count INTEGER DEFAULT 1,
			total_results INTEGER DEFAULT 0,
			last_hit INTEGER NOT NULL,
			PRIMARY KEY (search_term, project, resource_type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_search_affinity_term ON search_affinity(search_term)`,
		`CREATE INDEX IF NOT EXISTS idx_search_affinity_last_hit ON search_affinity(last_hit)`,
	}

	return ExecStatements(db, statements)
}
