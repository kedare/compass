package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// CloudRunClientFactory creates a Cloud Run client scoped to a project.
type CloudRunClientFactory func(ctx context.Context, project string) (CloudRunClient, error)

// CloudRunClient exposes the subset of gcp.Client used by the Cloud Run searcher.
type CloudRunClient interface {
	ListCloudRunServices(ctx context.Context) ([]*gcp.CloudRunService, error)
}

// CloudRunProvider searches Cloud Run services for query matches.
type CloudRunProvider struct {
	NewClient CloudRunClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *CloudRunProvider) Kind() ResourceKind {
	return KindCloudRunService
}

// Search implements the Provider interface.
func (p *CloudRunProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	services, err := client.ListCloudRunServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Cloud Run services in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(services))
	for _, svc := range services {
		if svc == nil || !query.Matches(svc.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindCloudRunService,
			Name:     svc.Name,
			Project:  project,
			Location: svc.Region,
			Details:  cloudRunDetails(svc),
		})
	}

	return matches, nil
}

// cloudRunDetails extracts display metadata for a Cloud Run service.
func cloudRunDetails(svc *gcp.CloudRunService) map[string]string {
	details := make(map[string]string)

	if svc.URL != "" {
		details["url"] = svc.URL
	}

	if svc.LatestRevision != "" {
		details["revision"] = svc.LatestRevision
	}

	return details
}
