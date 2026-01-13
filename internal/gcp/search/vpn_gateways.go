package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/gcp"
)

// VPNGatewayClient exposes the subset of gcp.Client used by the VPN gateway searcher.
type VPNGatewayClient interface {
	ListVPNGateways(ctx context.Context) ([]*gcp.VPNGatewayInfo, error)
}

// VPNGatewayProvider searches VPN gateways for query matches.
type VPNGatewayProvider struct {
	NewClient func(ctx context.Context, project string) (VPNGatewayClient, error)
}

// Kind returns the resource kind this provider handles.
func (p *VPNGatewayProvider) Kind() ResourceKind {
	return KindVPNGateway
}

// Search implements the Provider interface.
func (p *VPNGatewayProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	gateways, err := client.ListVPNGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list VPN gateways in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(gateways))
	for _, gw := range gateways {
		if gw == nil || !query.Matches(gw.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindVPNGateway,
			Name:     gw.Name,
			Project:  project,
			Location: gw.Region,
			Details:  vpnGatewayDetails(gw),
		})
	}

	return matches, nil
}

// vpnGatewayDetails extracts display metadata for a VPN gateway.
func vpnGatewayDetails(gw *gcp.VPNGatewayInfo) map[string]string {
	details := make(map[string]string)

	if gw.Network != "" {
		// Extract network name from full URL
		parts := strings.Split(gw.Network, "/")
		details["network"] = parts[len(parts)-1]
	}

	if len(gw.Interfaces) > 0 {
		details["interfaces"] = fmt.Sprintf("%d", len(gw.Interfaces))
	}

	if len(gw.Tunnels) > 0 {
		details["tunnels"] = fmt.Sprintf("%d", len(gw.Tunnels))
	}

	return details
}
