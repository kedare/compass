package cmd

import "github.com/spf13/cobra"

var ipCmd = &cobra.Command{
	Use:   "ip",
	Short: "Inspect IP addresses across GCP resources",
	Long: `Discover which GCP resources reference specific IP addresses.
Use subcommands like "lookup" to search instances, load balancers, and reserved addresses.`,
}

func init() {
	gcpCmd.AddCommand(ipCmd)
}
