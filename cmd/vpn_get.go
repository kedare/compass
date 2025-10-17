package cmd

import (
	"context"
	"fmt"
	"strings"

	"compass/internal/gcp"
	"compass/internal/logger"
	"compass/internal/output"
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

	overview, err := client.ListVPNOverview(ctx, progress)
	if err != nil {
		spin.Fail("Failed to collect Cloud VPN data")
		logger.Log.Fatalf("Failed to fetch VPN overview: %v", err)
	}

	spin.Success("Cloud VPN data retrieved")

	var matches []*gcp.VPNGatewayInfo
	regionFilter := strings.TrimSpace(vpnGetRegion)

	if resourceType == "gateway" {
		matches = filterGatewaysByName(overview.Gateways, name, regionFilter)
		if len(matches) == 0 {
			logger.Log.Fatalf("No VPN gateway named %q found", name)
		}
		if len(matches) > 1 {
			logger.Log.Fatalf("Multiple VPN gateways named %q found; please specify --region", name)
		}
		if err := output.DisplayVPNGateway(matches[0], vpnGetOutput); err != nil {
			logger.Log.Fatalf("Failed to render VPN gateway: %v", err)
		}

		return
	}

	tunnel := selectTunnel(overview, name, regionFilter)
	if tunnel == nil {
		logger.Log.Fatalf("No VPN tunnel named %q found", name)
	}

	if err := output.DisplayVPNTunnel(tunnel, vpnGetOutput); err != nil {
		logger.Log.Fatalf("Failed to render VPN tunnel: %v", err)
	}
}

func filterGatewaysByName(gateways []*gcp.VPNGatewayInfo, name, region string) []*gcp.VPNGatewayInfo {
	var result []*gcp.VPNGatewayInfo
	for _, gw := range gateways {
		if !strings.EqualFold(gw.Name, name) {
			continue
		}
		if region != "" && !strings.EqualFold(gw.Region, region) {
			continue
		}
		result = append(result, gw)
	}
	return result
}

func selectTunnel(overview *gcp.VPNOverview, name, region string) *gcp.VPNTunnelInfo {
	matches := make([]*gcp.VPNTunnelInfo, 0)

	check := func(tunnel *gcp.VPNTunnelInfo) {
		if !strings.EqualFold(tunnel.Name, name) {
			return
		}
		if region != "" && !strings.EqualFold(tunnel.Region, region) {
			return
		}
		matches = append(matches, tunnel)
	}

	for _, gw := range overview.Gateways {
		for _, tunnel := range gw.Tunnels {
			check(tunnel)
		}
	}

	for _, tunnel := range overview.OrphanTunnels {
		check(tunnel)
	}

	if len(matches) == 0 {
		return nil
	}

	if len(matches) > 1 {
		logger.Log.Fatalf("Multiple VPN tunnels named %q found; please specify --region", name)
	}

	return matches[0]
}

func init() {
	vpnCmd.AddCommand(vpnGetCmd)

	vpnGetCmd.Flags().StringVarP(&vpnGetType, "type", "t", "gateway", "Resource type: gateway or tunnel")
	vpnGetCmd.Flags().StringVar(&vpnGetRegion, "region", "", "Optional region to disambiguate names")
	vpnGetCmd.Flags().StringVarP(&vpnGetOutput, "output", "o",
		output.DefaultFormat("text", []string{"text", "table", "json"}),
		"Output format: text, table, json")
}
