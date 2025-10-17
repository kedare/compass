// Package output provides formatting and display functions for command-line output.
// This file contains rendering functions for Cloud VPN resources including gateways,
// tunnels, and BGP sessions with support for multiple output formats.
package output

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"codeberg.org/kedare/compass/internal/gcp"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

const (
	// Status classification constants.
	statusClassGood    = "good"
	statusClassBad     = "bad"
	statusClassWarn    = "warn"
	statusClassNeutral = "neutral"
	statusClassUnknown = "unknown"

	// Status token constants.
	statusEstablished          = "ESTABLISHED"
	statusUp                   = "UP"
	statusFailed               = "FAILED"
	statusDown                 = "DOWN"
	statusRejected             = "REJECTED"
	statusError                = "ERROR"
	statusStopped              = "STOPPED"
	statusDeprovisioning       = "DEPROVISIONING"
	statusNoIncomingPackets    = "NO_INCOMING_PACKETS"
	statusNegotiationFailure   = "NEGOTIATION_FAILURE"
	statusPeerIdentityMismatch = "PEER_IDENTITY_MISMATCH"
	statusProvisioning         = "PROVISIONING"
	statusWait                 = "WAIT"
	statusAlloc                = "ALLOC"
	statusFirstHandshake       = "FIRST_HANDSHAKE"
	statusStart                = "START"
	statusConnect              = "CONNECT"
	statusUnknown              = "UNKNOWN"

	// Display constants.
	displayNone           = "none"
	displayUnknownPair    = "? ↔ ?"
	displayUnknownChar    = "?"
	displayArrowSeparator = " ↔ "
)

// DisplayVPNOverview renders a complete Cloud VPN infrastructure overview to stdout.
//
// Supported formats:
//   - "json": Raw JSON output of the complete data structure
//   - "table": Tabular view with gateways, tunnels, and BGP peers in separate tables
//   - "text" (default): Hierarchical text view with color-coded status indicators
//
// The text format includes:
//   - Gateway information with interfaces and attached tunnels
//   - Tunnel details with IPSec peers and BGP sessions
//   - BGP peer status with advertised and received route prefixes
//   - Orphan resources (tunnels and BGP sessions without proper associations)
//   - Warning sections (when showWarnings is true) for configuration issues
func DisplayVPNOverview(data *gcp.VPNOverview, format string, showWarnings bool) error {
	switch strings.ToLower(format) {
	case "json":
		return displayJSON(data)
	case "table":
		return displayVPNTable(data, showWarnings)
	default:
		return displayVPNText(data, showWarnings)
	}
}

// DisplayVPNGateway renders details for a single VPN gateway to stdout.
//
// Supported formats:
//   - "json": Raw JSON output
//   - "table": Single-row table with gateway summary
//   - "text" (default): Detailed text view including tunnels and BGP sessions
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

// DisplayVPNTunnel renders details for a single VPN tunnel to stdout.
//
// Supported formats:
//   - "json": Raw JSON output
//   - "table": Single-row table with tunnel summary and BGP peer count
//   - "text" (default): Detailed text view including IPSec config, BGP sessions, and route prefixes
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

// renderGatewayText renders a single VPN gateway in text format with its associated tunnels.
// Returns an error if rendering fails (currently always returns nil).
func renderGatewayText(gw *gcp.VPNGatewayInfo) error {
	if gw == nil {
		fmt.Println("VPN gateway not found")

		return nil
	}

	renderGatewayHeader(gw, "")

	if len(gw.Tunnels) > 0 {
		fmt.Println("  Tunnels:")

		for _, tunnel := range sortedTunnels(gw.Tunnels) {
			renderTunnelDetails(tunnel, "    ", false)
		}
	}

	return nil
}

// renderTunnelText renders a single VPN tunnel in text format with full details.
// Includes tunnel status, IPSec configuration, BGP sessions, and route information.
// Returns an error if rendering fails (currently always returns nil).
func renderTunnelText(tunnel *gcp.VPNTunnelInfo) error {
	if tunnel == nil {
		fmt.Println("VPN tunnel not found")

		return nil
	}

	fmt.Printf("🔗 Tunnel: %s (%s)\n", colorTunnelName(tunnel), tunnel.Region)

	if tunnel.Description != "" {
		indentPrintf("", 1, "Description:  %s\n", tunnel.Description)
	}

	renderTunnelDetailsBody(tunnel, "", 1, true, true)

	return nil
}

