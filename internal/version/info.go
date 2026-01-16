package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

const (
	// GitHubRepo is the repository path for checking updates
	GitHubRepo = "kedare/compass"
	// GitHubAPIURL is the base URL for GitHub API
	GitHubAPIURL = "https://api.github.com"
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

// RelativeTime returns a human-readable relative time string for the build date.
// Returns empty string if the date cannot be parsed.
func (i Info) RelativeTime() string {
	if i.BuildDate == "unknown" || i.BuildDate == "" {
		return ""
	}

	// Try parsing common date formats
	formats := []string{
		"2006-01-02",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		time.RFC3339,
	}

	var buildTime time.Time
	var err error
	for _, format := range formats {
		buildTime, err = time.Parse(format, i.BuildDate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return ""
	}

	return humanize.Time(buildTime)
}

// githubRelease represents a GitHub release from the API
type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckForUpdate checks GitHub for a newer version.
// Returns the latest version tag and URL if a newer version is available, or empty strings if current is latest.
// Returns an error if the check fails.
func CheckForUpdate() (latestVersion string, url string, err error) {
	currentVersion := Get().Version
	if currentVersion == "dev" || currentVersion == "unknown" {
		return "", "", nil // Skip check for dev builds
	}

	client := &http.Client{Timeout: 5 * time.Second}
	apiURL := fmt.Sprintf("%s/repos/%s/releases/latest", GitHubAPIURL, GitHubRepo)

	resp, err := client.Get(apiURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to check for updates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to check for updates: HTTP %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("failed to parse release info: %w", err)
	}

	latestTag := strings.TrimPrefix(release.TagName, "v")
	currentTag := strings.TrimPrefix(currentVersion, "v")

	if isNewerVersion(latestTag, currentTag) {
		return release.TagName, release.HTMLURL, nil
	}

	return "", "", nil
}

// isNewerVersion returns true if latest is newer than current.
// Handles semver-like versions (e.g., "1.2.3").
func isNewerVersion(latest, current string) bool {
	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(current, ".")

	// Pad shorter version with zeros
	maxLen := len(latestParts)
	if len(currentParts) > maxLen {
		maxLen = len(currentParts)
	}
	for len(latestParts) < maxLen {
		latestParts = append(latestParts, "0")
	}
	for len(currentParts) < maxLen {
		currentParts = append(currentParts, "0")
	}

	for i := 0; i < maxLen; i++ {
		latestNum, _ := strconv.Atoi(latestParts[i])
		currentNum, _ := strconv.Atoi(currentParts[i])

		if latestNum > currentNum {
			return true
		}
		if latestNum < currentNum {
			return false
		}
	}

	return false // versions are equal
}
