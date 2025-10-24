package version

import (
	"runtime"
	"strings"
)

// The following variables can be overridden at build time using -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
	BuildUser = "unknown"
	BuildHost = "unknown"
	BuildArch = ""
)

// Info contains metadata about the compiled binary.
type Info struct {
	Version   string
	Commit    string
	BuildDate string
	BuildUser string
	BuildHost string
	BuildArch string
	GoVersion string
}

// Get returns build metadata, normalizing defaults where necessary.
func Get() Info {
	arch := strings.TrimSpace(BuildArch)
	if arch == "" {
		arch = runtime.GOOS + "/" + runtime.GOARCH
	}

	return Info{
		Version:   fallback(Version, "dev"),
		Commit:    fallback(Commit, "unknown"),
		BuildDate: fallback(BuildDate, "unknown"),
		BuildUser: fallback(BuildUser, "unknown"),
		BuildHost: fallback(BuildHost, "unknown"),
		BuildArch: arch,
		GoVersion: runtime.Version(),
	}
}

// fallback returns the trimmed value or the provided default when the value is empty.
func fallback(value, defaultValue string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return defaultValue
	}

	return trimmed
}
