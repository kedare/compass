package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/gcp"
)

// AddressClientFactory creates a Compute Engine client scoped to a project.
type AddressClientFactory func(ctx context.Context, project string) (AddressClient, error)

// AddressClient exposes the subset of gcp.Client used by the address searcher.
type AddressClient interface {
	ListAddresses(ctx context.Context) ([]*gcp.Address, error)
}

// AddressProvider searches IP address reservations for query matches.
type AddressProvider struct {
	NewClient AddressClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *AddressProvider) Kind() ResourceKind {
	return KindAddress
}

// Search implements the Provider interface.
func (p *AddressProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	addresses, err := client.ListAddresses(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list addresses in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(addresses))
	for _, addr := range addresses {
		if addr == nil || !query.MatchesAny(addr.Name, addr.Address, addr.Description) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindAddress,
			Name:     addr.Name,
			Project:  project,
			Location: addr.Region,
			Details:  addressDetails(addr),
		})
	}

	return matches, nil
}

// addressDetails extracts display metadata for an IP address reservation.
func addressDetails(addr *gcp.Address) map[string]string {
	details := make(map[string]string)

	if addr.Address != "" {
		details["address"] = addr.Address
	}

	if addr.AddressType != "" {
		details["type"] = addr.AddressType
	}

	if addr.Status != "" {
		details["status"] = addr.Status
	}

	if addr.Description != "" {
		details["description"] = addr.Description
	}

	if addr.Subnetwork != "" {
		details["subnetwork"] = addr.Subnetwork
	}

	if len(addr.Users) > 0 {
		details["users"] = strings.Join(addr.Users, ", ")
	}

	return details
}
