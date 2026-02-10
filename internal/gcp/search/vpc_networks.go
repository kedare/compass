package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// VPCNetworkClientFactory creates a Compute Engine client scoped to a project.
type VPCNetworkClientFactory func(ctx context.Context, project string) (VPCNetworkClient, error)

// VPCNetworkClient exposes the subset of gcp.Client used by the VPC network searcher.
type VPCNetworkClient interface {
	ListVPCNetworks(ctx context.Context) ([]*gcp.VPCNetwork, error)
}

// VPCNetworkProvider searches VPC networks for query matches.
type VPCNetworkProvider struct {
	NewClient VPCNetworkClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *VPCNetworkProvider) Kind() ResourceKind {
	return KindVPCNetwork
}

// Search implements the Provider interface.
func (p *VPCNetworkProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	networks, err := client.ListVPCNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list VPC networks in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(networks))
	for _, network := range networks {
		if network == nil || !query.MatchesAny(network.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindVPCNetwork,
			Name:     network.Name,
			Project:  project,
			Location: "global",
			Details:  vpcNetworkDetails(network),
		})
	}

	return matches, nil
}

// vpcNetworkDetails extracts display metadata for a VPC network.
func vpcNetworkDetails(network *gcp.VPCNetwork) map[string]string {
	details := make(map[string]string)

	if network.AutoCreateSubnetworks {
		details["mode"] = "auto"
	} else {
		details["mode"] = "custom"
	}

	if network.SubnetCount > 0 {
		details["subnets"] = fmt.Sprintf("%d", network.SubnetCount)
	}

	return details
}
