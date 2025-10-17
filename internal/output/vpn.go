package output

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"compass/internal/gcp"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// DisplayVPNOverview renders VPN gateway/tunnel/BGP data in the requested format.
func DisplayVPNOverview(data *gcp.VPNOverview, format string) error {
	switch strings.ToLower(format) {
	case "json":
		return displayJSON(data)
	case "table":
		return displayVPNTable(data)
	default:
		return displayVPNText(data)
	}
}

func displayVPNText(data *gcp.VPNOverview) error {
	if data == nil || len(data.Gateways) == 0 && len(data.OrphanTunnels) == 0 && len(data.OrphanSessions) == 0 {
		fmt.Println("No VPN resources found")

		return nil
	}

	for _, gw := range sortedGateways(data.Gateways) {
		fmt.Printf("🔐 Gateway: %s (%s)\n", gw.Name, gw.Region)

		if gw.Description != "" {
			fmt.Printf("  Description: %s\n", gw.Description)
		}

		if gw.Network != "" {
			fmt.Printf("  Network:     %s\n", resourceName(gw.Network))
		}

		if len(gw.Interfaces) > 0 {
			fmt.Println("  Interfaces:")

			for _, iface := range gw.Interfaces {
				fmt.Printf("    - #%d IP: %s\n", iface.Id, iface.IpAddress)
			}
		}

		if len(gw.Tunnels) == 0 {
			fmt.Println("  Tunnels:     none")
			fmt.Println()

			continue
		}

		fmt.Println("  Tunnels:")

		for _, tunnel := range sortedTunnels(gw.Tunnels) {
			fmt.Printf("    • %s (%s)\n", colorTunnelName(tunnel), tunnel.Region)

			if strings.TrimSpace(tunnel.PeerIP) != "" {
				fmt.Printf("      Peer IP:      %s\n", tunnel.PeerIP)
			}

			if tunnel.PeerGateway != "" {
				fmt.Printf("      Peer Gateway: %s\n", resourceName(tunnel.PeerGateway))
			}

			if tunnel.PeerExternal != "" {
				fmt.Printf("      Peer External: %s\n", resourceName(tunnel.PeerExternal))
			}

			if tunnel.RouterName != "" {
				fmt.Printf("      Router:       %s\n", tunnel.RouterName)
			}

			if tunnel.Status != "" {
				fmt.Printf("      Status:       %s\n", colorStatus(tunnel.Status))
			}

			if tunnel.DetailedStatus != "" {
				fmt.Printf("      Detail:       %s\n", tunnel.DetailedStatus)
			}

			if tunnel.IkeVersion != 0 {
				fmt.Printf("      IKE Version:  %d\n", tunnel.IkeVersion)
			}

			if len(tunnel.BgpSessions) > 0 {
				fmt.Println("      BGP Peers:")

				for _, peer := range sortedPeers(tunnel.BgpSessions) {
					fmt.Printf("        - %s (%s, ASN %d, %s)\n", colorPeerName(peer), peer.PeerIP, peer.PeerASN, colorPeerState(peer))
				}
			}
		}

		fmt.Println()
	}

	if len(data.OrphanTunnels) > 0 {
		fmt.Println("⚠️  Orphan Tunnels (not attached to HA VPN gateways):")

		for _, tunnel := range sortedTunnels(data.OrphanTunnels) {
			fmt.Printf("  • %s (%s) peer %s\n", colorTunnelName(tunnel), tunnel.Region, tunnel.PeerIP)

			if tunnel.RouterName != "" {
				fmt.Printf("    Router: %s\n", tunnel.RouterName)
			}

			if tunnel.Status != "" {
				fmt.Printf("    Status: %s\n", colorStatus(tunnel.Status))
			}
		}

		fmt.Println()
	}

	if len(data.OrphanSessions) > 0 {
		fmt.Println("⚠️  Orphan BGP Sessions (no tunnel association):")

		for _, peer := range sortedPeers(data.OrphanSessions) {
			fmt.Printf("  • %s on router %s (%s) peer %s ASN %d\n",
				peer.Name, peer.RouterName, peer.Region, peer.PeerIP, peer.PeerASN)
		}
	}

	return nil
}

