package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// TargetPoolClientFactory creates a Compute Engine client scoped to a project.
type TargetPoolClientFactory func(ctx context.Context, project string) (TargetPoolClient, error)

// TargetPoolClient exposes the subset of gcp.Client used by the target pool searcher.
type TargetPoolClient interface {
	ListTargetPools(ctx context.Context) ([]*gcp.TargetPool, error)
}

// TargetPoolProvider searches target pools for query matches.
type TargetPoolProvider struct {
	NewClient TargetPoolClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *TargetPoolProvider) Kind() ResourceKind {
	return KindTargetPool
}

// Search implements the Provider interface.
func (p *TargetPoolProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	pools, err := client.ListTargetPools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list target pools in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(pools))
	for _, pool := range pools {
		if pool == nil || !query.Matches(pool.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindTargetPool,
			Name:     pool.Name,
			Project:  project,
			Location: pool.Region,
			Details:  targetPoolDetails(pool),
		})
	}

	return matches, nil
}

// targetPoolDetails extracts display metadata for a target pool.
func targetPoolDetails(pool *gcp.TargetPool) map[string]string {
	details := make(map[string]string)

	if pool.SessionAffinity != "" {
		details["sessionAffinity"] = pool.SessionAffinity
	}

	if pool.InstanceCount > 0 {
		details["instances"] = fmt.Sprintf("%d", pool.InstanceCount)
	}

	return details
}
