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

func DisplayVPNGateway(gw *gcp.VPNGatewayInfo, format string) error {
	switch strings.ToLower(format) {
	case "json":
		return displayJSON(gw)
	case "table":
		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.SetStyle(table.StyleLight)
		t.AppendHeader(table.Row{"Gateway", "Region", "Network", "Interfaces", "Tunnels"})
		t.AppendRow(table.Row{
			gw.Name,
			gw.Region,
			resourceName(gw.Network),
			len(gw.Interfaces),
			len(gw.Tunnels),
		})
		t.Render()

		return nil
	default:
		return renderGatewayText(gw)
	}
}

func DisplayVPNTunnel(tunnel *gcp.VPNTunnelInfo, format string) error {
	switch strings.ToLower(format) {
	case "json":
		return displayJSON(tunnel)
	case "table":
		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.SetStyle(table.StyleLight)
		t.AppendHeader(table.Row{"Tunnel", "Region", "Status", "Router", "BGP Peers"})
		peers := tunnelPeerSummary(tunnel)
		t.AppendRow(table.Row{
			colorTunnelName(tunnel),
			tunnel.Region,
			colorStatus(tunnel.Status),
			tunnel.RouterName,
			peers,
		})
		t.Render()

		return nil
	default:
		return renderTunnelText(tunnel)
	}
}

func renderGatewayText(gw *gcp.VPNGatewayInfo) error {
	if gw == nil {
		fmt.Println("VPN gateway not found")

		return nil
	}

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

	if len(gw.Labels) > 0 {
		fmt.Println("  Labels:")

		for k, v := range gw.Labels {
			fmt.Printf("    %s: %s\n", k, v)
		}
	}

	if len(gw.Tunnels) > 0 {
		fmt.Println("  Tunnels:")

		for _, tunnel := range sortedTunnels(gw.Tunnels) {
			fmt.Printf("    • %s [%s]\n", colorTunnelName(tunnel), colorStatus(tunnel.Status))

			if tunnel.PeerIP != "" {
				fmt.Printf("      IPSec Peers: %s\n", formatIPSecPeers(tunnel))
			}

			if tunnel.RouterName != "" {
				fmt.Printf("      Router: %s\n", tunnel.RouterName)
			}

			if len(tunnel.BgpSessions) > 0 {
				fmt.Println("      BGP Peers:")

				for _, peer := range sortedPeers(tunnel.BgpSessions) {
					fmt.Printf("        - %s\n", formatPeerDetail(peer))

					if len(peer.AdvertisedPrefixes) > 0 {
						fmt.Printf("          Advertised: %s\n", strings.Join(peer.AdvertisedPrefixes, ", "))
					}

					if len(peer.LearnedPrefixes) > 0 {
						fmt.Printf("          Learned:    %s\n", strings.Join(peer.LearnedPrefixes, ", "))
					}
				}
			}
		}
	}

	return nil
}

func renderTunnelText(tunnel *gcp.VPNTunnelInfo) error {
	if tunnel == nil {
		fmt.Println("VPN tunnel not found")

		return nil
	}

	fmt.Printf("🔗 Tunnel: %s (%s)\n", colorTunnelName(tunnel), tunnel.Region)

	if tunnel.Description != "" {
		fmt.Printf("  Description:  %s\n", tunnel.Description)
	}

	if tunnel.Status != "" {
		fmt.Printf("  Status:       %s\n", colorStatus(tunnel.Status))
	}

	if tunnel.DetailedStatus != "" {
		fmt.Printf("  Detail:       %s\n", tunnel.DetailedStatus)
	}

	if tunnel.PeerIP != "" {
		fmt.Printf("  IPSec Peers:  %s\n", formatIPSecPeers(tunnel))
	}

	if tunnel.PeerGateway != "" {
		fmt.Printf("  Peer Gateway: %s\n", resourceName(tunnel.PeerGateway))
	}

	if tunnel.PeerExternal != "" {
		fmt.Printf("  Peer External:%s\n", resourceName(tunnel.PeerExternal))
	}

	if tunnel.RouterName != "" {
		fmt.Printf("  Router:       %s\n", tunnel.RouterName)
	}

	if tunnel.IkeVersion != 0 {
		fmt.Printf("  IKE Version:  %d\n", tunnel.IkeVersion)
	}

	if tunnel.SharedSecretHash != "" {
		fmt.Printf("  Secret Hash:  %s\n", tunnel.SharedSecretHash)
	}

	if len(tunnel.BgpSessions) > 0 {
		fmt.Println("  BGP Peers:")

		for _, peer := range sortedPeers(tunnel.BgpSessions) {
			fmt.Printf("    - %s\n", formatPeerDetail(peer))

			if len(peer.AdvertisedPrefixes) > 0 {
				fmt.Printf("      Advertised: %s\n", strings.Join(peer.AdvertisedPrefixes, ", "))
			}

			if len(peer.LearnedPrefixes) > 0 {
				fmt.Printf("      Learned:    %s\n", strings.Join(peer.LearnedPrefixes, ", "))
			}
		}
	}

	return nil
}

