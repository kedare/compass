package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// BackendServiceClientFactory creates a Compute Engine client scoped to a project.
type BackendServiceClientFactory func(ctx context.Context, project string) (BackendServiceClient, error)

// BackendServiceClient exposes the subset of gcp.Client used by the backend service searcher.
type BackendServiceClient interface {
	ListBackendServices(ctx context.Context) ([]*gcp.BackendService, error)
}

// BackendServiceProvider searches backend services for query matches.
type BackendServiceProvider struct {
	NewClient BackendServiceClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *BackendServiceProvider) Kind() ResourceKind {
	return KindBackendService
}

// Search implements the Provider interface.
func (p *BackendServiceProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	services, err := client.ListBackendServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list backend services in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(services))
	for _, svc := range services {
		if svc == nil || !query.Matches(svc.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindBackendService,
			Name:     svc.Name,
			Project:  project,
			Location: svc.Region,
			Details:  backendServiceDetails(svc),
		})
	}

	return matches, nil
}

// backendServiceDetails extracts display metadata for a backend service.
func backendServiceDetails(svc *gcp.BackendService) map[string]string {
	details := make(map[string]string)

	if svc.Protocol != "" {
		details["protocol"] = svc.Protocol
	}

	if svc.LoadBalancingScheme != "" {
		details["scheme"] = svc.LoadBalancingScheme
	}

	if svc.HealthCheckCount > 0 {
		details["healthChecks"] = fmt.Sprintf("%d", svc.HealthCheckCount)
	}

	if svc.BackendCount > 0 {
		details["backends"] = fmt.Sprintf("%d", svc.BackendCount)
	}

	return details
}
