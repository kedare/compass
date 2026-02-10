package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestAddressProviderReturnsMatches(t *testing.T) {
	client := &fakeAddressClient{addresses: []*gcp.Address{
		{Name: "prod-static-ip", Region: "us-central1", Address: "35.1.2.3", AddressType: "EXTERNAL", Status: "IN_USE"},
		{Name: "dev-ip", Region: "europe-west1", Address: "34.5.6.7", AddressType: "INTERNAL", Status: "RESERVED"},
	}}

	provider := &AddressProvider{NewClient: func(ctx context.Context, project string) (AddressClient, error) {
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

	if results[0].Name != "prod-static-ip" {
		t.Fatalf("expected prod-static-ip, got %s", results[0].Name)
	}

	if results[0].Type != KindAddress {
		t.Fatalf("expected type %s, got %s", KindAddress, results[0].Type)
	}

	if results[0].Details["address"] != "35.1.2.3" {
		t.Fatalf("expected address 35.1.2.3, got %s", results[0].Details["address"])
	}

	if results[0].Details["type"] != "EXTERNAL" {
		t.Fatalf("expected type EXTERNAL, got %s", results[0].Details["type"])
	}
}

func TestAddressProviderPropagatesErrors(t *testing.T) {
	provider := &AddressProvider{NewClient: func(context.Context, string) (AddressClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &AddressProvider{NewClient: func(context.Context, string) (AddressClient, error) {
		return &fakeAddressClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestAddressProviderMatchesByDescription(t *testing.T) {
	client := &fakeAddressClient{addresses: []*gcp.Address{
		{Name: "lb-frontend-ip", Address: "35.1.2.3", Region: "us-central1", AddressType: "EXTERNAL", Status: "IN_USE", Description: "frontend load balancer VIP"},
		{Name: "nat-ip", Address: "34.5.6.7", Region: "us-central1", AddressType: "EXTERNAL", Status: "IN_USE"},
	}}

	provider := &AddressProvider{NewClient: func(ctx context.Context, project string) (AddressClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "load balancer"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "lb-frontend-ip" {
		t.Fatalf("expected 1 result for description search, got %d", len(results))
	}
}

func TestAddressProviderMatchesByIP(t *testing.T) {
	client := &fakeAddressClient{addresses: []*gcp.Address{
		{Name: "web-ip", Address: "35.1.2.3", Region: "us-central1", AddressType: "EXTERNAL", Status: "IN_USE"},
		{Name: "db-ip", Address: "10.0.0.5", Region: "us-central1", AddressType: "INTERNAL", Status: "IN_USE"},
	}}

	provider := &AddressProvider{NewClient: func(ctx context.Context, project string) (AddressClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "10.0.0"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "db-ip" {
		t.Fatalf("expected 1 result for IP search, got %d", len(results))
	}
}

func TestAddressDetails(t *testing.T) {
	addr := &gcp.Address{
		Name:        "my-ip",
		Address:     "35.1.2.3",
		AddressType: "EXTERNAL",
		Status:      "IN_USE",
		Description: "web frontend IP",
		Subnetwork:  "default",
		Users:       []string{"forwarding-rule-1", "forwarding-rule-2"},
	}

	details := addressDetails(addr)
	if details["address"] != "35.1.2.3" {
		t.Errorf("expected address 35.1.2.3, got %q", details["address"])
	}
	if details["description"] != "web frontend IP" {
		t.Errorf("expected description 'web frontend IP', got %q", details["description"])
	}
	if details["subnetwork"] != "default" {
		t.Errorf("expected subnetwork 'default', got %q", details["subnetwork"])
	}
	if details["users"] != "forwarding-rule-1, forwarding-rule-2" {
		t.Errorf("expected users list, got %q", details["users"])
	}
}

func TestAddressDetailsEmpty(t *testing.T) {
	addr := &gcp.Address{Name: "minimal-ip"}
	details := addressDetails(addr)
	if _, ok := details["description"]; ok {
		t.Error("expected no description key for empty description")
	}
	if _, ok := details["subnetwork"]; ok {
		t.Error("expected no subnetwork key for empty subnetwork")
	}
	if _, ok := details["users"]; ok {
		t.Error("expected no users key for empty users")
	}
}

func TestAddressProviderNilProvider(t *testing.T) {
	var provider *AddressProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeAddressClient struct {
	addresses []*gcp.Address
	err       error
}

func (f *fakeAddressClient) ListAddresses(context.Context) ([]*gcp.Address, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.addresses, nil
}
