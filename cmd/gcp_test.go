package cmd

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestGcpRootCommand(t *testing.T) {
	if gcpCmd == nil {
		t.Fatal("gcpCmd is nil")
	}

	if gcpCmd.Use != "gcp" {
		t.Errorf("Expected Use to be 'gcp', got %q", gcpCmd.Use)
	}

	if gcpCmd.Short == "" {
		t.Error("Short description is empty")
	}

	if gcpCmd.Long == "" {
		t.Error("Long description is empty")
	}

	if gcpCmd.Run != nil || gcpCmd.RunE != nil {
		t.Error("gcpCmd should not execute without a subcommand")
	}
}

func TestGcpRootProjectFlag(t *testing.T) {
	projectFlag := gcpCmd.PersistentFlags().Lookup("project")
	if projectFlag == nil {
		t.Fatal("project flag not found on gcp command")
	}

	if projectFlag.Shorthand != "p" {
		t.Errorf("Expected project flag shorthand 'p', got %q", projectFlag.Shorthand)
	}

	if projectFlag.Usage == "" {
		t.Error("Project flag usage is empty")
	}
}

func TestGcpSshCommand(t *testing.T) {
	if gcpSshCmd == nil {
		t.Fatal("gcpSshCmd is nil")
	}

	if gcpSshCmd.Use != "ssh [instance-name]" {
		t.Errorf("Expected Use to be 'ssh [instance-name]', got %q", gcpSshCmd.Use)
	}

	if gcpSshCmd.Short == "" {
		t.Error("Short description is empty")
	}

	if gcpSshCmd.Long == "" {
		t.Error("Long description is empty")
	}

	if err := gcpSshCmd.Args(gcpSshCmd, []string{}); err == nil {
		t.Error("Expected error for no arguments")
	}

	if err := gcpSshCmd.Args(gcpSshCmd, []string{"instance1", "instance2"}); err == nil {
		t.Error("Expected error for too many arguments")
	}

	if err := gcpSshCmd.Args(gcpSshCmd, []string{"instance1"}); err != nil {
		t.Errorf("Unexpected error for single argument: %v", err)
	}
}

func TestGcpSshCommandFlags(t *testing.T) {
	zoneFlag := gcpSshCmd.Flags().Lookup("zone")
	if zoneFlag == nil {
		t.Fatal("zone flag not found on ssh command")
	}

	if zoneFlag.Shorthand != "z" {
		t.Errorf("Expected zone flag shorthand 'z', got %q", zoneFlag.Shorthand)
	}

	if zoneFlag.Usage == "" {
		t.Error("Zone flag usage is empty")
	}

	typeFlag := gcpSshCmd.Flags().Lookup("type")
	if typeFlag == nil {
		t.Fatal("type flag not found on ssh command")
	}

	if typeFlag.Shorthand != "t" {
		t.Errorf("Expected type flag shorthand 't', got %q", typeFlag.Shorthand)
	}

	if typeFlag.Usage == "" {
		t.Error("Type flag usage is empty")
	}

	sshFlag := gcpSshCmd.Flags().Lookup("ssh-flag")
	if sshFlag == nil {
		t.Fatal("ssh-flag not found on ssh command")
	}

	if sshFlag.Usage == "" {
		t.Error("ssh-flag usage is empty")
	}
}

func TestGcpCommandStructure(t *testing.T) {
	foundGcp := false

	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "gcp" {
			foundGcp = true

			break
		}
	}

	if !foundGcp {
		t.Fatal("gcp command not found in root command")
	}

	foundSsh := false

	for _, cmd := range gcpCmd.Commands() {
		if cmd.Name() == "ssh" {
			foundSsh = true

			break
		}
	}

	if !foundSsh {
		t.Fatal("ssh command not found under gcp command")
	}
}

