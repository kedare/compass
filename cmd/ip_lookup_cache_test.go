package cmd

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/kedare/compass/internal/cache"
	"github.com/stretchr/testify/require"
)

func TestPreferredProjectsForIP_UsesCachedSubnets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cache.SetEnabled(true)

	c, err := cache.New()
	require.NoError(t, err)

	origLoad := loadCacheFunc
	defer func() { loadCacheFunc = origLoad }()
	loadCacheFunc = func() (*cache.Cache, error) {
		return c, nil
	}

	entry := &cache.SubnetEntry{
		Project:     "test-project",
		Network:     "test-network",
		Region:      "us-central1",
		Name:        "subnet-a",
		PrimaryCIDR: "10.203.32.0/24",
	}

	err = c.RememberSubnet(entry)
	require.NoError(t, err)

	ip := net.ParseIP("10.203.32.12")
	require.NotNil(t, ip)

	projects := preferredProjectsForIP(ip)
	require.Len(t, projects, 1)
	require.Equal(t, "test-project", projects[0])

	cacheFile := filepath.Join(home, cache.CacheFileName)
	info, err := os.Stat(cacheFile)
	require.NoError(t, err)
	require.False(t, info.IsDir(), "expected cache file, found directory: %s", cacheFile)
}
