# Cache System Revamp: JSON to SQLite Migration

## Summary

This PR completely revamps the cache system, migrating from a JSON file-based cache to SQLite for better performance, reliability, and features. It also adds new CLI commands for cache management and enhances the project import workflow.

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

### 4. Performance Improvements

- **Prepared statements** - SQL queries use prepared statements for better performance
- **Batch operations** - `SetBatch()` and `RememberSubnetBatch()` for bulk inserts in transactions
- **CIDR range indexing** - Subnet IP lookups use indexed integer range queries instead of full table scans
- **WAL mode** - SQLite configured with Write-Ahead Logging for better concurrency

### 5. Reliability Improvements

- **Graceful degradation** - Cache failures don't break the application (no-op fallback)
- **Schema migrations** - Automatic migration between schema versions with backup
- **Stricter permissions** - Cache file created with 0600 permissions (owner read/write only)

### 6. Observability

- **Statistics tracking** - Hit/miss rates, operation counts, timing (shown in debug mode)
- **SQL query logging** - All SQL queries logged at trace level for debugging
- **Operation timing** - Each cache operation records duration for performance analysis

## Database Schema

```sql
-- Metadata and settings
CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

-- GCP resources
CREATE TABLE instances (name TEXT PRIMARY KEY, timestamp INTEGER, project TEXT, zone TEXT, ...);
CREATE TABLE zones (project TEXT PRIMARY KEY, timestamp INTEGER, zones_json TEXT);
CREATE TABLE projects (name TEXT PRIMARY KEY, timestamp INTEGER);
CREATE TABLE subnets (id INTEGER PRIMARY KEY, project TEXT, network TEXT, region TEXT, name TEXT,
                      primary_cidr TEXT, cidr_start_int INTEGER, cidr_end_int INTEGER, ...);
```

## File Changes

| File | Description |
|------|-------------|
| `cmd/cache.go` | New CLI commands for cache management |
| `cmd/gcp_projects.go` | Enhanced import with resource scanning |
| `internal/cache/cache.go` | Rewritten for SQLite with prepared statements |
| `internal/cache/schema.go` | Database schema and migrations |
| `internal/cache/migrate.go` | JSON to SQLite migration logic |
| `internal/cache/settings.go` | TTL configuration management |
| `internal/cache/stats.go` | Statistics tracking |
| `internal/cache/optimize.go` | Database optimization (VACUUM/ANALYZE) |
| `internal/gcp/scan.go` | Resource scanning for project import |

## Migration Path

1. On first run after update, the new cache system detects `~/.compass.cache.json`
2. Creates new SQLite database at `~/.cache/compass/cache.db`
3. Imports all data from JSON into SQLite
4. Renames old JSON file to `~/.compass.cache.json.migrated`
5. Future runs use SQLite directly

## Testing

- All existing tests updated and passing
- New tests for migration, schema, settings, and statistics
- Manual testing of CLI commands

## Breaking Changes

None - the migration is automatic and the public API is unchanged.
