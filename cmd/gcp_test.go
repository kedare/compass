package cmd

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestGcpRootCommand(t *testing.T) {
	require.NotNil(t, gcpCmd)
	require.Equal(t, "gcp", gcpCmd.Use)
	require.NotEmpty(t, gcpCmd.Short)
	require.NotEmpty(t, gcpCmd.Long)
	require.Nil(t, gcpCmd.Run)
	require.Nil(t, gcpCmd.RunE)
}

func TestGcpRootProjectFlag(t *testing.T) {
	projectFlag := gcpCmd.PersistentFlags().Lookup("project")
	require.NotNil(t, projectFlag)
	require.Equal(t, "p", projectFlag.Shorthand)
	require.NotEmpty(t, projectFlag.Usage)

	t.Run("project flag completion runs", func(t *testing.T) {
		completions, directive := gcpProjectCompletion(gcpCmd, nil, "")
		require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

		if len(completions) == 0 {
			t.Log("project completion returned no suggestions (may be empty cache)")
		}
	})
}

func TestGcpSshCommand(t *testing.T) {
	require.NotNil(t, gcpSshCmd)
	require.Equal(t, "ssh [instance-name]", gcpSshCmd.Use)
	require.NotEmpty(t, gcpSshCmd.Short)
	require.NotEmpty(t, gcpSshCmd.Long)
	require.Error(t, gcpSshCmd.Args(gcpSshCmd, []string{}))
	require.Error(t, gcpSshCmd.Args(gcpSshCmd, []string{"instance1", "instance2"}))
	require.NoError(t, gcpSshCmd.Args(gcpSshCmd, []string{"instance1"}))
	require.NotNil(t, gcpSshCmd.ValidArgsFunction)
}

func TestGcpSshCommandFlags(t *testing.T) {
	zoneFlag := gcpSshCmd.Flags().Lookup("zone")
	require.NotNil(t, zoneFlag)
	require.Equal(t, "z", zoneFlag.Shorthand)
	require.NotEmpty(t, zoneFlag.Usage)

	_, directive := gcpSSHZoneCompletion(gcpSshCmd, nil, "")
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	typeFlag := gcpSshCmd.Flags().Lookup("type")
	require.NotNil(t, typeFlag)
	require.Equal(t, "t", typeFlag.Shorthand)
	require.NotEmpty(t, typeFlag.Usage)

	sshFlag := gcpSshCmd.Flags().Lookup("ssh-flag")
	require.NotNil(t, sshFlag)
	require.NotEmpty(t, sshFlag.Usage)
}

func TestGcpCommandStructure(t *testing.T) {
	foundGcp := false

	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "gcp" {
			foundGcp = true

			break
		}
	}

	require.True(t, foundGcp, "gcp command not found in root command")

	foundSsh := false
	foundIP := false

	for _, cmd := range gcpCmd.Commands() {
		if cmd.Name() == "ssh" {
			foundSsh = true

			break
		}
	}

	require.True(t, foundSsh, "ssh command not found under gcp command")

	for _, cmd := range gcpCmd.Commands() {
		if cmd.Name() == "ip" {
			foundIP = true

			break
		}
	}

	require.True(t, foundIP, "ip command not found under gcp command")
}

func TestGcpCommandHelp(t *testing.T) {
	rootCmd.SetArgs([]string{"gcp", "--help"})
	defer rootCmd.SetArgs([]string{})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestGcpSshHelp(t *testing.T) {
	rootCmd.SetArgs([]string{"gcp", "ssh", "--help"})
	defer rootCmd.SetArgs([]string{})

	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestResourceTypeConstants(t *testing.T) {
	require.Equal(t, "instance", resourceTypeInstance)
	require.Equal(t, "mig", resourceTypeMIG)
}

func TestIPLookupCommand(t *testing.T) {
	require.Equal(t, "lookup <ip-address>", ipLookupCmd.Use)
	require.NotEmpty(t, ipLookupCmd.Short)
	require.Error(t, ipLookupCmd.Args(ipLookupCmd, []string{}))
	require.Error(t, ipLookupCmd.Args(ipLookupCmd, []string{"1.2.3.4", "extra"}))
	require.NoError(t, ipLookupCmd.Args(ipLookupCmd, []string{"1.2.3.4"}))
}

func TestEnumerateProjects(t *testing.T) {
	projects := []string{"alpha", "beta", "alpha", " ", "", "gamma"}
	result := enumerateProjects(projects)

	require.Len(t, result, 3)

	expected := map[string]struct{}{
		"alpha": {},
		"beta":  {},
		"gamma": {},
	}

	for _, project := range result {
		_, ok := expected[project]
		require.True(t, ok, "unexpected project %q in result", project)
	}
}

func TestDefaultMIGSelectionIndex(t *testing.T) {
	refs := []gcp.ManagedInstanceRef{
		{Name: "inst-1", Zone: "us-central1-a", Status: "STOPPED"},
		{Name: "inst-2", Zone: "us-central1-b", Status: gcp.InstanceStatusRunning},
		{Name: "inst-3", Zone: "us-central1-c", Status: "PROVISIONING"},
	}

	idx := defaultMIGSelectionIndex(refs)
	require.Equal(t, 1, idx)
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
	require.NoError(t, err)
	require.Equal(t, "inst-3", selected.Name)

	output := out.String()
	require.Contains(t, output, "[2]* inst-2")
}

func TestPromptMIGInstanceSelectionFromReader_DefaultChoice(t *testing.T) {
	refs := []gcp.ManagedInstanceRef{
		{Name: "inst-1", Zone: "us-central1-a", Status: gcp.InstanceStatusRunning},
		{Name: "inst-2", Zone: "us-central1-b", Status: "STOPPED"},
	}

	reader := bufio.NewReader(strings.NewReader("\n"))
	var out bytes.Buffer

	selected, err := promptMIGInstanceSelectionFromReader(reader, &out, "my-mig", refs)
	require.NoError(t, err)
	require.Equal(t, "inst-1", selected.Name)
}

func TestPromptMIGInstanceSelectionFromReader_InvalidRetry(t *testing.T) {
	refs := []gcp.ManagedInstanceRef{
		{Name: "inst-1", Zone: "us-central1-a", Status: gcp.InstanceStatusRunning},
		{Name: "inst-2", Zone: "us-central1-b", Status: "STOPPED"},
	}

	reader := bufio.NewReader(strings.NewReader("abc\n2\n"))
	var out bytes.Buffer

	selected, err := promptMIGInstanceSelectionFromReader(reader, &out, "my-mig", refs)
	require.NoError(t, err)
	require.Equal(t, "inst-2", selected.Name)
	require.Contains(t, out.String(), "Invalid selection")
}