// tunnelPeerSummary generates a comma-separated summary of all BGP peers for a tunnel.
// Returns "none" if the tunnel is nil or has no BGP sessions.
// Each peer summary includes name, endpoints, ASN, status, and route counts.
func tunnelPeerSummary(tunnel *gcp.VPNTunnelInfo) string {
	if tunnel == nil || len(tunnel.BgpSessions) == 0 {
		return displayNone
	}

	summaries := make([]string, 0, len(tunnel.BgpSessions))
	for _, peer := range sortedPeers(tunnel.BgpSessions) {
		summaries = append(summaries, formatPeerDetail(peer))
	}

	return strings.Join(summaries, ", ")
}

// displayVPNText renders the complete VPN overview in hierarchical text format.
// Shows gateways with their tunnels, followed by any orphan tunnels and sessions.
// If showWarnings is true, includes warning sections for configuration issues.
// Returns an error if rendering fails (currently always returns nil).
func displayVPNText(data *gcp.VPNOverview, showWarnings bool) error {
	if data == nil || len(data.Gateways) == 0 && len(data.OrphanTunnels) == 0 && len(data.OrphanSessions) == 0 {
		fmt.Println("No VPN resources found")

		return nil
	}

	for _, gw := range sortedGateways(data.Gateways) {
		renderGatewayHeader(gw, "")

		if len(gw.Tunnels) == 0 {
			fmt.Println("  Tunnels:     none")
			fmt.Println()

			continue
		}

		fmt.Println("  Tunnels:")

		for _, tunnel := range sortedTunnels(gw.Tunnels) {
			renderTunnelDetails(tunnel, "    ", true)
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
			fmt.Printf("  • %s on router %s (%s) endpoints %s status %s, received %d, advertised %d\n",
				colorPeerName(peer),
				peer.RouterName,
				peer.Region,
				formatEndpointPair(peer),
				colorPeerStatus(peer),
				peer.LearnedRoutes,
				peer.AdvertisedCount,
			)
		}

		fmt.Println()
	}

	// Only collect and display warnings if requested
	if showWarnings {
		// Collect gateways and BGP peers with issues
		var gatewaysWithNoTunnels []*gcp.VPNGatewayInfo
		var gatewaysWithOneTunnel []*gcp.VPNGatewayInfo
		var peersWithNoAdvertised []*gcp.BGPSessionInfo

		// Track tunnels with no received routes (tunnel name + peer info)
		type tunnelPeerInfo struct {
			tunnel *gcp.VPNTunnelInfo
			peer   *gcp.BGPSessionInfo
		}
		var tunnelsWithNoReceived []tunnelPeerInfo

		for _, gw := range data.Gateways {
			// Check for gateways with no tunnels
			if len(gw.Tunnels) == 0 {
				gatewaysWithNoTunnels = append(gatewaysWithNoTunnels, gw)
				continue
			}

			// Check for HA gateways with only one tunnel (not truly HA)
			if len(gw.Interfaces) >= 2 && len(gw.Tunnels) == 1 {
				gatewaysWithOneTunnel = append(gatewaysWithOneTunnel, gw)
			}

			// Check BGP peers for routing issues
			for _, tunnel := range gw.Tunnels {
				for _, peer := range tunnel.BgpSessions {
					if peer == nil || !peer.Enabled {
						continue
					}

					// Check if peer is UP but not advertising any routes
					if classifyStatus(peer.SessionStatus) == statusClassGood && peer.AdvertisedCount == 0 {
						peersWithNoAdvertised = append(peersWithNoAdvertised, peer)
					}

					// Check if peer is UP but not receiving any routes
					if classifyStatus(peer.SessionStatus) == statusClassGood && peer.LearnedRoutes == 0 {
						tunnelsWithNoReceived = append(tunnelsWithNoReceived, tunnelPeerInfo{
							tunnel: tunnel,
							peer:   peer,
						})
					}
				}
			}
		}

		// Display warnings for gateways with no tunnels
		if len(gatewaysWithNoTunnels) > 0 {
			fmt.Println("⚠️  Gateways With No Tunnels:")

			for _, gw := range sortedGateways(gatewaysWithNoTunnels) {
				fmt.Printf("  • %s (%s) - %d interface(s) configured but no tunnels\n",
					text.Colors{text.Bold, text.FgYellow}.Sprint(gw.Name),
					gw.Region,
					len(gw.Interfaces),
				)
			}

			fmt.Println()
		}

		// Display warnings for HA gateways with only one tunnel
		if len(gatewaysWithOneTunnel) > 0 {
			fmt.Println("⚠️  HA Gateways With Only One Tunnel (Not Highly Available):")

			for _, gw := range sortedGateways(gatewaysWithOneTunnel) {
				tunnelName := "unknown"
				if len(gw.Tunnels) > 0 && gw.Tunnels[0] != nil {
					tunnelName = gw.Tunnels[0].Name
				}
				fmt.Printf("  • %s (%s) - %d interface(s) but only 1 tunnel: %s\n",
					text.Colors{text.Bold, text.FgYellow}.Sprint(gw.Name),
					gw.Region,
					len(gw.Interfaces),
					tunnelName,
				)
			}

			fmt.Println()
		}

		// Display warnings for peers with no advertised routes
		if len(peersWithNoAdvertised) > 0 {
			fmt.Println("⚠️  BGP Peers Not Advertising Routes:")

			for _, peer := range sortedPeers(peersWithNoAdvertised) {
				fmt.Printf("  • %s on router %s (%s) endpoints %s status %s\n",
					colorPeerName(peer),
					peer.RouterName,
					peer.Region,
					formatEndpointPair(peer),
					colorPeerStatus(peer),
				)
			}

			fmt.Println()
		}

		// Display warnings for tunnels with peers not receiving routes
		if len(tunnelsWithNoReceived) > 0 {
			fmt.Println("⚠️  Tunnels Not Receiving BGP Routes:")

			// Sort by tunnel region and name
			sort.Slice(tunnelsWithNoReceived, func(i, j int) bool {
				if tunnelsWithNoReceived[i].tunnel.Region == tunnelsWithNoReceived[j].tunnel.Region {
					return tunnelsWithNoReceived[i].tunnel.Name < tunnelsWithNoReceived[j].tunnel.Name
				}
				return tunnelsWithNoReceived[i].tunnel.Region < tunnelsWithNoReceived[j].tunnel.Region
			})

			for _, info := range tunnelsWithNoReceived {
				fmt.Printf("  • %s (%s) on router %s - peer %s status %s\n",
					colorTunnelName(info.tunnel),
					info.tunnel.Region,
					info.peer.RouterName,
					colorPeerName(info.peer),
					colorPeerStatus(info.peer),
				)
			}
		}
	}

	return nil
}

