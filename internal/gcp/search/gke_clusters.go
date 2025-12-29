package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// GKEClusterClientFactory creates a GKE client scoped to a project.
type GKEClusterClientFactory func(ctx context.Context, project string) (GKEClusterClient, error)

// GKEClusterClient exposes the subset of gcp.Client used by the GKE cluster searcher.
type GKEClusterClient interface {
	ListGKEClusters(ctx context.Context) ([]*gcp.GKECluster, error)
}

// GKEClusterProvider searches GKE clusters for query matches.
type GKEClusterProvider struct {
	NewClient GKEClusterClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *GKEClusterProvider) Kind() ResourceKind {
	return KindGKECluster
}

// Search implements the Provider interface.
func (p *GKEClusterProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	clusters, err := client.ListGKEClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list GKE clusters in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster == nil || !query.Matches(cluster.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindGKECluster,
			Name:     cluster.Name,
			Project:  project,
			Location: cluster.Location,
			Details:  gkeClusterDetails(cluster),
		})
	}

	return matches, nil
}

// gkeClusterDetails extracts display metadata for a GKE cluster.
func gkeClusterDetails(cluster *gcp.GKECluster) map[string]string {
	details := make(map[string]string)

	if cluster.Status != "" {
		details["status"] = cluster.Status
	}

	if cluster.CurrentMasterVersion != "" {
		details["version"] = cluster.CurrentMasterVersion
	}

	if cluster.NodeCount > 0 {
		details["nodes"] = fmt.Sprintf("%d", cluster.NodeCount)
	}

	return details
}
