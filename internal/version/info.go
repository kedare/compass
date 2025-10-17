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

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}

	return value
}
