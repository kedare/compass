package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// HealthCheckClientFactory creates a Compute Engine client scoped to a project.
type HealthCheckClientFactory func(ctx context.Context, project string) (HealthCheckClient, error)

// HealthCheckClient exposes the subset of gcp.Client used by the health check searcher.
type HealthCheckClient interface {
	ListHealthChecks(ctx context.Context) ([]*gcp.HealthCheck, error)
}

// HealthCheckProvider searches health checks for query matches.
type HealthCheckProvider struct {
	NewClient HealthCheckClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *HealthCheckProvider) Kind() ResourceKind {
	return KindHealthCheck
}

// Search implements the Provider interface.
func (p *HealthCheckProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	checks, err := client.ListHealthChecks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list health checks in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(checks))
	for _, check := range checks {
		if check == nil || !query.Matches(check.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindHealthCheck,
			Name:     check.Name,
			Project:  project,
			Location: "global",
			Details:  healthCheckDetails(check),
		})
	}

	return matches, nil
}

// healthCheckDetails extracts display metadata for a health check.
func healthCheckDetails(check *gcp.HealthCheck) map[string]string {
	details := make(map[string]string)

	if check.Type != "" {
		details["type"] = check.Type
	}

	if check.Port > 0 {
		details["port"] = fmt.Sprintf("%d", check.Port)
	}

	return details
}
