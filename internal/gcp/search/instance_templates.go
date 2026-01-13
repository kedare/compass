package search

import (
	"context"
	"fmt"
	"strings"

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

	if tmpl.Description != "" {
		details["description"] = tmpl.Description
	}
	if tmpl.MachineType != "" {
		details["machineType"] = tmpl.MachineType
	}
	if tmpl.MinCPUPlatform != "" {
		details["minCpuPlatform"] = tmpl.MinCPUPlatform
	}
	details["canIpForward"] = fmt.Sprintf("%v", tmpl.CanIPForward)

	// Disks
	if len(tmpl.Disks) > 0 {
		var diskInfo []string
		for i, disk := range tmpl.Disks {
			info := fmt.Sprintf("#%d: %s", i+1, disk.DiskType)
			if disk.DiskSizeGb > 0 {
				info += fmt.Sprintf(" (%dGB)", disk.DiskSizeGb)
			}
			if disk.Boot {
				info += " [boot]"
			}
			diskInfo = append(diskInfo, info)
		}
		details["disks"] = strings.Join(diskInfo, ", ")
	}

	// Boot disk image
	for _, disk := range tmpl.Disks {
		if disk.Boot && disk.SourceImage != "" {
			details["sourceImage"] = disk.SourceImage
			break
		}
	}

	// Network interfaces
	if len(tmpl.NetworkInterfaces) > 0 {
		var netInfo []string
		for _, nic := range tmpl.NetworkInterfaces {
			info := nic.Network
			if nic.Subnetwork != "" {
				info += "/" + nic.Subnetwork
			}
			if nic.HasExternalIP {
				info += " (external IP)"
			}
			netInfo = append(netInfo, info)
		}
		details["networks"] = strings.Join(netInfo, ", ")
	}

	// Service accounts
	if len(tmpl.ServiceAccounts) > 0 {
		details["serviceAccounts"] = strings.Join(tmpl.ServiceAccounts, ", ")
	}

	// Tags
	if len(tmpl.Tags) > 0 {
		details["tags"] = strings.Join(tmpl.Tags, ", ")
	}

	// Labels
	if len(tmpl.Labels) > 0 {
		var labelInfo []string
		for k, v := range tmpl.Labels {
			labelInfo = append(labelInfo, fmt.Sprintf("%s=%s", k, v))
		}
		details["labels"] = strings.Join(labelInfo, ", ")
	}

	// Scheduling
	details["preemptible"] = fmt.Sprintf("%v", tmpl.Preemptible)
	details["automaticRestart"] = fmt.Sprintf("%v", tmpl.AutomaticRestart)
	if tmpl.OnHostMaintenance != "" {
		details["onHostMaintenance"] = tmpl.OnHostMaintenance
	}

	// GPU accelerators
	if len(tmpl.GPUAccelerators) > 0 {
		var gpuInfo []string
		for _, gpu := range tmpl.GPUAccelerators {
			gpuInfo = append(gpuInfo, fmt.Sprintf("%s x%d", gpu.Type, gpu.Count))
		}
		details["gpuAccelerators"] = strings.Join(gpuInfo, ", ")
	}

	// Metadata keys
	if len(tmpl.MetadataKeys) > 0 {
		details["metadataKeys"] = strings.Join(tmpl.MetadataKeys, ", ")
	}

	return details
}
