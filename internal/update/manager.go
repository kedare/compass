package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGitHubAPI = "https://api.github.com"
	defaultUserAgent = "compass-update"
)

// Manager coordinates update operations against a GitHub repository.
type Manager struct {
	client    *http.Client
	owner     string
	repo      string
	baseURL   string
	userAgent string
}

// Release contains enough metadata to select an artifact for download.
type Release struct {
	TagName string
	Assets  []Asset
}

// Asset describes a single downloadable artifact attached to a release.
type Asset struct {
	Name        string
	DownloadURL string
	Size        int64
}

// Option customizes the behaviour of a Manager during construction.
type Option func(*Manager)

// WithBaseURL overrides the GitHub API base URL, primarily for testing.
func WithBaseURL(baseURL string) Option {
	return func(m *Manager) {
		m.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithUserAgent overrides the HTTP user agent header.
func WithUserAgent(agent string) Option {
	return func(m *Manager) {
		if strings.TrimSpace(agent) != "" {
			m.userAgent = agent
		}
	}
}

// NewManager builds a Manager that targets the provided repository.
func NewManager(client *http.Client, owner, repo string, opts ...Option) *Manager {
	httpClient := client
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	manager := &Manager{
		client:    httpClient,
		owner:     owner,
		repo:      repo,
		baseURL:   defaultGitHubAPI,
		userAgent: defaultUserAgent,
	}

	for _, opt := range opts {
		opt(manager)
	}

	return manager
}

// LatestRelease retrieves metadata for the newest published GitHub release.
func (m *Manager) LatestRelease(ctx context.Context) (*Release, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", m.baseURL, m.owner, m.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create release request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	if m.userAgent != "" {
		req.Header.Set("User-Agent", m.userAgent)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release metadata: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status fetching release: %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode release metadata: %w", err)
	}

	release := &Release{
		TagName: payload.TagName,
		Assets:  make([]Asset, 0, len(payload.Assets)),
	}

	for _, asset := range payload.Assets {
		release.Assets = append(release.Assets, Asset{
			Name:        asset.Name,
			DownloadURL: asset.BrowserDownloadURL,
			Size:        asset.Size,
		})
	}

	return release, nil
}

// DownloadAsset initiates the HTTP download for the provided asset.
func (m *Manager) DownloadAsset(ctx context.Context, asset Asset) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	req.Header.Set("Accept", "application/octet-stream")
	if m.userAgent != "" {
		req.Header.Set("User-Agent", m.userAgent)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download asset: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer func() {
			_ = resp.Body.Close()
		}()
		return nil, fmt.Errorf("unexpected download status for %s: %s", asset.Name, resp.Status)
	}

	return resp, nil
}

// ErrNoMatchingAsset indicates that no release asset matches the current platform.
var ErrNoMatchingAsset = errors.New("no release asset found for the current platform")

// AssetForCurrentPlatform selects the release asset whose name matches the runtime OS and architecture.
func AssetForCurrentPlatform(release *Release) (Asset, error) {
	if release == nil {
		return Asset{}, fmt.Errorf("release cannot be nil")
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	candidates := expectedAssetNames(goos, goarch)
	for _, candidate := range candidates {
		for _, asset := range release.Assets {
			if asset.Name == candidate {
				return asset, nil
			}
		}
	}

	for _, asset := range release.Assets {
		if assetMatchesPlatform(asset.Name, goos, goarch) {
			return asset, nil
		}
	}

	return Asset{}, ErrNoMatchingAsset
}

// ShouldUpdate compares semantic versions and determines if an update is available.
func ShouldUpdate(currentVersion, latestVersion string) bool {
	current := normalizeVersion(currentVersion)
	latest := normalizeVersion(latestVersion)

	if latest == "" {
		return false
	}

	currentSem, currentValid := parseSemVersion(current)
	latestSem, latestValid := parseSemVersion(latest)

	switch {
	case latestValid && currentValid:
		return latestSem.Compare(currentSem) > 0
	case latestValid && !currentValid:
		return true
	case !latestValid:
		return strings.TrimSpace(currentVersion) != strings.TrimSpace(latestVersion)
	default:
		return false
	}
}

// normalizeVersion removes common prefixes while keeping prerelease metadata intact.
func normalizeVersion(version string) string {
	return strings.TrimSpace(strings.TrimPrefix(version, "v"))
}

// assetMatchesPlatform checks if an asset name corresponds to the provided OS/architecture pair.
func assetMatchesPlatform(name, goos, goarch string) bool {
	return strings.HasPrefix(name, "compass") &&
		strings.Contains(name, goos) &&
		strings.Contains(name, goarch) &&
		(filepath.Ext(name) == "" || goos == "windows" || strings.HasSuffix(name, ".exe"))
}

// expectedAssetNames returns likely asset names for the given OS/architecture combo.
func expectedAssetNames(goos, goarch string) []string {
	base := fmt.Sprintf("compass-%s-%s", goos, goarch)
	names := []string{base}
	if goos == "windows" {
		names = append([]string{base + ".exe"}, names...)
	}

	return names
}

// semVersion tracks semantic version components relevant for precedence checks.
type semVersion struct {
	major      int
	minor      int
	patch      int
	preRelease string
}

// Compare implements semantic version ordering, ignoring build metadata.
func (s semVersion) Compare(other semVersion) int {
	if s.major != other.major {
		return compareInt(s.major, other.major)
	}
	if s.minor != other.minor {
		return compareInt(s.minor, other.minor)
	}
	if s.patch != other.patch {
		return compareInt(s.patch, other.patch)
	}

	if s.preRelease == other.preRelease {
		return 0
	}

	if s.preRelease == "" {
		return 1
	}
	if other.preRelease == "" {
		return -1
	}

	return strings.Compare(s.preRelease, other.preRelease)
}

// parseSemVersion extracts major/minor/patch and prerelease information from a semantic version string.
func parseSemVersion(version string) (semVersion, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return semVersion{}, false
	}

	withoutBuild := version
	if idx := strings.Index(version, "+"); idx != -1 {
		withoutBuild = version[:idx]
	}

	var preRelease string
	base := withoutBuild
	if idx := strings.Index(base, "-"); idx != -1 {
		preRelease = base[idx+1:]
		base = base[:idx]
	}

	segments := strings.Split(base, ".")
	if len(segments) == 0 {
		return semVersion{}, false
	}

	for len(segments) < 3 {
		segments = append(segments, "0")
	}

	major, err := strconv.Atoi(segments[0])
	if err != nil {
		return semVersion{}, false
	}

	minor, err := strconv.Atoi(segments[1])
	if err != nil {
		return semVersion{}, false
	}

	patch, err := strconv.Atoi(segments[2])
	if err != nil {
		return semVersion{}, false
	}

	return semVersion{
		major:      major,
		minor:      minor,
		patch:      patch,
		preRelease: preRelease,
	}, true
}

// compareInt returns semantic ordering for integers.
func compareInt(a, b int) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	default:
		return 0
	}
}
