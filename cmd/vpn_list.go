package cmd

import (
	"context"

	"compass/internal/gcp"
	"compass/internal/logger"
	"compass/internal/output"
	"github.com/spf13/cobra"
)

var vpnOutputFormat string

var vpnListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"l", "ls"},
	Short:   "List Cloud VPN gateways, tunnels, and BGP peers",
	Long: `Display a consolidated view of Cloud VPN infrastructure, including HA VPN gateways,
associated tunnels, and Cloud Router BGP peers. Supports text, table, and JSON formats.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runVPNList(cmd.Context())
	},
}

func runVPNList(ctx context.Context) {
	logger.Log.Debug("Fetching VPN inventory")

	client, err := gcp.NewClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create GCP client: %v", err)
	}

	spin := output.NewSpinner("Collecting Cloud VPN data")
	spin.Start()

	progress := func(message string) {
		if message == "" {
			return
		}

		spin.Update(message)
		logger.Log.Debug(message)
	}

	overview, err := client.ListVPNOverview(ctx, progress)
	if err != nil {
		spin.Fail("Failed to collect Cloud VPN data")
		logger.Log.Fatalf("Failed to fetch VPN overview: %v", err)
	}

	spin.Success("Cloud VPN data retrieved")

	if err := output.DisplayVPNOverview(overview, vpnOutputFormat); err != nil {
		logger.Log.Fatalf("Failed to render VPN overview: %v", err)
	}
}

func init() {
	vpnCmd.AddCommand(vpnListCmd)

	vpnListCmd.Flags().StringVarP(&vpnOutputFormat, "output", "o",
		output.DefaultFormat("text", []string{"text", "table", "json"}),
		"Output format: text, table, json")
}