func displayVPNTable(data *gcp.VPNOverview) error {
	if data == nil || len(data.Gateways) == 0 {
		fmt.Println("No VPN gateways found")

		return nil
	}

	tw := table.NewWriter()
	tw.SetOutputMirror(os.Stdout)
	tw.SetStyle(table.StyleRounded)

	tw.AppendHeader(table.Row{"Gateway", "Region", "Network", "#Interfaces", "#Tunnels"})

	for _, gw := range sortedGateways(data.Gateways) {
		tw.AppendRow(table.Row{
			gw.Name,
			gw.Region,
			resourceName(gw.Network),
			len(gw.Interfaces),
			len(gw.Tunnels),
		})
	}

	tw.Render()
	fmt.Println()

	detail := table.NewWriter()
	detail.SetOutputMirror(os.Stdout)
	detail.SetStyle(table.StyleLight)
	detail.AppendHeader(table.Row{"Gateway", "Tunnel", "Region", "Status", "Peer IP", "Router", "BGP Peers"})

	for _, gw := range sortedGateways(data.Gateways) {
		for _, tunnel := range sortedTunnels(gw.Tunnels) {
			peerNames := make([]string, 0, len(tunnel.BgpSessions))

			for _, peer := range sortedPeers(tunnel.BgpSessions) {
				display := fmt.Sprintf("%s (%s)", colorPeerName(peer), colorPeerState(peer))
				peerNames = append(peerNames, display)
			}

			detail.AppendRow(table.Row{
				gw.Name,
				colorTunnelName(tunnel),
				tunnel.Region,
				colorStatus(tunnel.Status),
				tunnel.PeerIP,
				tunnel.RouterName,
				strings.Join(peerNames, ", "),
			})
		}
	}

	if detail.Length() > 0 {
		detail.Render()
	}

	if len(data.OrphanTunnels) > 0 {
		fmt.Println()
		orphans := table.NewWriter()
		orphans.SetOutputMirror(os.Stdout)
		orphans.SetStyle(table.StyleLight)
		orphans.AppendHeader(table.Row{"Orphan Tunnel", "Region", "Peer IP", "Router"})

		for _, tunnel := range sortedTunnels(data.OrphanTunnels) {
			orphans.AppendRow(table.Row{
				tunnel.Name,
				tunnel.Region,
				tunnel.PeerIP,
				tunnel.RouterName,
			})
		}

		orphans.Render()
	}

	if len(data.OrphanSessions) > 0 {
		fmt.Println()
		bgps := table.NewWriter()
		bgps.SetOutputMirror(os.Stdout)
		bgps.SetStyle(table.StyleLight)
		bgps.AppendHeader(table.Row{"BGP Peer", "Router", "Region", "Peer IP", "ASN", "State"})

		for _, peer := range sortedPeers(data.OrphanSessions) {
			bgps.AppendRow(table.Row{
				colorPeerName(peer),
				peer.RouterName,
				peer.Region,
				peer.PeerIP,
				peer.PeerASN,
				colorPeerState(peer),
			})
		}

		bgps.Render()
	}

	return nil
}

func sortedGateways(gateways []*gcp.VPNGatewayInfo) []*gcp.VPNGatewayInfo {
	result := append([]*gcp.VPNGatewayInfo{}, gateways...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Region == result[j].Region {
			return result[i].Name < result[j].Name
		}

		return result[i].Region < result[j].Region
	})

	return result
}

func sortedTunnels(tunnels []*gcp.VPNTunnelInfo) []*gcp.VPNTunnelInfo {
	result := append([]*gcp.VPNTunnelInfo{}, tunnels...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Region == result[j].Region {
			return result[i].Name < result[j].Name
		}

		return result[i].Region < result[j].Region
	})

	return result
}

func sortedPeers(peers []*gcp.BGPSessionInfo) []*gcp.BGPSessionInfo {
	result := append([]*gcp.BGPSessionInfo{}, peers...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].RouterName == result[j].RouterName {
			return result[i].Name < result[j].Name
		}

		return result[i].RouterName < result[j].RouterName
	})

	return result
}

func resourceName(link string) string {
	if link == "" {
		return ""
	}
	parts := strings.Split(link, "/")

	return parts[len(parts)-1]
}

func colorTunnelName(tunnel *gcp.VPNTunnelInfo) string {
	if tunnel == nil {
		return ""
	}
	style := classifyStatus(tunnel.Status)
	return applyStyle(tunnel.Name, style)
}

func colorStatus(status string) string {
	style := classifyStatus(status)
	return applyStyle(status, style)
}

func colorPeerName(peer *gcp.BGPSessionInfo) string {
	if peer == nil {
		return ""
	}
	if peer.Enabled {
		return text.Colors{text.Bold, text.FgGreen}.Sprint(peer.Name)
	}
	return text.Colors{text.FgYellow}.Sprint(peer.Name)
}

func colorPeerState(peer *gcp.BGPSessionInfo) string {
	if peer == nil {
		return "unknown"
	}
	if peer.Enabled {
		return text.Colors{text.Bold, text.FgGreen}.Sprint("enabled")
	}
	return text.Colors{text.FgYellow}.Sprint("disabled")
}

func classifyStatus(status string) string {
	value := strings.ToUpper(strings.TrimSpace(status))
	if value == "" {
		return "unknown"
	}

	goodTokens := []string{"ESTABLISHED"}
	badTokens := []string{"FAILED", "DOWN", "REJECTED", "ERROR", "STOPPED", "DEPROVISIONING", "NO_INCOMING_PACKETS", "NEGOTIATION_FAILURE", "PEER_IDENTITY_MISMATCH"}
	warnTokens := []string{"PROVISIONING", "WAIT", "ALLOC", "FIRST_HANDSHAKE"}

	for _, token := range goodTokens {
		if strings.Contains(value, token) {
			return "good"
		}
	}

	for _, token := range badTokens {
		if strings.Contains(value, token) {
			return "bad"
		}
	}

	for _, token := range warnTokens {
		if strings.Contains(value, token) {
			return "warn"
		}
	}

	return "neutral"
}

func applyStyle(textValue, style string) string {
	if strings.TrimSpace(textValue) == "" {
		return textValue
	}

	switch style {
	case "good":
		return text.Colors{text.Bold, text.FgGreen}.Sprint(textValue)
	case "bad":
		return text.Colors{text.Bold, text.FgRed}.Sprint(textValue)
	case "warn":
		return text.Colors{text.Bold, text.FgYellow}.Sprint(textValue)
	default:
		return textValue
	}
}
