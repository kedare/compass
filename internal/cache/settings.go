package cache

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/kedare/compass/internal/logger"
)

// TTLType represents a resource type for TTL configuration.
type TTLType string

const (
	// TTLTypeGlobal is the default TTL for all resource types.
	TTLTypeGlobal TTLType = "global"
	// TTLTypeInstances is the TTL for instance entries.
	TTLTypeInstances TTLType = "instances"
	// TTLTypeZones is the TTL for zone listings.
	TTLTypeZones TTLType = "zones"
	// TTLTypeProjects is the TTL for project entries.
	TTLTypeProjects TTLType = "projects"
	// TTLTypeSubnets is the TTL for subnet entries.
	TTLTypeSubnets TTLType = "subnets"
)

// ValidTTLTypes returns all valid TTL types.
func ValidTTLTypes() []TTLType {
	return []TTLType{
		TTLTypeGlobal,
		TTLTypeInstances,
		TTLTypeZones,
		TTLTypeProjects,
		TTLTypeSubnets,
	}
}

// IsValidTTLType checks if the given type is valid.
func IsValidTTLType(t string) bool {
	for _, valid := range ValidTTLTypes() {
		if string(valid) == t {
			return true
		}
	}

	return false
}

// settingKey returns the settings key for a TTL type.
func settingKey(t TTLType) string {
	return fmt.Sprintf("ttl_%s", t)
}

// GetTTL returns the TTL for a specific resource type.
// Falls back to global TTL if type-specific is not set.
// Falls back to default TTL if neither is set.
func (c *Cache) GetTTL(t TTLType) time.Duration {
	if c == nil || c.db == nil {
		return DefaultCacheExpiry
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("GetTTL", time.Since(start))
	}()

	// Try type-specific TTL first
	if t != TTLTypeGlobal {
		ttl, err := c.getTTLFromDB(t)
		if err == nil && ttl > 0 {
			return ttl
		}
	}

	// Fall back to global TTL
	ttl, err := c.getTTLFromDB(TTLTypeGlobal)
	if err == nil && ttl > 0 {
		return ttl
	}

	// Fall back to default
	return DefaultCacheExpiry
}

// SetTTL sets the TTL for a specific resource type.
func (c *Cache) SetTTL(t TTLType, ttl time.Duration) error {
	if c == nil || c.db == nil {
		return ErrCacheDisabled
	}

	start := time.Now()
	defer func() {
		c.stats.recordOperation("SetTTL", time.Since(start))
	}()

	key := settingKey(t)
	value := strconv.FormatInt(int64(ttl.Seconds()), 10)

	_, err := c.exec(
		`INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set TTL for %s: %w", t, err)
	}

	logger.Log.Debugf("Set TTL for %s to %v", t, ttl)

	return nil
}

// GetAllTTLs returns all configured TTLs.
func (c *Cache) GetAllTTLs() map[TTLType]time.Duration {
	result := make(map[TTLType]time.Duration)

	if c == nil || c.db == nil {
		return result
	}

	for _, t := range ValidTTLTypes() {
		ttl, err := c.getTTLFromDB(t)
		if err == nil && ttl > 0 {
			result[t] = ttl
		}
	}

	return result
}

// ClearTTL removes the TTL setting for a specific type.
func (c *Cache) ClearTTL(t TTLType) error {
	if c == nil || c.db == nil {
		return ErrCacheDisabled
	}

	key := settingKey(t)

	_, err := c.exec(`DELETE FROM settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("failed to clear TTL for %s: %w", t, err)
	}

	logger.Log.Debugf("Cleared TTL for %s", t)

	return nil
}

// getTTLFromDB retrieves a TTL value from the database.
func (c *Cache) getTTLFromDB(t TTLType) (time.Duration, error) {
	key := settingKey(t)
	var value string

	err := c.queryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}

	return time.Duration(seconds) * time.Second, nil
}

// getEffectiveTTL returns the effective TTL for a resource type,
// considering type-specific, global, and default values.
func (c *Cache) getEffectiveTTL(t TTLType) time.Duration {
	if c == nil || c.db == nil {
		return DefaultCacheExpiry
	}

	return c.GetTTL(t)
}
