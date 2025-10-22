package output

import (
	"os"
	"strings"
	"sync"
)

var (
	currentFormat   string
	formatMutex     sync.RWMutex
	jsonModeEnabled bool
)

// DefaultFormat returns the preferred output format unless COMPASS_OUTPUT is set to a supported value.
// It also tracks the selected format globally to allow spinners and other UI elements to adapt.
func DefaultFormat(preferred string, allowed []string) string {
	env := strings.TrimSpace(os.Getenv("COMPASS_OUTPUT"))
	if env == "" {
		setCurrentFormat(preferred)
		return preferred
	}

	env = strings.ToLower(env)
	for _, option := range allowed {
		if env == option {
			setCurrentFormat(env)
			return env
		}
	}

	setCurrentFormat(preferred)
	return preferred
}

// setCurrentFormat stores the current output format and determines if JSON mode is active.
func setCurrentFormat(format string) {
	formatMutex.Lock()
	defer formatMutex.Unlock()
	currentFormat = format
	jsonModeEnabled = (format == "json")
}

// SetFormat explicitly sets the output format and updates JSON mode detection.
// This should be called when the actual output format is determined (e.g., from flags).
func SetFormat(format string) {
	setCurrentFormat(format)
}

// IsJSONMode returns true if the current output format is JSON.
// When in JSON mode, spinners and progress bars should be disabled to avoid
// corrupting the JSON output stream.
func IsJSONMode() bool {
	formatMutex.RLock()
	defer formatMutex.RUnlock()
	return jsonModeEnabled
}
