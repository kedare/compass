package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// CloudSQLClientFactory creates a Cloud SQL client scoped to a project.
type CloudSQLClientFactory func(ctx context.Context, project string) (CloudSQLClient, error)

// CloudSQLClient exposes the subset of gcp.Client used by the Cloud SQL searcher.
type CloudSQLClient interface {
	ListCloudSQLInstances(ctx context.Context) ([]*gcp.CloudSQLInstance, error)
}

// CloudSQLProvider searches Cloud SQL instances for query matches.
type CloudSQLProvider struct {
	NewClient CloudSQLClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *CloudSQLProvider) Kind() ResourceKind {
	return KindCloudSQLInstance
}

// Search implements the Provider interface.
func (p *CloudSQLProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	instances, err := client.ListCloudSQLInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Cloud SQL instances in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || !query.MatchesAny(inst.Name, inst.DatabaseVersion, inst.Tier) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindCloudSQLInstance,
			Name:     inst.Name,
			Project:  project,
			Location: inst.Region,
			Details:  cloudSQLDetails(inst),
		})
	}

	return matches, nil
}

// cloudSQLDetails extracts display metadata for a Cloud SQL instance.
func cloudSQLDetails(inst *gcp.CloudSQLInstance) map[string]string {
	details := make(map[string]string)

	if inst.DatabaseVersion != "" {
		details["version"] = inst.DatabaseVersion
	}

	if inst.Tier != "" {
		details["tier"] = inst.Tier
	}

	if inst.State != "" {
		details["state"] = inst.State
	}

	return details
}
