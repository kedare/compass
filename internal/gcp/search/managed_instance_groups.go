package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// MIGClientFactory creates a Compute Engine client scoped to a project.
type MIGClientFactory func(ctx context.Context, project string) (MIGClient, error)

// MIGClient exposes the subset of gcp.Client used by the MIG searcher.
type MIGClient interface {
	ListManagedInstanceGroups(ctx context.Context, location string) ([]gcp.ManagedInstanceGroup, error)
}

// MIGProvider searches Managed Instance Groups for query matches.
type MIGProvider struct {
	NewClient MIGClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *MIGProvider) Kind() ResourceKind {
	return KindManagedInstanceGroup
}

// Search implements the Provider interface.
func (p *MIGProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	migs, err := client.ListManagedInstanceGroups(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list managed instance groups in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(migs))
	for _, mig := range migs {
		if !query.Matches(mig.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindManagedInstanceGroup,
			Name:     mig.Name,
			Project:  project,
			Location: mig.Location,
			Details:  migDetails(&mig),
		})
	}

	return matches, nil
}

// migDetails extracts display metadata for a Managed Instance Group.
func migDetails(mig *gcp.ManagedInstanceGroup) map[string]string {
	details := make(map[string]string)

	if mig.IsRegional {
		details["scope"] = "regional"
	} else {
		details["scope"] = "zonal"
	}

	return details
}
