package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/gcp"
)

// VPNTunnelClient exposes the subset of gcp.Client used by the VPN tunnel searcher.
type VPNTunnelClient interface {
	ListVPNTunnels(ctx context.Context) ([]*gcp.VPNTunnelInfo, error)
}

// VPNTunnelProvider searches VPN tunnels for query matches.
type VPNTunnelProvider struct {
	NewClient func(ctx context.Context, project string) (VPNTunnelClient, error)
}

// Kind returns the resource kind this provider handles.
func (p *VPNTunnelProvider) Kind() ResourceKind {
	return KindVPNTunnel
}

// Search implements the Provider interface.
func (p *VPNTunnelProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	tunnels, err := client.ListVPNTunnels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list VPN tunnels in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if tunnel == nil || !query.MatchesAny(tunnel.Name, tunnel.PeerIP, tunnel.LocalGatewayIP) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindVPNTunnel,
			Name:     tunnel.Name,
			Project:  project,
			Location: tunnel.Region,
			Details:  vpnTunnelDetails(tunnel),
		})
	}

	return matches, nil
}

// vpnTunnelDetails extracts display metadata for a VPN tunnel.
func vpnTunnelDetails(tunnel *gcp.VPNTunnelInfo) map[string]string {
	details := make(map[string]string)

	if tunnel.Status != "" {
		details["status"] = tunnel.Status
	}

	if tunnel.PeerIP != "" {
		details["peerIP"] = tunnel.PeerIP
	}

	if tunnel.LocalGatewayIP != "" {
		details["localIP"] = tunnel.LocalGatewayIP
	}

	if tunnel.IkeVersion > 0 {
		details["ikeVersion"] = fmt.Sprintf("%d", tunnel.IkeVersion)
	}

	// Extract gateway name from full URL
	if tunnel.GatewayLink != "" {
		parts := strings.Split(tunnel.GatewayLink, "/")
		details["gateway"] = parts[len(parts)-1]
		details["isHA"] = "true" // HA VPN tunnel
	} else if tunnel.TargetGatewayLink != "" {
		details["isHA"] = "false" // Classic VPN tunnel
	}

	return details
}
