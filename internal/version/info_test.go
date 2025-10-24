package version

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetUsesFallbacks(t *testing.T) {
	orig := []string{Version, Commit, BuildDate, BuildUser, BuildHost, BuildArch}
	Version, Commit, BuildDate, BuildUser, BuildHost, BuildArch = "", "", "", "", "", ""
	t.Cleanup(func() {
		Version, Commit, BuildDate, BuildUser, BuildHost, BuildArch = orig[0], orig[1], orig[2], orig[3], orig[4], orig[5]
	})

	info := Get()

	require.Equal(t, "dev", info.Version)
	require.Equal(t, "unknown", info.Commit)
	require.Equal(t, "unknown", info.BuildDate)
	require.Equal(t, "unknown", info.BuildUser)
	require.Equal(t, "unknown", info.BuildHost)
	require.Equal(t, runtime.GOOS+"/"+runtime.GOARCH, info.BuildArch)
	require.Equal(t, runtime.Version(), info.GoVersion)
}

func TestGetRespectsProvidedValues(t *testing.T) {
	orig := []string{Version, Commit, BuildDate, BuildUser, BuildHost, BuildArch}
	Version, Commit, BuildDate, BuildUser, BuildHost, BuildArch = "1.2.3", "abcd123", "2025-01-01", "tester", "host", "custom/arch"
	t.Cleanup(func() {
		Version, Commit, BuildDate, BuildUser, BuildHost, BuildArch = orig[0], orig[1], orig[2], orig[3], orig[4], orig[5]
	})

	info := Get()

	require.Equal(t, "1.2.3", info.Version)
	require.Equal(t, "abcd123", info.Commit)
	require.Equal(t, "2025-01-01", info.BuildDate)
	require.Equal(t, "tester", info.BuildUser)
	require.Equal(t, "host", info.BuildHost)
	require.Equal(t, "custom/arch", info.BuildArch)
}

func TestFallback(t *testing.T) {
	require.Equal(t, "default", fallback("", "default"))
	require.Equal(t, "value", fallback(" value ", "default"))
}
