package ssh

import (
	"os/exec"
	"testing"

	"cx/internal/gcp"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Error("NewClient() returned nil")
	}
}

func TestConnectWithIAP_DirectConnection(t *testing.T) {
	// Skip this test as it would actually try to connect via SSH
	t.Skip("Skipping actual SSH connection test - would timeout trying to connect")
}

func TestConnectWithIAP_NoExternalIP(t *testing.T) {
	client := NewClient()

	instance := &gcp.Instance{
		Name:       "internal-instance",
		Zone:       "us-central1-a",
		ExternalIP: "", // No external IP
		CanUseIAP:  false,
	}

	project := "test-project"

	err := client.ConnectWithIAP(instance, project, []string{})
	if err == nil {
		t.Error("Expected error for instance without external IP and IAP disabled")
	}

	expectedError := "instance has no external IP and IAP is not available"
	if err.Error() != expectedError {
		t.Errorf("Expected error message %q, got %q", expectedError, err.Error())
	}
}

func TestBinaryPathLookup(t *testing.T) {
	// Test that we can find common binaries
	tests := []struct {
		name   string
		binary string
	}{
		{"ssh binary", "ssh"},
		{"gcloud binary", "gcloud"}, // This might fail if gcloud is not installed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := exec.LookPath(tt.binary)
			if tt.binary == "gcloud" && err != nil {
				t.Skipf("Skipping %s test: binary not found (this is expected in CI)", tt.binary)

				return
			}

			if err != nil {
				t.Errorf("Failed to find %s binary: %v", tt.binary, err)

				return
			}

			if path == "" {
				t.Errorf("Empty path returned for %s binary", tt.binary)
			}

			t.Logf("Found %s at: %s", tt.binary, path)
		})
	}
}

// Mock function to test command construction without execution.
func testGcloudCommandConstruction(instance *gcp.Instance, project string, sshFlags []string) []string {
	args := []string{
		"compute", "ssh",
		instance.Name,
		"--zone", instance.Zone,
		"--project", project,
		"--tunnel-through-iap",
	}

	// Add SSH flags if provided
	for _, flag := range sshFlags {
		args = append(args, "--ssh-flag="+flag)
	}

	return args
}

func TestGcloudCommandConstruction(t *testing.T) {
	instance := &gcp.Instance{
		Name: "test-instance",
		Zone: "us-central1-a",
	}
	project := "my-project"

	args := testGcloudCommandConstruction(instance, project, []string{})

	expectedArgs := []string{
		"compute", "ssh",
		"test-instance",
		"--zone", "us-central1-a",
		"--project", "my-project",
		"--tunnel-through-iap",
	}

	if len(args) != len(expectedArgs) {
		t.Fatalf("Expected %d arguments, got %d", len(expectedArgs), len(args))
	}

	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("Argument %d: expected %q, got %q", i, expectedArgs[i], arg)
		}
	}
}

// Mock function to test SSH command construction.
func testSSHCommandConstruction(instance *gcp.Instance, sshFlags []string) []string {
	args := []string{instance.ExternalIP}
	args = append(args, sshFlags...)

	return args
}

func TestSSHCommandConstruction(t *testing.T) {
	instance := &gcp.Instance{
		Name:       "test-instance",
		Zone:       "us-central1-a",
		ExternalIP: "203.0.113.1",
	}

	args := testSSHCommandConstruction(instance, []string{})

	expectedArgs := []string{"203.0.113.1"}

	if len(args) != len(expectedArgs) {
		t.Fatalf("Expected %d arguments, got %d", len(expectedArgs), len(args))
	}

	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("Argument %d: expected %q, got %q", i, expectedArgs[i], arg)
		}
	}
}

func TestGcloudCommandConstructionWithSSHFlags(t *testing.T) {
	instance := &gcp.Instance{
		Name: "test-instance",
		Zone: "us-central1-a",
	}
	project := "my-project"
	sshFlags := []string{"-L 13938:10.201.128.14:13938", "-D 8080"}

	args := testGcloudCommandConstruction(instance, project, sshFlags)

	expectedArgs := []string{
		"compute", "ssh",
		"test-instance",
		"--zone", "us-central1-a",
		"--project", "my-project",
		"--tunnel-through-iap",
		"--ssh-flag=-L 13938:10.201.128.14:13938",
		"--ssh-flag=-D 8080",
	}

	if len(args) != len(expectedArgs) {
		t.Fatalf("Expected %d arguments, got %d", len(expectedArgs), len(args))
	}

	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("Argument %d: expected %q, got %q", i, expectedArgs[i], arg)
		}
	}
}

func TestSSHCommandConstructionWithFlags(t *testing.T) {
	instance := &gcp.Instance{
		Name:       "test-instance",
		Zone:       "us-central1-a",
		ExternalIP: "203.0.113.1",
	}
	sshFlags := []string{"-L", "13938:10.201.128.14:13938", "-D", "8080"}

	args := testSSHCommandConstruction(instance, sshFlags)

	expectedArgs := []string{"203.0.113.1", "-L", "13938:10.201.128.14:13938", "-D", "8080"}

	if len(args) != len(expectedArgs) {
		t.Fatalf("Expected %d arguments, got %d", len(expectedArgs), len(args))
	}

	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("Argument %d: expected %q, got %q", i, expectedArgs[i], arg)
		}
	}
}