// displayVPNTable renders the complete VPN overview in table format.
// Creates separate tables for gateways, tunnel details, orphan tunnels, and orphan BGP sessions.
// The showWarnings parameter is currently not used for table format.
// Returns an error if rendering fails (currently always returns nil).
func displayVPNTable(data *gcp.VPNOverview, showWarnings bool) error {
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
				display := fmt.Sprintf("%s %s [%s R%d/A%d]",
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
		orphans.AppendHeader(table.Row{"Orphan Tunnel", "Region", "Status", "IPSec Peer", "Router"})

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
		bgps.AppendHeader(table.Row{"BGP Peer", "Router", "Region", "BGP Peers", "State", "Received", "Advertised"})

		for _, peer := range sortedPeers(data.OrphanSessions) {
			bgps.AppendRow(table.Row{
				colorPeerName(peer),
				peer.RouterName,
				peer.Region,
				formatEndpointPair(peer),
				colorPeerStatus(peer),
				peer.LearnedRoutes,
				peer.AdvertisedCount,
			})
		}

		bgps.Render()
	}

	return nil
}

// sortedGateways returns a copy of the gateway slice sorted by region, then by name.
// This ensures consistent output ordering across multiple invocations.
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

// sortedTunnels returns a copy of the tunnel slice sorted by region, then by name.
// This ensures consistent output ordering across multiple invocations.
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

// sortedPeers returns a copy of the BGP session slice sorted by router name, then by peer name.
// This ensures consistent output ordering across multiple invocations.
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

