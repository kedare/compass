package migrations

import (
	"database/sql"
	"net"
)

func init() {
	Register(&v2CIDR{})
}

// v2CIDR adds settings table and CIDR bounds columns to subnets.
type v2CIDR struct{}

func (m *v2CIDR) Version() int {
	return 2
}

func (m *v2CIDR) Description() string {
	return "Add settings table and CIDR bounds columns for faster IP lookups"
}

func (m *v2CIDR) Up(db *sql.DB) error {
	statements := []string{
		// Create settings table
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		// Add CIDR bounds columns to subnets
		`ALTER TABLE subnets ADD COLUMN cidr_start_int INTEGER`,
		`ALTER TABLE subnets ADD COLUMN cidr_end_int INTEGER`,
		// Create index for CIDR range queries
		`CREATE INDEX IF NOT EXISTS idx_subnets_cidr_range ON subnets(cidr_start_int, cidr_end_int)`,
	}

	if err := ExecStatements(db, statements); err != nil {
		return err
	}

	// Update existing subnets with CIDR bounds (best effort)
	_ = m.updateExistingCIDRBounds(db)

	return nil
}

// updateExistingCIDRBounds populates CIDR bounds for existing subnet rows.
func (m *v2CIDR) updateExistingCIDRBounds(db *sql.DB) error {
	query := `SELECT id, primary_cidr FROM subnets WHERE cidr_start_int IS NULL AND primary_cidr IS NOT NULL AND primary_cidr != ''`

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

		_, _ = db.Exec(updateQuery, start, end, id)
	}

	return rows.Err()
}

// cidrToIntRange converts a CIDR string to integer range for fast lookups.
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

	// Convert IP to integer
	start := int64(ip4[0])<<24 | int64(ip4[1])<<16 | int64(ip4[2])<<8 | int64(ip4[3])

	// Calculate end of range using mask
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return 0, 0
	}

	hostBits := uint(32 - ones)
	end := start | ((1 << hostBits) - 1)

	return start, end
}
