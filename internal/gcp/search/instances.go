package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// InstanceClientFactory creates a Compute Engine client scoped to a project.
type InstanceClientFactory func(ctx context.Context, project string) (InstanceClient, error)

// InstanceClient exposes the subset of gcp.Client used by the searcher.
type InstanceClient interface {
	ListInstances(ctx context.Context, zone string) ([]*gcp.Instance, error)
}

// InstanceProvider searches Compute Engine instances for query matches.
type InstanceProvider struct {
	NewClient InstanceClientFactory
}

// Search implements the Provider interface.
func (p *InstanceProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	instances, err := client.ListInstances(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list instances in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || !query.Matches(inst.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindComputeInstance,
			Name:     inst.Name,
			Project:  inst.Project,
			Location: inst.Zone,
			Details:  instanceDetails(inst),
		})
	}

	return matches, nil
}

// instanceDetails extracts display metadata for a Compute Engine instance.
func instanceDetails(inst *gcp.Instance) map[string]string {
	details := map[string]string{
		"status": inst.Status,
	}

	if inst.InternalIP != "" {
		details["internalIP"] = inst.InternalIP
	}

	if inst.ExternalIP != "" {
		details["externalIP"] = inst.ExternalIP
	}

	if inst.MachineType != "" {
		details["machineType"] = inst.MachineType
	}

	return details
}