// resourceName extracts the final component from a GCP resource URL path.
// For example, "projects/foo/regions/us-central1/networks/my-net" returns "my-net".
// Returns an empty string if the input is empty.
func resourceName(link string) string {
	if link == "" {
		return ""
	}
	parts := strings.Split(link, "/")

	return parts[len(parts)-1]
}

// colorTunnelName returns the tunnel name with color formatting based on its status.
// Green for UP/ESTABLISHED, red for DOWN/FAILED, yellow for transitional states.
// Returns an empty string if the tunnel is nil.
func colorTunnelName(tunnel *gcp.VPNTunnelInfo) string {
	if tunnel == nil {
		return ""
	}
	style := classifyStatus(tunnel.Status)

	return applyStyle(tunnel.Name, style)
}

// colorStatus returns the status string with color formatting based on its classification.
// Green for good states, red for bad states, yellow for warning states, uncolored for neutral.
func colorStatus(status string) string {
	style := classifyStatus(status)

	return applyStyle(status, style)
}

// colorPeerName returns the BGP peer name with color formatting based on enabled status and session state.
// Green if enabled and UP, yellow if enabled but not UP, red if disabled.
// Returns an empty string if the peer is nil.
func colorPeerName(peer *gcp.BGPSessionInfo) string {
	style := text.Colors{text.Bold, text.FgRed}

	if peer == nil {
		return ""
	}

	status := classifyStatus(peer.SessionStatus)

	if peer.Enabled && status == statusClassBad {
		style = text.Colors{text.Bold, text.FgYellow}
	} else if peer.Enabled && status == statusClassGood {
		style = text.Colors{text.Bold, text.FgGreen}
	}

	return style.Sprint(peer.Name)
}

// colorPeerStatus returns a formatted status string for a BGP peer with color coding.
// Combines session status and state if they differ (e.g., "UP/ESTABLISHED").
// Returns "UNKNOWN" if the peer is nil or status is empty.
func colorPeerStatus(peer *gcp.BGPSessionInfo) string {
	if peer == nil {
		return applyStyle(statusUnknown, statusClassWarn)
	}

	status := strings.ToUpper(strings.TrimSpace(peer.SessionStatus))
	if status == "" {
		status = statusUnknown
	}

	state := strings.ToUpper(strings.TrimSpace(peer.SessionState))
	label := status

	if state != "" && !strings.EqualFold(status, state) {
		label = fmt.Sprintf("%s/%s", status, state)
	}

	return applyStyle(label, classifyStatus(status))
}

// classifyStatus categorizes a status string into one of: "good", "bad", "warn", "neutral", or "unknown".
// Used to determine appropriate color coding for status displays.
// Returns "unknown" for empty strings, "good" for UP/ESTABLISHED, "bad" for DOWN/FAILED/ERROR,
// "warn" for transitional states, and "neutral" for unrecognized values.
func classifyStatus(status string) string {
	value := strings.ToUpper(strings.TrimSpace(status))
	if value == "" {
		return statusClassUnknown
	}

	goodTokens := []string{statusEstablished, statusUp}
	badTokens := []string{statusFailed, statusDown, statusRejected, statusError, statusStopped, statusDeprovisioning, statusNoIncomingPackets, statusNegotiationFailure, statusPeerIdentityMismatch}
	warnTokens := []string{statusProvisioning, statusWait, statusAlloc, statusFirstHandshake, statusStart, statusConnect}

	for _, token := range goodTokens {
		if strings.Contains(value, token) {
			return statusClassGood
		}
	}

	for _, token := range badTokens {
		if strings.Contains(value, token) {
			return statusClassBad
		}
	}

	for _, token := range warnTokens {
		if strings.Contains(value, token) {
			return statusClassWarn
		}
	}

	return statusClassNeutral
}

// applyStyle applies terminal color formatting to text based on the specified style.
// Supported styles: "good" (green), "bad" (red), "warn" (yellow), or uncolored for others.
// Returns the original text unchanged if it's empty or whitespace-only.
func applyStyle(textValue, style string) string {
	if strings.TrimSpace(textValue) == "" {
		return textValue
	}

	switch style {
	case statusClassGood:
		return text.Colors{text.Bold, text.FgGreen}.Sprint(textValue)
	case statusClassBad:
		return text.Colors{text.Bold, text.FgRed}.Sprint(textValue)
	case statusClassWarn:
		return text.Colors{text.Bold, text.FgYellow}.Sprint(textValue)
	default:
		return textValue
	}
}

