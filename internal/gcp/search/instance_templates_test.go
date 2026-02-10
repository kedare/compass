package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestInstanceTemplateProviderReturnsMatches(t *testing.T) {
	client := &fakeInstanceTemplateClient{templates: []*gcp.InstanceTemplate{
		{
			Name:        "prod-template",
			Description: "Production template",
			MachineType: "e2-standard-4",
			Tags:        []string{"http-server", "https-server"},
		},
		{
			Name:        "dev-template",
			Description: "Development template",
			MachineType: "e2-medium",
		},
	}}

	provider := &InstanceTemplateProvider{NewClient: func(ctx context.Context, project string) (InstanceTemplateClient, error) {
		if project != "proj-a" {
			t.Fatalf("unexpected project %s", project)
		}
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "prod"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "prod-template" {
		t.Fatalf("expected prod-template, got %s", results[0].Name)
	}

	if results[0].Type != KindInstanceTemplate {
		t.Fatalf("expected type %s, got %s", KindInstanceTemplate, results[0].Type)
	}

	if results[0].Location != "global" {
		t.Fatalf("expected location global, got %s", results[0].Location)
	}

	if results[0].Details["machineType"] != "e2-standard-4" {
		t.Fatalf("expected machineType e2-standard-4, got %s", results[0].Details["machineType"])
	}
}

func TestInstanceTemplateProviderMatchesByDetailFields(t *testing.T) {
	client := &fakeInstanceTemplateClient{templates: []*gcp.InstanceTemplate{
		{Name: "tmpl-a", Description: "Web server template", MachineType: "e2-standard-4"},
		{Name: "tmpl-b", Description: "Database template", MachineType: "n2-highmem-16"},
	}}

	provider := &InstanceTemplateProvider{NewClient: func(ctx context.Context, project string) (InstanceTemplateClient, error) {
		return client, nil
	}}

	// Search by description
	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "Database"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "tmpl-b" {
		t.Fatalf("expected 1 result for description search, got %d", len(results))
	}

	// Search by machine type
	results, err = provider.Search(context.Background(), "proj-a", Query{Term: "e2-standard"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "tmpl-a" {
		t.Fatalf("expected 1 result for machine type search, got %d", len(results))
	}
}

func TestInstanceTemplateProviderPropagatesErrors(t *testing.T) {
	provider := &InstanceTemplateProvider{NewClient: func(context.Context, string) (InstanceTemplateClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &InstanceTemplateProvider{NewClient: func(context.Context, string) (InstanceTemplateClient, error) {
		return &fakeInstanceTemplateClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestInstanceTemplateProviderNilProvider(t *testing.T) {
	var provider *InstanceTemplateProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestInstanceTemplateDetails(t *testing.T) {
	tmpl := &gcp.InstanceTemplate{
		Name:              "test-template",
		Description:       "Test description",
		MachineType:       "n1-standard-1",
		MinCPUPlatform:    "Intel Skylake",
		CanIPForward:      true,
		Preemptible:       true,
		AutomaticRestart:  false,
		OnHostMaintenance: "TERMINATE",
		Tags:              []string{"web", "api"},
		ServiceAccounts:   []string{"sa@project.iam.gserviceaccount.com"},
		MetadataKeys:      []string{"startup-script", "enable-oslogin"},
		Labels:            map[string]string{"env": "prod"},
		Disks: []gcp.InstanceTemplateDisk{
			{DiskType: "pd-ssd", DiskSizeGb: 100, Boot: true, SourceImage: "debian-cloud/debian-11"},
		},
		NetworkInterfaces: []gcp.InstanceTemplateNetwork{
			{Network: "default", Subnetwork: "default-subnet", HasExternalIP: true},
		},
		GPUAccelerators: []gcp.InstanceTemplateGPU{
			{Type: "nvidia-tesla-t4", Count: 1},
		},
	}

	details := instanceTemplateDetails(tmpl)

	if details["description"] != "Test description" {
		t.Errorf("expected description 'Test description', got %q", details["description"])
	}

	if details["machineType"] != "n1-standard-1" {
		t.Errorf("expected machineType 'n1-standard-1', got %q", details["machineType"])
	}

	if details["preemptible"] != "true" {
		t.Errorf("expected preemptible 'true', got %q", details["preemptible"])
	}

	if details["canIpForward"] != "true" {
		t.Errorf("expected canIpForward 'true', got %q", details["canIpForward"])
	}
}

type fakeInstanceTemplateClient struct {
	templates []*gcp.InstanceTemplate
	err       error
}

func (f *fakeInstanceTemplateClient) ListInstanceTemplates(context.Context) ([]*gcp.InstanceTemplate, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.templates, nil
}
