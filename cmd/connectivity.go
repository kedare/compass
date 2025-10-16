package cmd

import (
	"github.com/spf13/cobra"
)

var connectivityTestCmd = &cobra.Command{
	Use:     "connectivity-test",
	Aliases: []string{"ct"},
	Short:   "Manage GCP connectivity tests",
	Long: `Create and manage Google Cloud Network Connectivity Tests to validate network paths between resources.

Connectivity tests help you diagnose network connectivity issues by simulating traffic and analyzing
the network path between source and destination endpoints.`,
}

func init() {
	gcpCmd.AddCommand(connectivityTestCmd)
}
