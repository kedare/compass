package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// InstanceTemplateClientFactory creates a Compute Engine client scoped to a project.
type InstanceTemplateClientFactory func(ctx context.Context, project string) (InstanceTemplateClient, error)

// InstanceTemplateClient exposes the subset of gcp.Client used by the instance template searcher.
type InstanceTemplateClient interface {
	ListInstanceTemplates(ctx context.Context) ([]*gcp.InstanceTemplate, error)
}

// InstanceTemplateProvider searches Instance Templates for query matches.
type InstanceTemplateProvider struct {
	NewClient InstanceTemplateClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *InstanceTemplateProvider) Kind() ResourceKind {
	return KindInstanceTemplate
}

// Search implements the Provider interface.
func (p *InstanceTemplateProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	templates, err := client.ListInstanceTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list instance templates in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(templates))
	for _, tmpl := range templates {
		if tmpl == nil || !query.Matches(tmpl.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindInstanceTemplate,
			Name:     tmpl.Name,
			Project:  project,
			Location: "global",
			Details:  instanceTemplateDetails(tmpl),
		})
	}

	return matches, nil
}

// instanceTemplateDetails extracts display metadata for an Instance Template.
func instanceTemplateDetails(tmpl *gcp.InstanceTemplate) map[string]string {
	details := make(map[string]string)

	if tmpl.MachineType != "" {
		details["machineType"] = tmpl.MachineType
	}

	return details
}