func TestGcpCommandHelp(t *testing.T) {
	rootCmd.SetArgs([]string{"gcp", "--help"})
	defer rootCmd.SetArgs([]string{})

	if err := rootCmd.Execute(); err != nil {
		t.Errorf("Help command returned error: %v", err)
	}
}

func TestGcpSshHelp(t *testing.T) {
	rootCmd.SetArgs([]string{"gcp", "ssh", "--help"})
	defer rootCmd.SetArgs([]string{})

	if err := rootCmd.Execute(); err != nil {
		t.Errorf("Help command returned error: %v", err)
	}
}

func TestResourceTypeConstants(t *testing.T) {
	if resourceTypeInstance != "instance" {
		t.Errorf("Expected resourceTypeInstance to be 'instance', got %q", resourceTypeInstance)
	}

	if resourceTypeMIG != "mig" {
		t.Errorf("Expected resourceTypeMIG to be 'mig', got %q", resourceTypeMIG)
	}
}

func TestDefaultMIGSelectionIndex(t *testing.T) {
	refs := []gcp.ManagedInstanceRef{
		{Name: "inst-1", Zone: "us-central1-a", Status: "STOPPED"},
		{Name: "inst-2", Zone: "us-central1-b", Status: gcp.InstanceStatusRunning},
		{Name: "inst-3", Zone: "us-central1-c", Status: "PROVISIONING"},
	}

	if idx := defaultMIGSelectionIndex(refs); idx != 1 {
		t.Fatalf("Expected default index 1, got %d", idx)
	}
}

func TestPromptMIGInstanceSelectionFromReader_ExplicitChoice(t *testing.T) {
	refs := []gcp.ManagedInstanceRef{
		{Name: "inst-1", Zone: "us-central1-a", Status: "STOPPED"},
		{Name: "inst-2", Zone: "us-central1-b", Status: gcp.InstanceStatusRunning},
		{Name: "inst-3", Zone: "us-central1-c", Status: "RUNNING"},
	}

	reader := bufio.NewReader(strings.NewReader("3\n"))
	var out bytes.Buffer

	selected, err := promptMIGInstanceSelectionFromReader(reader, &out, "my-mig", refs)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if selected.Name != "inst-3" {
		t.Fatalf("Expected inst-3, got %s", selected.Name)
	}

	output := out.String()
	if !strings.Contains(output, "[2]* inst-2") {
		t.Fatalf("Expected default marker for inst-2 in output, got %q", output)
	}
}

func TestPromptMIGInstanceSelectionFromReader_DefaultChoice(t *testing.T) {
	refs := []gcp.ManagedInstanceRef{
		{Name: "inst-1", Zone: "us-central1-a", Status: gcp.InstanceStatusRunning},
		{Name: "inst-2", Zone: "us-central1-b", Status: "STOPPED"},
	}

	reader := bufio.NewReader(strings.NewReader("\n"))
	var out bytes.Buffer

	selected, err := promptMIGInstanceSelectionFromReader(reader, &out, "my-mig", refs)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if selected.Name != "inst-1" {
		t.Fatalf("Expected default inst-1, got %s", selected.Name)
	}
}

func TestPromptMIGInstanceSelectionFromReader_InvalidRetry(t *testing.T) {
	refs := []gcp.ManagedInstanceRef{
		{Name: "inst-1", Zone: "us-central1-a", Status: gcp.InstanceStatusRunning},
		{Name: "inst-2", Zone: "us-central1-b", Status: "STOPPED"},
	}

	reader := bufio.NewReader(strings.NewReader("abc\n2\n"))
	var out bytes.Buffer

	selected, err := promptMIGInstanceSelectionFromReader(reader, &out, "my-mig", refs)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if selected.Name != "inst-2" {
		t.Fatalf("Expected inst-2 after retry, got %s", selected.Name)
	}

	if !strings.Contains(out.String(), "Invalid selection") {
		t.Fatalf("Expected invalid selection prompt in output, got %q", out.String())
	}
}
