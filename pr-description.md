# Cache System Revamp: JSON to SQLite Migration with Smart Learning

## Summary

This PR completely revamps the cache system, migrating from a JSON file-based cache to SQLite for better performance, reliability, and features. It also adds new CLI commands for cache management, enhances the project import workflow, and introduces a smart search affinity learning system that prioritizes projects based on your search patterns.

## Key Changes

### 1. SQLite Migration

**Before:** Cache stored in `~/.compass.cache.json` as a single JSON file
**After:** Cache stored in `~/.cache/compass/cache.db` as a SQLite database

Benefits:
- **Concurrent write handling** - No more full-file rewrites with mutex held during I/O
- **Efficient updates** - Single row updates instead of rewriting entire JSON file
- **Better query performance** - Indexed lookups for subnet IP matching
- **ACID compliance** - Safe writes, no corruption on crash
- **Scalability** - Handles larger datasets without loading everything into memory

The migration is automatic - existing JSON cache data is imported on first run.

### 2. New Cache CLI Commands

#### `compass cache info`
Display detailed cache information including size, entry counts, TTL settings, and optimization status.

#### `compass cache ttl`
Configure Time-To-Live settings per resource type:
```bash
compass cache ttl get                    # Show all TTL settings
compass cache ttl get instances          # Show specific TTL
compass cache ttl set instances 168h     # Set instance TTL to 7 days
compass cache ttl clear instances        # Reset to default
```

Supported TTL types: `global`, `instances`, `zones`, `projects`, `subnets`

#### `compass cache optimize`
Run database maintenance (VACUUM and ANALYZE) to reclaim disk space:
```bash
compass cache optimize
```
Shows before/after size and space saved. Runs automatically every 30 days.

#### `compass cache clear`
Remove all cached entries (preserves TTL settings).

#### `compass cache sql`
Execute custom SQL queries on the cache database:
```bash
compass cache sql "SELECT * FROM instances LIMIT 10"
compass cache sql -f query.sql
```

### 3. Enhanced Project Import

`compass gcp projects import` now scans and caches resources after selecting projects:

- **Zones** - Available zones per project
- **Instances** - All compute instances for faster SSH lookups
- **Subnets** - All subnets for faster IP lookups

This pre-populates the cache so subsequent commands run significantly faster.

**New: Regex filtering** - Import projects matching a pattern without interactive selection:
```bash
# Import all projects matching a regex pattern
compass gcp projects import --regex "^prod-.*"
compass gcp projects import -r "dev|staging"

# Case-insensitive matching
compass gcp projects import -r "(?i)^PROD-"
```

This is useful for automation and CI/CD pipelines where interactive selection isn't possible.

### 4. Performance Improvements

- **Prepared statements** - SQL queries use prepared statements for better performance
- **Batch operations** - `SetBatch()` and `RememberSubnetBatch()` for bulk inserts in transactions
- **CIDR range indexing** - Subnet IP lookups use indexed integer range queries instead of full table scans
- **WAL mode** - SQLite configured with Write-Ahead Logging for better concurrency

### 5. Reliability Improvements

- **Graceful degradation** - Cache failures don't break the application (no-op fallback)
- **Schema migrations** - Automatic migration between schema versions with backup
- **Stricter permissions** - Cache file created with 0600 permissions (owner read/write only)
- **Modular migration system** - Clean, maintainable migration architecture (see below)

### 6. Modular Migration Architecture

The database migration system uses a clean, interface-based design:

```
internal/cache/migrations/
├── registry.go      # Migration interface and registry
├── v2_cidr.go       # v2: Settings table + CIDR bounds columns
├── v3_usage.go      # v3: last_used columns for usage ordering
└── v4_affinity.go   # v4: search_affinity table
```

**Key features:**
- **Migration interface** - Each migration implements `Version()`, `Description()`, and `Up()` methods
- **Self-registering** - Migrations register themselves via `init()` functions
- **Idempotent** - Safe to run multiple times (handles "already exists" errors gracefully)
- **Centralized logging** - Migration progress logged from the runner, not individual migrations
- **Version tracking** - Schema version stored in metadata table for upgrade detection

### 7. Usage-Based Ordering

Resources and projects are now ordered by most recently used (MRU) rather than alphabetically:

