package cache

import "sync"

var (
	cacheEnabled = true
	flagMu       sync.RWMutex
)

// Enabled reports whether cache usage is enabled for the current process.
func Enabled() bool {
	flagMu.RLock()
	defer flagMu.RUnlock()

	return cacheEnabled
}

// SetEnabled toggles cache reads and writes globally.
func SetEnabled(enabled bool) {
	flagMu.Lock()
	cacheEnabled = enabled
	flagMu.Unlock()
}
