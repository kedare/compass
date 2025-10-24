package cmd

import (
	"context"
	"testing"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// stubCompletionCache implements completionCache for tests.
type stubCompletionCache struct {
	projects  []string
	locations map[string][]cache.CachedLocation
}

func (s *stubCompletionCache) GetProjects() []string {
	if s == nil {
		return nil
	}

	out := make([]string, len(s.projects))
	copy(out, s.projects)

	return out
}

func (s *stubCompletionCache) GetLocationsByProject(project string) ([]cache.CachedLocation, bool) {
	if s == nil {
		return nil, false
	}

	entries, ok := s.locations[project]
	if !ok {
		return nil, false
	}

	out := make([]cache.CachedLocation, len(entries))
	copy(out, entries)

	return out, true
}

// stubConnectivityClient implements connectivityClient to supply canned results.
type stubConnectivityClient struct {
	tests []*gcp.ConnectivityTestResult
	err   error
}

func (s *stubConnectivityClient) ListTests(context.Context, string) ([]*gcp.ConnectivityTestResult, error) {
	return s.tests, s.err
}

// stubComputeClient implements computeClient for completion tests.
type stubComputeClient struct {
	instances []*gcp.Instance
	migs      map[string][]gcp.ManagedInstanceGroup
	zones     []string
	regions   []string
}

func (s *stubComputeClient) ListInstances(_ context.Context, zone string) ([]*gcp.Instance, error) {
	if s == nil {
		return nil, nil
	}

	if zone == "" {
		return s.instances, nil
	}

	filtered := make([]*gcp.Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		if inst == nil {
			continue
		}

		if inst.Zone == zone {
			filtered = append(filtered, inst)
		}
	}

	return filtered, nil
}

func (s *stubComputeClient) ListManagedInstanceGroups(_ context.Context, location string) ([]gcp.ManagedInstanceGroup, error) {
	if s == nil || s.migs == nil {
		return nil, nil
	}

	if location == "" {
		var all []gcp.ManagedInstanceGroup
		for _, groups := range s.migs {
			all = append(all, groups...)
		}

		return all, nil
	}

	return s.migs[location], nil
}

func (s *stubComputeClient) ListZones(context.Context) ([]string, error) {
	return s.zones, nil
}

func (s *stubComputeClient) ListRegions(context.Context) ([]string, error) {
	return s.regions, nil
}

func TestGcpProjectCompletionUsesCacheAndFlag(t *testing.T) {
	origLoader := loadCompletionCache
	loadCompletionCache = func() (completionCache, error) {
		return &stubCompletionCache{
			projects: []string{"alpha", "beta"},
		}, nil
	}
	t.Cleanup(func() { loadCompletionCache = origLoader })

	origProject := project
	project = "default"
	t.Cleanup(func() { project = origProject })

	cmd := &cobra.Command{}
	cmd.Flags().String("project", "", "")
	require.NoError(t, cmd.Flags().Set("project", "gamma"))

	results, directive := gcpProjectCompletion(cmd, nil, "")
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	require.ElementsMatch(t, []string{"alpha", "beta", "gamma"}, results)
}

func TestGcpConnectivityTestNameCompletion(t *testing.T) {
	origFactory := newConnectivityClientFunc
	newConnectivityClientFunc = func(context.Context, string) (connectivityClient, error) {
		return &stubConnectivityClient{
			tests: []*gcp.ConnectivityTestResult{
				{Name: "tests/default"},
				{Name: "tests/friendly", DisplayName: "Friendly Name"},
			},
		}, nil
	}
	t.Cleanup(func() { newConnectivityClientFunc = origFactory })

	cmd := &cobra.Command{}
	cmd.Flags().String("project", "", "")

	results, directive := gcpConnectivityTestNameCompletion(cmd, nil, "tests/")
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	require.ElementsMatch(t, []string{
		"tests/default",
		"tests/friendly\tFriendly Name",
	}, results)
}

func TestGcpSSHCompletionUsesClientData(t *testing.T) {
	stub := &stubComputeClient{
		instances: []*gcp.Instance{{Name: "inst-1", Zone: "us-central1-a"}},
		migs: map[string][]gcp.ManagedInstanceGroup{
			"us-central1-a": {{Name: "mig-a", Location: "us-central1-a", IsRegional: false}},
			"us-central1":   {{Name: "mig-reg", Location: "us-central1", IsRegional: true}},
		},
	}
	origFactory := newComputeClientFunc
	newComputeClientFunc = func(context.Context, string) (computeClient, error) {
		return stub, nil
	}
	t.Cleanup(func() { newComputeClientFunc = origFactory })

	origResourceType := resourceType
	resourceType = ""
	t.Cleanup(func() { resourceType = origResourceType })

	cmd := &cobra.Command{}
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("zone", "", "")
	cmd.Flags().String("type", "", "")
	require.NoError(t, cmd.Flags().Set("project", "demo"))
	require.NoError(t, cmd.Flags().Set("zone", "us-central1-a"))

	results, directive := gcpSSHCompletion(cmd, nil, "")
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	require.ElementsMatch(t, []string{
		"inst-1\tCompute instance (us-central1-a)",
		"mig-a\tManaged instance group (zone us-central1-a)",
		"mig-reg\tManaged instance group (region us-central1)",
	}, results)
}

func TestGcpSSHCompletionUsesCacheWhenNoProjectOrZone(t *testing.T) {
	origLoader := loadCompletionCache
	loadCompletionCache = func() (completionCache, error) {
		return &stubCompletionCache{
			projects: []string{"proj-a"},
			locations: map[string][]cache.CachedLocation{
				"proj-a": {
					{Name: "inst-1", Type: cache.ResourceTypeInstance, Location: "us-central1-a"},
					{Name: "mig-1", Type: cache.ResourceTypeMIG, Location: "us-central1"},
				},
			},
		}, nil
	}
	t.Cleanup(func() { loadCompletionCache = origLoader })

	origProject := project
	origZone := zone
	origResourceType := resourceType
	project = ""
	zone = ""
	resourceType = ""
	t.Cleanup(func() {
		project = origProject
		zone = origZone
		resourceType = origResourceType
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("zone", "", "")
	cmd.Flags().String("type", "", "")

	results, directive := gcpSSHCompletion(cmd, nil, "")
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	require.ElementsMatch(t, []string{
		"inst-1\tCached instance (us-central1-a)",
		"mig-1\tCached mig (us-central1)",
	}, results)
}

func TestGcpSSHZoneCompletionUsesZonesAndRegions(t *testing.T) {
	stub := &stubComputeClient{
		zones:   []string{"us-central1-a", "europe-west1-b"},
		regions: []string{"us-central1", "europe-west1"},
	}
	origFactory := newComputeClientFunc
	newComputeClientFunc = func(context.Context, string) (computeClient, error) {
		return stub, nil
	}
	t.Cleanup(func() { newComputeClientFunc = origFactory })

	origResourceType := resourceType
	resourceType = ""
	t.Cleanup(func() { resourceType = origResourceType })

	cmd := &cobra.Command{}
	cmd.Flags().String("project", "", "")

	results, directive := gcpSSHZoneCompletion(cmd, nil, "us-central1")
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	require.Contains(t, results, "us-central1-a\tCompute zone")
	require.Contains(t, results, "us-central1\tCompute region")
}

func TestMigCompletionLocations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty zone",
			input:    "",
			expected: []string{""},
		},
		{
			name:     "zone value",
			input:    "us-central1-b",
			expected: []string{"us-central1-b", "us-central1"},
		},
		{
			name:     "region value",
			input:    "us-central1",
			expected: []string{"us-central1"},
		},
		{
			name:     "deduplicated values",
			input:    "europe-west2-b",
			expected: []string{"europe-west2-b", "europe-west2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := migCompletionLocations(tt.input)

			require.Len(t, result, len(tt.expected))
			for i, value := range tt.expected {
				require.Equal(t, value, result[i])
			}
		})
	}
}

func TestZoneLooksLikeZone(t *testing.T) {
	require.True(t, zoneLooksLikeZone("us-central1-a"))
	require.False(t, zoneLooksLikeZone("us-central1"))
}

func TestRegionFromZone(t *testing.T) {
	require.Equal(t, "us-central1", regionFromZone("us-central1-a"))
	require.Empty(t, regionFromZone("us-central1"))
}

func TestDeduplicateStrings(t *testing.T) {
	values := []string{"a", "b", "a", "c", "b"}
	result := deduplicateStrings(values)

	require.Len(t, result, 3)

	expected := []string{"a", "b", "c"}
	for i, value := range expected {
		require.Equal(t, value, result[i])
	}
}