// formatEndpointPair formats the BGP session endpoints with ASNs as "<local AS:localASN> ↔ <remote AS:peerASN>".
// Uses "?" for missing IP values. Example: "<169.254.0.1 AS:64512> ↔ <169.254.0.2 AS:65001>".
// If ASNs are not available, falls back to simple "localIP ↔ remoteIP" format.
func formatEndpointPair(peer *gcp.BGPSessionInfo) string {
	if peer == nil {
		return displayUnknownPair
	}

	localIP := strings.TrimSpace(peer.LocalIP)
	if localIP == "" {
		localIP = displayUnknownChar
	}

	remoteIP := strings.TrimSpace(peer.PeerIP)
	if remoteIP == "" {
		remoteIP = displayUnknownChar
	}

	return fmt.Sprintf("<local %s AS%d>%s<remote %s AS%d>", localIP, peer.LocalASN, displayArrowSeparator, remoteIP, peer.PeerASN)
}

// formatIPSecPeers formats the IPSec tunnel endpoints as "local ↔ remote".
// Falls back to BGP session local IPs if the tunnel's local gateway IP is not set.
// Uses "?" for missing values. Example: "35.186.82.3 ↔ 203.0.113.1".
func formatIPSecPeers(tunnel *gcp.VPNTunnelInfo) string {
	if tunnel == nil {
		return displayUnknownPair
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
		local = displayUnknownChar
	}

	remote := strings.TrimSpace(tunnel.PeerIP)
	if remote == "" {
		remote = displayUnknownChar
	}

	return fmt.Sprintf("<local %s> %s <remote %s>", local, displayArrowSeparator, remote)
}

// formatPeerDetail creates a detailed single-line summary of a BGP peer.
// Includes peer name, endpoints, ASN, status, and route counts (received/advertised).
// Returns "?" if the peer is nil.
func formatPeerDetail(peer *gcp.BGPSessionInfo) string {
	if peer == nil {
		return displayUnknownChar
	}

	return fmt.Sprintf("%s endpoints %s status %s, received %d, advertised %d",
		colorPeerName(peer),
		formatEndpointPair(peer),
		colorPeerStatus(peer),
		peer.LearnedRoutes,
		peer.AdvertisedCount,
	)
}

// indentPrintf is a helper function that prints formatted output with additional indentation.
// It combines a base indent string with additional indent levels (each level adds 2 spaces)
// before formatting and printing the output.
//
// Example:
//
//	indentPrintf("  ", 2, "Status: %s\n", "UP")  // prints "      Status: UP\n"
//	indentPrintf("", 1, "Name: %s\n", "foo")      // prints "  Name: foo\n"
func indentPrintf(baseIndent string, levels int, format string, args ...interface{}) {
	indent := baseIndent + strings.Repeat("  ", levels)
	fmt.Printf(indent+format, args...)
}

// renderBGPPeers renders BGP peer information with the specified indentation.
// For each peer, it displays session status, endpoint IPs, ASN, and route counts.
// If available, it shows advertised and received route prefixes.
// Received routes that are selected as "best" routes are displayed in bold.
func renderBGPPeers(peers []*gcp.BGPSessionInfo, indent string) {
	if len(peers) == 0 {
		return
	}

	indentPrintf(indent, 0, "BGP Peers:\n")

	for _, peer := range sortedPeers(peers) {
		indentPrintf(indent, 1, "- %s\n", formatPeerDetail(peer))

		if len(peer.AdvertisedPrefixes) > 0 {
			indentPrintf(indent, 3, "Advertised: %s\n", strings.Join(peer.AdvertisedPrefixes, ", "))
		}

		if len(peer.LearnedPrefixes) > 0 {
			// Create a set of best route prefixes for quick lookup
			bestRoutes := make(map[string]bool)
			for _, prefix := range peer.BestRoutePrefixes {
				bestRoutes[prefix] = true
			}

			// Format each received prefix: best routes in bold, non-best routes grayed out
			formatted := make([]string, 0, len(peer.LearnedPrefixes))

			for _, prefix := range peer.LearnedPrefixes {
				if bestRoutes[prefix] {
					// Highlight best routes in bold
					formatted = append(formatted, text.Colors{text.Bold}.Sprint(prefix))
				} else {
					// Gray out non-best routes
					formatted = append(formatted, text.Colors{text.Faint}.Sprint(prefix))
				}
			}

			indentPrintf(indent, 3, "Received:   %s\n", strings.Join(formatted, ", "))
		}
	}
}

