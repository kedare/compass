package cmd

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/kedare/compass/internal/cache"
)

func TestPreferredProjectsForIP_UsesCachedSubnets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cache.SetEnabled(true)

	c, err := cache.New()
	if err != nil {
		t.Fatalf("cache.New(): %v", err)
	}

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

	if err := c.RememberSubnet(entry); err != nil {
		t.Fatalf("RememberSubnet(): %v", err)
	}

	ip := net.ParseIP("10.203.32.12")
	if ip == nil {
		t.Fatal("failed to parse IP")
	}

	projects := preferredProjectsForIP(ip)
	if len(projects) != 1 || projects[0] != "test-project" {
		t.Fatalf("expected cached project, got %v", projects)
	}

	cacheFile := filepath.Join(home, cache.CacheFileName)
	if info, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("expected cache file saved: %v", err)
	} else if info.IsDir() {
		t.Fatalf("expected cache file, found directory: %s", cacheFile)
	}
}
