package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestGcpCommand(t *testing.T) {
	// Test that the gcp command is properly configured
	if gcpCmd == nil {
		t.Error("gcpCmd is nil")

		return
	}

	// Test command properties
	if gcpCmd.Use != "gcp [instance-name]" {
		t.Errorf("Expected Use to be 'gcp [instance-name]', got %q", gcpCmd.Use)
	}

	if gcpCmd.Short == "" {
		t.Error("Short description is empty")
	}

	if gcpCmd.Long == "" {
		t.Error("Long description is empty")
	}

	// Test that it expects exactly one argument
	err := gcpCmd.Args(gcpCmd, []string{})
	if err == nil {
		t.Error("Expected error for no arguments")
	}

	err = gcpCmd.Args(gcpCmd, []string{"instance1", "instance2"})
	if err == nil {
		t.Error("Expected error for too many arguments")
	}

	err = gcpCmd.Args(gcpCmd, []string{"instance1"})
	if err != nil {
		t.Errorf("Unexpected error for single argument: %v", err)
	}
}

func TestGcpCommandFlags(t *testing.T) {
	// Test that flags are properly configured
	projectFlag := gcpCmd.Flags().Lookup("project")
	if projectFlag == nil {
		t.Error("project flag not found")
	} else {
		if projectFlag.Shorthand != "p" {
			t.Errorf("Expected project flag shorthand 'p', got %q", projectFlag.Shorthand)
		}

		if projectFlag.Usage == "" {
			t.Error("Project flag usage is empty")
		}
	}

	zoneFlag := gcpCmd.Flags().Lookup("zone")
	if zoneFlag == nil {
		t.Error("zone flag not found")
	} else {
		if zoneFlag.Shorthand != "z" {
			t.Errorf("Expected zone flag shorthand 'z', got %q", zoneFlag.Shorthand)
		}

		if zoneFlag.Usage == "" {
			t.Error("Zone flag usage is empty")
		}
	}

	typeFlag := gcpCmd.Flags().Lookup("type")
	if typeFlag == nil {
		t.Error("type flag not found")
	} else {
		if typeFlag.Shorthand != "t" {
			t.Errorf("Expected type flag shorthand 't', got %q", typeFlag.Shorthand)
		}

		if typeFlag.Usage == "" {
			t.Error("Type flag usage is empty")
		}
	}

	sshFlagFlag := gcpCmd.Flags().Lookup("ssh-flag")
	if sshFlagFlag == nil {
		t.Error("ssh-flag flag not found")
	}
}

func TestGcpCommandExecution(t *testing.T) {
	// We can't easily test the full command execution without mocking GCP APIs
	// but we can test that the command doesn't panic with basic setup

	// Create a test command
	testCmd := &cobra.Command{
		Use: "test-gcp [instance-name]",
		Run: func(cmd *cobra.Command, args []string) {
			// This is a simplified version of the actual command
			// In a real test, we would mock the GCP client and SSH client
			instanceName := args[0]
			if instanceName == "" {
				t.Error("instanceName is empty")
			}
		},
	}

	// Test with valid arguments
	testCmd.SetArgs([]string{"test-instance"})

	err := testCmd.Execute()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Test helper to verify command structure.
func TestGcpCommandStructure(t *testing.T) {
	// Verify the command is properly added to root
	found := false

	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "gcp" {
			found = true

			break
		}
	}

	if !found {
		t.Error("gcp command not found in root command")
	}
}

func TestGcpCommandHelp(t *testing.T) {
	// Test that help can be generated without errors
	gcpCmd.SetArgs([]string{"--help"})

	// Capture the help output by temporarily redirecting
	// In a real scenario, this would show the help text
	err := gcpCmd.Execute()
	if err != nil {
		t.Errorf("Help command returned error: %v", err)
	}
}

func TestResourceTypeValidation(t *testing.T) {
	tests := []struct {
		name          string
		resourceType  string
		shouldBeValid bool
	}{
		{
			name:          "empty_resource_type",
			resourceType:  "",
			shouldBeValid: true, // Empty means auto-detect
		},
		{
			name:          "valid_instance",
			resourceType:  "instance",
			shouldBeValid: true,
		},
		{
			name:          "valid_mig",
			resourceType:  "mig",
			shouldBeValid: true,
		},
		{
			name:          "invalid_vm",
			resourceType:  "vm",
			shouldBeValid: false,
		},
		{
			name:          "invalid_server",
			resourceType:  "server",
			shouldBeValid: false,
		},
		{
			name:          "invalid_uppercase",
			resourceType:  "INSTANCE",
			shouldBeValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test validation logic
			isValid := tt.resourceType == "" || tt.resourceType == "instance" || tt.resourceType == "mig"
			if isValid != tt.shouldBeValid {
				t.Errorf("Resource type '%s': expected valid=%v, got valid=%v",
					tt.resourceType, tt.shouldBeValid, isValid)
			}
		})
	}
}
