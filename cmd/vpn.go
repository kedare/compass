package cmd

import "github.com/spf13/cobra"

var vpnCmd = &cobra.Command{
	Use:   "vpn",
	Short: "Inspect GCP Cloud VPN resources",
	Long: `Explore Cloud VPN gateways, tunnels, and BGP sessions in your project.
Use subcommands such as "overview" for a consolidated view across regions.`,
}

func init() {
	gcpCmd.AddCommand(vpnCmd)
}
