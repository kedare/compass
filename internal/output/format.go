package output

import (
	"os"
	"strings"
)

// DefaultFormat returns the preferred output format unless COMPASS_OUTPUT is set to a supported value.
func DefaultFormat(preferred string, allowed []string) string {
	env := strings.TrimSpace(os.Getenv("COMPASS_OUTPUT"))
	if env == "" {
		return preferred
	}

	env = strings.ToLower(env)
	for _, option := range allowed {
		if env == option {
			return env
		}
	}

	return preferred
}
