package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// GKENodePoolClientFactory creates a GKE client scoped to a project.
type GKENodePoolClientFactory func(ctx context.Context, project string) (GKENodePoolClient, error)

// GKENodePoolClient exposes the subset of gcp.Client used by the GKE node pool searcher.
type GKENodePoolClient interface {
	ListGKENodePools(ctx context.Context) ([]*gcp.GKENodePool, error)
}

// GKENodePoolProvider searches GKE node pools for query matches.
type GKENodePoolProvider struct {
	NewClient GKENodePoolClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *GKENodePoolProvider) Kind() ResourceKind {
	return KindGKENodePool
}

// Search implements the Provider interface.
func (p *GKENodePoolProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	pools, err := client.ListGKENodePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list GKE node pools in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(pools))
	for _, pool := range pools {
		if pool == nil || !query.MatchesAny(pool.Name, pool.ClusterName, pool.MachineType) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindGKENodePool,
			Name:     pool.Name,
			Project:  project,
			Location: pool.Location,
			Details:  gkeNodePoolDetails(pool),
		})
	}

	return matches, nil
}

// gkeNodePoolDetails extracts display metadata for a GKE node pool.
func gkeNodePoolDetails(pool *gcp.GKENodePool) map[string]string {
	details := make(map[string]string)

	if pool.ClusterName != "" {
		details["cluster"] = pool.ClusterName
	}

	if pool.MachineType != "" {
		details["machineType"] = pool.MachineType
	}

	if pool.NodeCount > 0 {
		details["nodes"] = fmt.Sprintf("%d", pool.NodeCount)
	}

	if pool.Status != "" {
		details["status"] = pool.Status
	}

	return details
}