- **`last_used` tracking** - Each instance, project, and subnet tracks when it was last accessed
- **Smart project ordering** - `GetProjectsByUsage()` returns projects by most recent use
- **Faster repeat searches** - Previously accessed projects are searched first

This makes repeat searches significantly faster by prioritizing projects you've used recently.

### 8. Search Affinity Learning (Schema v4)

The cache now learns from your search patterns to prioritize projects more intelligently:

- **Search affinity table** - Records which search terms find results in which projects
- **Prefix learning** - Extracts prefixes from search terms (e.g., "netmgt" from "netmgt-dt40")
- **Smart scoring** - Combines frequency, recency, and result volume for prioritization
- **Automatic cleanup** - Old affinity entries are cleaned up periodically

**How it works:**
1. When you search for "netmgt-dt40" and find results in "project-network", this association is recorded
2. The prefix "netmgt" is also associated with "project-network"
3. Future searches for anything starting with "netmgt" will prioritize "project-network"
4. The scoring uses:
   - Recency factor (half-life of ~7 days)
   - Frequency factor (logarithmic, with diminishing returns)
   - Volume factor (more results = slightly higher score)

### 9. Observability

- **Statistics tracking** - Hit/miss rates, operation counts, timing (shown in debug mode)
- **SQL query logging** - All SQL queries logged at trace level for debugging
- **Operation timing** - Each cache operation records duration for performance analysis

## Database Schema

```sql
-- Metadata and settings
CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

-- GCP resources (with last_used tracking)
CREATE TABLE instances (name TEXT PRIMARY KEY, timestamp INTEGER, project TEXT, zone TEXT,
                        last_used INTEGER, ...);
CREATE TABLE zones (project TEXT PRIMARY KEY, timestamp INTEGER, zones_json TEXT);
CREATE TABLE projects (name TEXT PRIMARY KEY, timestamp INTEGER, last_used INTEGER);
CREATE TABLE subnets (id INTEGER PRIMARY KEY, project TEXT, network TEXT, region TEXT, name TEXT,
                      primary_cidr TEXT, cidr_start_int INTEGER, cidr_end_int INTEGER,
                      last_used INTEGER, ...);

-- Search affinity learning (v4)
CREATE TABLE search_affinity (
    search_term TEXT NOT NULL,
    project TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    hit_count INTEGER DEFAULT 1,
    total_results INTEGER DEFAULT 0,
    last_hit INTEGER NOT NULL,
    PRIMARY KEY (search_term, project, resource_type)
);
```

## File Changes

| File | Description |
|------|-------------|
| `cmd/cache.go` | New CLI commands for cache management |
| `cmd/gcp_projects.go` | Enhanced import with resource scanning and regex filtering |
| `cmd/gcp_projects_test.go` | Tests for regex filtering functionality |
| `cmd/gcp_search.go` | Search with affinity-based project prioritization |
| `internal/cache/cache.go` | Rewritten for SQLite with prepared statements and last_used tracking |
| `internal/cache/schema.go` | Database schema initialization and migration runner |
| `internal/cache/migrations/registry.go` | Migration interface and self-registering registry |
| `internal/cache/migrations/v2_cidr.go` | v2: Settings table + CIDR bounds columns |
| `internal/cache/migrations/v3_usage.go` | v3: last_used columns for usage ordering |
| `internal/cache/migrations/v4_affinity.go` | v4: search_affinity table |
| `internal/cache/migrate.go` | JSON to SQLite migration logic |
| `internal/cache/affinity.go` | Search affinity learning system |
| `internal/cache/settings.go` | TTL configuration management |
| `internal/cache/stats.go` | Statistics tracking |
| `internal/cache/optimize.go` | Database optimization (VACUUM/ANALYZE) |
| `internal/gcp/scan.go` | Resource scanning for project import |
| `internal/tui/search_view.go` | TUI search with affinity integration |

## Migration Path

1. On first run after update, the new cache system detects `~/.compass.cache.json`
2. Creates new SQLite database at `~/.cache/compass/cache.db`
3. Imports all data from JSON into SQLite
4. Renames old JSON file to `~/.compass.cache.json.migrated`
5. Future runs use SQLite directly

## Testing

- All existing tests updated and passing
- New tests for migration, schema, settings, and statistics
- New tests for search affinity learning system
- Manual testing of CLI commands

## Breaking Changes

None - the migration is automatic and the public API is unchanged. Database schema migrations (v1 → v4) are handled automatically with backups.
