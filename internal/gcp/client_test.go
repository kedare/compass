package gcp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
			if result != tt.expected {
				t.Errorf("extractInstanceName() = %v, want %v", result, tt.expected)
			}
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
			if result != tt.expected {
				t.Errorf("extractZoneName() = %v, want %v", result, tt.expected)
			}
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
			if result != tt.expected {
				t.Errorf("extractZoneFromInstanceURL() = %v, want %v", result, tt.expected)
			}
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
			if result != tt.expected {
				t.Errorf("extractMachineType() = %v, want %v", result, tt.expected)
			}
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
			if result != tt.expected {
				t.Errorf("isRegion(%q) = %v, want %v", tt.location, result, tt.expected)
			}
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
	if result.Name != "test-instance" {
		t.Errorf("convertInstance().Name = %v, want %v", result.Name, "test-instance")
	}

	if result.Zone != "us-central1-a" {
		t.Errorf("convertInstance().Zone = %v, want %v", result.Zone, "us-central1-a")
	}

	if result.Status != "RUNNING" {
		t.Errorf("convertInstance().Status = %v, want %v", result.Status, "RUNNING")
	}

	if result.MachineType != "n1-standard-1" {
		t.Errorf("convertInstance().MachineType = %v, want %v", result.MachineType, "n1-standard-1")
	}

	if result.InternalIP != "10.0.0.1" {
		t.Errorf("convertInstance().InternalIP = %v, want %v", result.InternalIP, "10.0.0.1")
	}

	if result.ExternalIP != "203.0.113.1" {
		t.Errorf("convertInstance().ExternalIP = %v, want %v", result.ExternalIP, "203.0.113.1")
	}

	if result.CanUseIAP {
		t.Errorf("convertInstance().CanUseIAP = %v, want %v", result.CanUseIAP, false)
	}
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

	if result.InternalIP != "10.0.0.2" {
		t.Errorf("convertInstance().InternalIP = %v, want %v", result.InternalIP, "10.0.0.2")
	}

	if result.ExternalIP != "" {
		t.Errorf("convertInstance().ExternalIP = %v, want %v", result.ExternalIP, "")
	}

	if !result.CanUseIAP {
		t.Errorf("convertInstance().CanUseIAP = %v, want %v", result.CanUseIAP, true)
	}
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
		if !ok {
			t.Fatalf("unexpected page token: %s", pageToken)
		}

		return page, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inst == nil || inst.Name != "target-instance" {
		t.Fatalf("expected to find target-instance, got %#v", inst)
	}
}

func TestFindInstanceInAggregatedPagesNotFound(t *testing.T) {
	_, err := findInstanceInAggregatedPages("missing", func(string) (*compute.InstanceAggregatedList, error) {
		return &compute.InstanceAggregatedList{}, nil
	})

	if !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("expected ErrInstanceNotFound, got %v", err)
	}
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
		if !ok {
			t.Fatalf("unexpected page token: %s", token)
		}

		return page, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scope != "regions/us-central1" {
		t.Fatalf("expected scope regions/us-central1, got %s", scope)
	}
}

func TestFindMIGScopeAcrossPagesNotFound(t *testing.T) {
	_, err := findMIGScopeAcrossPages("missing", func(string) (*compute.InstanceGroupManagerAggregatedList, error) {
		return &compute.InstanceGroupManagerAggregatedList{}, nil
	})

	if !errors.Is(err, ErrMIGNotInAggregatedList) {
		t.Fatalf("expected ErrMIGNotInAggregatedList, got %v", err)
	}
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
		if !ok {
			t.Fatalf("unexpected token %s", token)
		}

		return page.items, page.next, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}

	if instances[1].Instance != "instances/two" {
		t.Fatalf("unexpected instance order: %#v", instances)
	}
}

func TestCollectManagedInstancesError(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := collectManagedInstances(func(string) ([]*compute.ManagedInstance, string, error) {
		return nil, "", sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestGetDefaultProjectFromEnv(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")

	if project := getDefaultProject(); project != "env-project" {
		t.Fatalf("expected env-project, got %s", project)
	}
}

func TestGetDefaultProjectFromConfig(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")

	configDir := t.TempDir()

	configPath := filepath.Join(configDir, "configurations")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatalf("failed to create config path: %v", err)
	}

	content := "[core]\nproject = config-project\n"
	if err := os.WriteFile(filepath.Join(configPath, "config_default"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	t.Setenv("CLOUDSDK_CONFIG", configDir)

	if project := getDefaultProject(); project != "config-project" {
		t.Fatalf("expected config-project, got %s", project)
	}
}

func TestGetDefaultProjectNoSource(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())

	if project := getDefaultProject(); project != "" {
		t.Fatalf("expected empty project, got %s", project)
	}
}