func tunnelPeerSummary(tunnel *gcp.VPNTunnelInfo) string {
	if tunnel == nil || len(tunnel.BgpSessions) == 0 {
		return "none"
	}

	summaries := make([]string, 0, len(tunnel.BgpSessions))
	for _, peer := range sortedPeers(tunnel.BgpSessions) {
		summaries = append(summaries, formatPeerDetail(peer))
	}

	return strings.Join(summaries, ", ")
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
				fmt.Printf("      IPSec Peers:  %s\n", formatIPSecPeers(tunnel))
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
					fmt.Printf("        - %s\n", formatPeerDetail(peer))
					if len(peer.AdvertisedPrefixes) > 0 {
						fmt.Printf("          Advertised: %s\n", strings.Join(peer.AdvertisedPrefixes, ", "))
					}
					if len(peer.LearnedPrefixes) > 0 {
						fmt.Printf("          Learned:    %s\n", strings.Join(peer.LearnedPrefixes, ", "))
					}
				}
			}
		}

		fmt.Println()
	}

	if len(data.OrphanTunnels) > 0 {
		fmt.Println("⚠️  Orphan Tunnels (not attached to HA VPN gateways):")

		for _, tunnel := range sortedTunnels(data.OrphanTunnels) {
			fmt.Printf("  • %s (%s) peers %s\n", colorTunnelName(tunnel), tunnel.Region, formatIPSecPeers(tunnel))

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
			fmt.Printf("  • %s on router %s (%s) endpoints %s AS%d status %s, learned %d, advertised %d\n",
				colorPeerName(peer),
				peer.RouterName,
				peer.Region,
				formatEndpointPair(peer),
				peer.PeerASN,
				colorPeerStatus(peer),
				peer.LearnedRoutes,
				peer.AdvertisedCount,
			)
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
	detail.AppendHeader(table.Row{"Gateway", "Tunnel", "Region", "Status", "Router", "BGP Peers"})

	for _, gw := range sortedGateways(data.Gateways) {
		for _, tunnel := range sortedTunnels(gw.Tunnels) {
			peerNames := make([]string, 0, len(tunnel.BgpSessions))

			for _, peer := range sortedPeers(tunnel.BgpSessions) {
				display := fmt.Sprintf("%s %s [%s L%d/A%d]",
					colorPeerName(peer),
					formatEndpointPair(peer),
					colorPeerStatus(peer),
					peer.LearnedRoutes,
					peer.AdvertisedCount,
				)
				peerNames = append(peerNames, display)
			}

			detail.AppendRow(table.Row{
				gw.Name,
				colorTunnelName(tunnel),
				tunnel.Region,
				colorStatus(tunnel.Status),
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
		orphans.AppendHeader(table.Row{"Orphan Tunnel", "Region", "Status", "IPSec Peers", "Router"})

		for _, tunnel := range sortedTunnels(data.OrphanTunnels) {
			orphans.AppendRow(table.Row{
				colorTunnelName(tunnel),
				tunnel.Region,
				colorStatus(tunnel.Status),
				formatIPSecPeers(tunnel),
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
		bgps.AppendHeader(table.Row{"BGP Peer", "Router", "Region", "BGP Peers", "ASN", "State", "Learned", "Advertised"})

		for _, peer := range sortedPeers(data.OrphanSessions) {
			bgps.AppendRow(table.Row{
				colorPeerName(peer),
				peer.RouterName,
				peer.Region,
				formatEndpointPair(peer),
				peer.PeerASN,
				colorPeerStatus(peer),
				peer.LearnedRoutes,
				peer.AdvertisedCount,
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

func colorPeerStatus(peer *gcp.BGPSessionInfo) string {
	if peer == nil {
		return applyStyle("UNKNOWN", "warn")
	}

	status := strings.ToUpper(strings.TrimSpace(peer.SessionStatus))
	if status == "" {
		status = "UNKNOWN"
	}

	state := strings.ToUpper(strings.TrimSpace(peer.SessionState))
	label := status

	if state != "" && !strings.EqualFold(status, state) {
		label = fmt.Sprintf("%s/%s", status, state)
	}

	return applyStyle(label, classifyStatus(status))
}

func classifyStatus(status string) string {
	value := strings.ToUpper(strings.TrimSpace(status))
	if value == "" {
		return "unknown"
	}

	goodTokens := []string{"ESTABLISHED", "UP"}
	badTokens := []string{"FAILED", "DOWN", "REJECTED", "ERROR", "STOPPED", "DEPROVISIONING", "NO_INCOMING_PACKETS", "NEGOTIATION_FAILURE", "PEER_IDENTITY_MISMATCH"}
	warnTokens := []string{"PROVISIONING", "WAIT", "ALLOC", "FIRST_HANDSHAKE", "START", "CONNECT"}

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

func formatEndpointPair(peer *gcp.BGPSessionInfo) string {
	if peer == nil {
		return "? ↔ ?"
	}

	local := strings.TrimSpace(peer.LocalIP)
	if local == "" {
		local = "?"
	}

	remote := strings.TrimSpace(peer.PeerIP)
	if remote == "" {
		remote = "?"
	}

	return fmt.Sprintf("%s ↔ %s", local, remote)
}

func formatIPSecPeers(tunnel *gcp.VPNTunnelInfo) string {
	if tunnel == nil {
		return "? ↔ ?"
	}

	local := strings.TrimSpace(tunnel.LocalGatewayIP)
	if local == "" {
		for _, peer := range tunnel.BgpSessions {
			if peer == nil {
				continue
			}

			if v := strings.TrimSpace(peer.LocalIP); v != "" {
				local = v

				break
			}
		}
	}

	if local == "" {
		local = "?"
	}

	remote := strings.TrimSpace(tunnel.PeerIP)
	if remote == "" {
		remote = "?"
	}

	return fmt.Sprintf("%s ↔ %s", local, remote)
}

func formatPeerDetail(peer *gcp.BGPSessionInfo) string {
	if peer == nil {
		return "?"
	}

	return fmt.Sprintf("%s endpoints %s (AS%d) status %s, learned %d, advertised %d",
		colorPeerName(peer),
		formatEndpointPair(peer),
		peer.PeerASN,
		colorPeerStatus(peer),
		peer.LearnedRoutes,
		peer.AdvertisedCount,
	)
}
