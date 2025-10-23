package update

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAssetForCurrentPlatform verifies that the selector chooses the asset matching the runtime platform.
func TestAssetForCurrentPlatform(t *testing.T) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	expectedName := fmt.Sprintf("compass-%s-%s", goos, goarch)

	release := &Release{
		TagName: "v1.2.3",
		Assets: []Asset{
			{
				Name:        expectedName,
				DownloadURL: "https://example.com/download",
				Size:        42,
			},
		},
	}

	asset, err := AssetForCurrentPlatform(release)
	require.NoError(t, err)
	require.Equal(t, expectedName, asset.Name)
}

// TestAssetForCurrentPlatformErr ensures an error is returned when no matching asset is available.
func TestAssetForCurrentPlatformErr(t *testing.T) {
	release := &Release{
		TagName: "v1.2.3",
		Assets: []Asset{
			{Name: "compass-linux-arm64"},
		},
	}

	_, err := AssetForCurrentPlatform(release)
	require.Error(t, err)
}

// TestShouldUpdate covers the rules that decide whether an update should be applied.
func TestShouldUpdate(t *testing.T) {
	testCases := []struct {
		name     string
		current  string
		latest   string
		expected bool
	}{
		{name: "same version", current: "v1.0.0", latest: "v1.0.0", expected: false},
		{name: "latest newer", current: "1.0.0", latest: "v1.1.0", expected: true},
		{name: "current newer", current: "v1.2.0", latest: "v1.1.0", expected: false},
		{name: "current dev", current: "dev", latest: "v0.1.0", expected: true},
		{name: "latest invalid", current: "v1.0.0", latest: "not-semver", expected: true},
		{name: "prerelease lower precedence", current: "1.0.0", latest: "1.0.0-rc1", expected: false},
		{name: "prerelease to release", current: "1.0.0-rc1", latest: "1.0.0", expected: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldUpdate(tc.current, tc.latest)
			require.Equal(t, tc.expected, got)
		})
	}
}
