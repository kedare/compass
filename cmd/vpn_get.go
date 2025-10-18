package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/output"
	"github.com/spf13/cobra"
)

var (
	vpnGetType   string
	vpnGetRegion string
	vpnGetOutput string
)

var vpnGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show details for a Cloud VPN gateway or tunnel",
	Long: `Fetches a specific Cloud VPN resource and displays rich metadata including
interfaces, linked tunnels, and BGP session state. Use --type to select either
'gateway' or 'tunnel'.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runVPNGet(cmd.Context(), strings.TrimSpace(args[0]))
	},
}

func runVPNGet(ctx context.Context, name string) {
	if name == "" {
		logger.Log.Fatalf("resource name must not be empty")
	}

	resourceType := strings.ToLower(vpnGetType)
	switch resourceType {
	case "gateway", "tunnel":
	default:
		logger.Log.Fatalf("invalid type %q; must be 'gateway' or 'tunnel'", vpnGetType)
	}

	client, err := gcp.NewClient(ctx, project)
	if err != nil {
		logger.Log.Fatalf("Failed to create GCP client: %v", err)
	}

	spin := output.NewSpinner(fmt.Sprintf("Fetching Cloud VPN %s details", resourceType))
	spin.Start()

	progress := func(message string) {
		if message == "" {
			return
		}

		spin.Update(message)
		logger.Log.Debug(message)
	}

	regionFilter := strings.TrimSpace(vpnGetRegion)

	switch resourceType {
	case "gateway":
		info, err := client.GetVPNGatewayOverview(ctx, regionFilter, name, progress)
		if err != nil {
			spin.Fail("Failed to collect gateway data")
			logger.Log.Fatalf("Failed to fetch VPN gateway: %v", err)
		}

		spin.Success("Cloud VPN data retrieved")

		if err := output.DisplayVPNGateway(info, vpnGetOutput); err != nil {
			logger.Log.Fatalf("Failed to render VPN gateway: %v", err)
		}
	case "tunnel":
		info, err := client.GetVPNTunnelOverview(ctx, regionFilter, name, progress)
		if err != nil {
			spin.Fail("Failed to collect tunnel data")
			logger.Log.Fatalf("Failed to fetch VPN tunnel: %v", err)
		}

		spin.Success("Cloud VPN data retrieved")

		if err := output.DisplayVPNTunnel(info, vpnGetOutput); err != nil {
			logger.Log.Fatalf("Failed to render VPN tunnel: %v", err)
		}
	}
}

func init() {
	vpnCmd.AddCommand(vpnGetCmd)

	vpnGetCmd.Flags().StringVarP(&vpnGetType, "type", "t", "gateway", "Resource type: gateway or tunnel")
	vpnGetCmd.Flags().StringVar(&vpnGetRegion, "region", "", "Optional region to disambiguate names")
	vpnGetCmd.Flags().StringVarP(&vpnGetOutput, "output", "o",
		output.DefaultFormat("text", []string{"text", "table", "json"}),
		"Output format: text, table, json")
}
