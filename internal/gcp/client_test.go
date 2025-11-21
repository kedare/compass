package gcp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kedare/compass/internal/cache"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/compute/v1"
)

func TestExtractInstanceName(t *testing.T) {
	tests := []struct {
		name        string
		instanceURL string
		expected    string
	}{
		{
			name:        "valid instance URL",
			instanceURL: "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/instances/test-instance",
			expected:    "test-instance",
		},
		{
			name:        "short instance URL",
			instanceURL: "zones/us-central1-a/instances/my-instance",
			expected:    "my-instance",
		},
		{
			name:        "simple instance name",
			instanceURL: "instance-name",
			expected:    "instance-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractInstanceName(tt.instanceURL)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractZoneName(t *testing.T) {
	tests := []struct {
		name     string
		zoneURL  string
		expected string
	}{
		{
			name:     "valid zone URL",
			zoneURL:  "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a",
			expected: "us-central1-a",
		},
		{
			name:     "short zone URL",
			zoneURL:  "zones/europe-west2-b",
			expected: "europe-west2-b",
		},
		{
			name:     "simple zone name",
			zoneURL:  "us-west1-c",
			expected: "us-west1-c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractZoneName(tt.zoneURL)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractZoneFromInstanceURL(t *testing.T) {
	tests := []struct {
		name        string
		instanceURL string
		expected    string
	}{
		{
			name:        "valid instance URL with zone",
			instanceURL: "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/instances/test-instance",
			expected:    "us-central1-a",
		},
		{
			name:        "short instance URL with zone",
			instanceURL: "projects/my-project/zones/europe-west2-b/instances/my-instance",
			expected:    "europe-west2-b",
		},
		{
			name:        "no zone in URL",
			instanceURL: "instances/my-instance",
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractZoneFromInstanceURL(tt.instanceURL)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMachineType(t *testing.T) {
	tests := []struct {
		name           string
		machineTypeURL string
		expected       string
	}{
		{
			name:           "valid machine type URL",
			machineTypeURL: "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/machineTypes/n1-standard-1",
			expected:       "n1-standard-1",
		},
		{
			name:           "short machine type URL",
			machineTypeURL: "zones/us-central1-a/machineTypes/e2-micro",
			expected:       "e2-micro",
		},
		{
			name:           "simple machine type",
			machineTypeURL: "n2-standard-4",
			expected:       "n2-standard-4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMachineType(tt.machineTypeURL)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRegion(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name     string
		location string
		expected bool
	}{
		{
			name:     "valid region format",
			location: "us-central1",
			expected: true,
		},
		{
			name:     "valid region format with dash",
			location: "europe-west2",
			expected: true,
		},
		{
			name:     "valid zone format",
			location: "us-central1-a",
			expected: false,
		},
		{
			name:     "valid zone format with letter",
			location: "europe-west2-b",
			expected: false,
		},
		{
			name:     "long region format",
			location: "asia-southeast1",
			expected: true,
		},
		{
			name:     "single word",
			location: "invalid",
			expected: false,
		},
		{
			name:     "empty string",
			location: "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.isRegion(tt.location)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertInstance(t *testing.T) {
	client := &Client{}

	// Create a mock compute instance
	computeInstance := &compute.Instance{
		Name:        "test-instance",
		Zone:        "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a",
		Status:      "RUNNING",
		MachineType: "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/machineTypes/n1-standard-1",
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				NetworkIP: "10.0.0.1",
				AccessConfigs: []*compute.AccessConfig{
					{
						NatIP: "203.0.113.1",
					},
				},
			},
		},
	}

	result := client.convertInstance(computeInstance)

	// Verify all fields are correctly converted
	require.Equal(t, "test-instance", result.Name)
	require.Equal(t, "us-central1-a", result.Zone)
	require.Equal(t, "RUNNING", result.Status)
	require.Equal(t, "n1-standard-1", result.MachineType)
	require.Equal(t, "10.0.0.1", result.InternalIP)
	require.Equal(t, "203.0.113.1", result.ExternalIP)
	require.False(t, result.CanUseIAP)
}

func TestConvertInstanceNoExternalIP(t *testing.T) {
	client := &Client{}

	// Create a mock compute instance without external IP
	computeInstance := &compute.Instance{
		Name:        "internal-instance",
		Zone:        "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a",
		Status:      "RUNNING",
		MachineType: "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/machineTypes/n1-standard-1",
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				NetworkIP: "10.0.0.2",
			},
		},
	}

	result := client.convertInstance(computeInstance)

	require.Equal(t, "10.0.0.2", result.InternalIP)
	require.Empty(t, result.ExternalIP)
	require.True(t, result.CanUseIAP)
}

func TestFindInstanceInAggregatedPages(t *testing.T) {
	pages := map[string]*compute.InstanceAggregatedList{
		"": {
			Items: map[string]compute.InstancesScopedList{
				"zones/us-central1-a": {
					Instances: []*compute.Instance{{Name: "other-instance"}},
				},
			},
			NextPageToken: "token-1",
		},
		"token-1": {
			Items: map[string]compute.InstancesScopedList{
				"zones/us-central1-b": {
					Instances: []*compute.Instance{{Name: "target-instance", Zone: "projects/p/zones/us-central1-b"}},
				},
			},
		},
	}

	inst, err := findInstanceInAggregatedPages("target-instance", func(pageToken string) (*compute.InstanceAggregatedList, error) {
		page, ok := pages[pageToken]
		require.True(t, ok, "unexpected page token: %s", pageToken)

		return page, nil
	})
	require.NoError(t, err)
	require.NotNil(t, inst)
	require.Equal(t, "target-instance", inst.Name)
}

func TestFindInstanceInAggregatedPagesNotFound(t *testing.T) {
	_, err := findInstanceInAggregatedPages("missing", func(string) (*compute.InstanceAggregatedList, error) {
		return &compute.InstanceAggregatedList{}, nil
	})

	require.ErrorIs(t, err, ErrInstanceNotFound)
}

func TestFindMIGScopeAcrossPages(t *testing.T) {
	pages := map[string]*compute.InstanceGroupManagerAggregatedList{
		"": {
			Items: map[string]compute.InstanceGroupManagersScopedList{
				"zones/us-central1-a": {
					InstanceGroupManagers: []*compute.InstanceGroupManager{{Name: "other"}},
				},
			},
			NextPageToken: "next",
		},
		"next": {
			Items: map[string]compute.InstanceGroupManagersScopedList{
				"regions/us-central1": {
					InstanceGroupManagers: []*compute.InstanceGroupManager{{Name: "target-mig"}},
				},
			},
		},
	}

	scope, err := findMIGScopeAcrossPages("target-mig", func(token string) (*compute.InstanceGroupManagerAggregatedList, error) {
		page, ok := pages[token]
		require.True(t, ok, "unexpected page token: %s", token)

		return page, nil
	})
	require.NoError(t, err)
	require.Equal(t, "regions/us-central1", scope)
}

func TestFindMIGScopeAcrossPagesNotFound(t *testing.T) {
	_, err := findMIGScopeAcrossPages("missing", func(string) (*compute.InstanceGroupManagerAggregatedList, error) {
		return &compute.InstanceGroupManagerAggregatedList{}, nil
	})

	require.ErrorIs(t, err, ErrMIGNotInAggregatedList)
}

func TestCollectInstances(t *testing.T) {
	pages := map[string]struct {
		next  string
		items []*compute.Instance
	}{
		"": {
			items: []*compute.Instance{{Name: "inst-1"}},
			next:  "next",
		},
		"next": {
			items: []*compute.Instance{{Name: "inst-2"}},
		},
	}

	instances, err := collectInstances(func(token string) ([]*compute.Instance, string, error) {
		page, ok := pages[token]
		require.True(t, ok, "unexpected token %s", token)

		return page.items, page.next, nil
	})
	require.NoError(t, err)
	require.Len(t, instances, 2)
	require.Equal(t, "inst-2", instances[1].Name)
}

func TestCollectInstancesError(t *testing.T) {
	sentinel := errors.New("fail")

	_, err := collectInstances(func(string) ([]*compute.Instance, string, error) {
		return nil, "", sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

func TestCollectInstanceGroupManagers(t *testing.T) {
	pages := map[string]struct {
		next  string
		items []*compute.InstanceGroupManager
	}{
		"": {
			items: []*compute.InstanceGroupManager{{Name: "group-1"}},
			next:  "token",
		},
		"token": {
			items: []*compute.InstanceGroupManager{{Name: "group-2"}},
		},
	}

	groups, err := collectInstanceGroupManagers(func(token string) ([]*compute.InstanceGroupManager, string, error) {
		page, ok := pages[token]
		require.True(t, ok, "unexpected token %s", token)

		return page.items, page.next, nil
	})
	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.Equal(t, "group-1", groups[0].Name)
	require.Equal(t, "group-2", groups[1].Name)
}

func TestCollectInstanceGroupManagersError(t *testing.T) {
	sentinel := errors.New("boom")

	_, err := collectInstanceGroupManagers(func(string) ([]*compute.InstanceGroupManager, string, error) {
		return nil, "", sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

func TestCollectManagedInstances(t *testing.T) {
	pages := map[string]struct {
		next  string
		items []*compute.ManagedInstance
	}{
		"": {
			items: []*compute.ManagedInstance{{Instance: "instances/one"}},
			next:  "token",
		},
		"token": {
			items: []*compute.ManagedInstance{{Instance: "instances/two"}},
		},
	}

	instances, err := collectManagedInstances(func(token string) ([]*compute.ManagedInstance, string, error) {
		page, ok := pages[token]
		require.True(t, ok, "unexpected token %s", token)

		return page.items, page.next, nil
	})
	require.NoError(t, err)
	require.Len(t, instances, 2)
	require.Equal(t, "instances/two", instances[1].Instance)
}

func TestCollectManagedInstancesError(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := collectManagedInstances(func(string) ([]*compute.ManagedInstance, string, error) {
		return nil, "", sentinel
	})

	require.ErrorIs(t, err, sentinel)
}

func TestCollectZones(t *testing.T) {
	pages := map[string]struct {
		next  string
		items []*compute.Zone
	}{
		"": {
			items: []*compute.Zone{{Name: "us-central1-a"}},
			next:  "token",
		},
		"token": {
			items: []*compute.Zone{{Name: "us-central1-b"}},
		},
	}

	zones, err := collectZones(func(token string) ([]*compute.Zone, string, error) {
		page, ok := pages[token]
		require.True(t, ok, "unexpected token %s", token)

		return page.items, page.next, nil
	})
	require.NoError(t, err)
	require.Len(t, zones, 2)
	require.Equal(t, "us-central1-b", zones[1].Name)
}

func TestCollectZonesError(t *testing.T) {
	sentinel := errors.New("fail")

	_, err := collectZones(func(string) ([]*compute.Zone, string, error) {
		return nil, "", sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

func TestGetDefaultProjectFromEnv(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")

	project := getDefaultProject()
	require.Equal(t, "env-project", project)
}

func TestGetDefaultProjectFromConfig(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")

	configDir := t.TempDir()

	configPath := filepath.Join(configDir, "configurations")
	err := os.MkdirAll(configPath, 0o755)
	require.NoError(t, err)

	content := "[core]\nproject = config-project\n"
	err = os.WriteFile(filepath.Join(configPath, "config_default"), []byte(content), 0o644)
	require.NoError(t, err)

	t.Setenv("CLOUDSDK_CONFIG", configDir)

	project := getDefaultProject()
	require.Equal(t, "config-project", project)
}

func TestGetDefaultProjectNoSource(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())

	project := getDefaultProject()
	require.Empty(t, project)
}

func TestFormatProgressKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "trims whitespace",
			input:    "  region:us-central1  ",
			expected: "region:us-central1",
		},
		{
			name:     "extracts first token",
			input:    "aggregate results pending",
			expected: "aggregate",
		},
		{
			name:     "empty string",
			input:    "   ",
			expected: "",
		},
		{
			name:     "single word",
			input:    "scope:zone",
			expected: "scope:zone",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.expected, FormatProgressKey(tt.input))
		})
	}
}

func TestProgressHelpers(t *testing.T) {
	t.Parallel()

	t.Run("captures update success and failure", func(t *testing.T) {
		var events []ProgressEvent
		cb := func(evt ProgressEvent) {
			events = append(events, evt)
		}

		progressUpdate(cb, "task", "working")
		progressSuccess(cb, "task", "done")

		sentinel := errors.New("boom")
		progressFailure(cb, "task", "failed", sentinel)

		require.Len(t, events, 3)

		checkEvent := func(evt ProgressEvent) {
			if evt.Info {
				require.NoError(t, evt.Err, "informational events must not include errors")
			}
		}

		require.Equal(t, ProgressEvent{Key: "task", Message: "working"}, events[0])
		require.False(t, events[0].Info)
		checkEvent(events[0])

		require.Equal(t, ProgressEvent{
			Key:     "task",
			Message: "done",
			Done:    true,
		}, events[1])
		require.False(t, events[1].Info)
		checkEvent(events[1])

		require.Equal(t, ProgressEvent{
			Key:     "task",
			Message: "failed",
			Err:     sentinel,
			Done:    true,
		}, events[2])
		require.False(t, events[2].Info)
		require.ErrorIs(t, events[2].Err, sentinel)
		checkEvent(events[2])
	})

	t.Run("nil callback does nothing", func(t *testing.T) {
		require.NotPanics(t, func() {
			progressUpdate(nil, "key", "message")
			progressSuccess(nil, "key", "message")
			progressFailure(nil, "key", "message", errors.New("ignored"))
		})
	})

	t.Run("informational events must not carry errors", func(t *testing.T) {
		event := ProgressEvent{Key: "info", Message: "done", Done: true, Info: true}
		require.True(t, event.Info)
		require.NoError(t, event.Err)
	})
}

func TestCacheInstancePreservesIAPPreference(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	c, err := cache.New()
	require.NoError(t, err)

	preferIAP := true
	require.NoError(t, c.Set("iap-instance", &cache.LocationInfo{
		Project: "demo",
		Zone:    "us-central1-a",
		Type:    cache.ResourceTypeInstance,
		IAP:     &preferIAP,
	}))

	client := &Client{
		cache:   c,
		project: "demo",
	}

	client.cacheInstance("iap-instance", &Instance{
		Name: "iap-instance",
		Zone: "us-central1-b",
	})

	stored, ok := c.Get("iap-instance")
	require.True(t, ok)
	require.NotNil(t, stored)
	require.Equal(t, "us-central1-b", stored.Zone)
	require.NotNil(t, stored.IAP)
	require.True(t, *stored.IAP)
}