// renderGatewayHeader renders the gateway header and basic information.
// This includes the gateway name, region, description, network, interfaces, and labels.
// The output is indented according to the indent parameter for hierarchical display.
func renderGatewayHeader(gw *gcp.VPNGatewayInfo, indent string) {
	if gw == nil {
		return
	}

	indentPrintf(indent, 0, "🔐 Gateway: %s (%s)\n", gw.Name, gw.Region)

	if gw.Description != "" {
		indentPrintf(indent, 1, "Description: %s\n", gw.Description)
	}

	if gw.Network != "" {
		indentPrintf(indent, 1, "Network:     %s\n", resourceName(gw.Network))
	}

	if len(gw.Interfaces) > 0 {
		indentPrintf(indent, 1, "Interfaces:\n")

		for _, iface := range gw.Interfaces {
			indentPrintf(indent, 2, "- #%d IP: %s\n", iface.Id, iface.IpAddress)
		}
	}

	if len(gw.Labels) > 0 {
		indentPrintf(indent, 1, "Labels:\n")

		for k, v := range gw.Labels {
			indentPrintf(indent, 2, "%s: %s\n", k, v)
		}
	}
}

// renderTunnelDetails renders tunnel details with the specified indentation and whether to show the region.
// The showRegion parameter controls whether the region is displayed in the header or the status is shown instead.
// This is a wrapper around renderTunnelDetailsBody that adds the tunnel header line.
func renderTunnelDetails(tunnel *gcp.VPNTunnelInfo, indent string, showRegion bool) {
	if tunnel == nil {
		return
	}

	if showRegion {
		indentPrintf(indent, 0, "• %s (%s)\n", colorTunnelName(tunnel), tunnel.Region)
	} else {
		indentPrintf(indent, 0, "• %s [%s]\n", colorTunnelName(tunnel), colorStatus(tunnel.Status))
	}

	renderTunnelDetailsBody(tunnel, indent, 1, showRegion, false)
}

// renderTunnelDetailsBody renders the body of tunnel details (without the header).
// The levels parameter specifies how many indentation levels to add beyond the base indent.
// The showStatus parameter controls whether to display the tunnel status field.
// The showSecretHash parameter controls whether to display the pre-shared key hash.
// This function is called by both renderTunnelDetails and renderTunnelText to avoid code duplication.
func renderTunnelDetailsBody(tunnel *gcp.VPNTunnelInfo, indent string, levels int, showStatus bool, showSecretHash bool) {
	if tunnel == nil {
		return
	}

	if tunnel.PeerIP != "" {
		indentPrintf(indent, levels, "IPSec Peer:  %s\n", formatIPSecPeers(tunnel))
	}

	if tunnel.PeerGateway != "" {
		indentPrintf(indent, levels, "Peer Gateway: %s\n", resourceName(tunnel.PeerGateway))
	}

	if tunnel.RouterName != "" {
		indentPrintf(indent, levels, "Router:       %s\n", tunnel.RouterName)
	}

	if showStatus && tunnel.Status != "" {
		indentPrintf(indent, levels, "Status:       %s\n", colorStatus(tunnel.Status))
	}

	if tunnel.DetailedStatus != "" {
		indentPrintf(indent, levels, "Detail:       %s\n", tunnel.DetailedStatus)
	}

	if tunnel.IkeVersion != 0 {
		indentPrintf(indent, levels, "IKE Version:  %d\n", tunnel.IkeVersion)
	}

	if showSecretHash && tunnel.SharedSecretHash != "" {
		indentPrintf(indent, levels, "Secret Hash:  %s\n", tunnel.SharedSecretHash)
	}

	renderBGPPeers(tunnel.BgpSessions, indent+strings.Repeat("  ", levels))
}
