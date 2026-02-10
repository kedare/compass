package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// SubnetClientFactory creates a Compute Engine client scoped to a project.
type SubnetClientFactory func(ctx context.Context, project string) (SubnetClient, error)

// SubnetClient exposes the subset of gcp.Client used by the subnet searcher.
type SubnetClient interface {
	ListSubnets(ctx context.Context) ([]*gcp.Subnet, error)
}

// SubnetProvider searches subnets for query matches.
type SubnetProvider struct {
	NewClient SubnetClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *SubnetProvider) Kind() ResourceKind {
	return KindSubnet
}

// Search implements the Provider interface.
func (p *SubnetProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	subnets, err := client.ListSubnets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list subnets in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(subnets))
	for _, subnet := range subnets {
		if subnet == nil || !query.MatchesAny(subnet.Name, subnet.IPCidrRange, subnet.Network) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindSubnet,
			Name:     subnet.Name,
			Project:  project,
			Location: subnet.Region,
			Details:  subnetDetails(subnet),
		})
	}

	return matches, nil
}

// subnetDetails extracts display metadata for a subnet.
func subnetDetails(subnet *gcp.Subnet) map[string]string {
	details := make(map[string]string)

	if subnet.IPCidrRange != "" {
		details["cidr"] = subnet.IPCidrRange
	}

	if subnet.Network != "" {
		details["network"] = subnet.Network
	}

	if subnet.Purpose != "" {
		details["purpose"] = subnet.Purpose
	}

	return details
}
