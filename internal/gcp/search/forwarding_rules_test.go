package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestForwardingRuleProviderReturnsMatches(t *testing.T) {
	client := &fakeForwardingRuleClient{rules: []*gcp.ForwardingRule{
		{Name: "prod-lb-frontend", Region: "us-central1", IPAddress: "35.1.2.3", IPProtocol: "TCP", PortRange: "443", LoadBalancingScheme: "EXTERNAL"},
		{Name: "dev-frontend", Region: "europe-west1", IPAddress: "10.0.0.1", IPProtocol: "TCP", PortRange: "80", LoadBalancingScheme: "INTERNAL"},
	}}

	provider := &ForwardingRuleProvider{NewClient: func(ctx context.Context, project string) (ForwardingRuleClient, error) {
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

	if results[0].Name != "prod-lb-frontend" {
		t.Fatalf("expected prod-lb-frontend, got %s", results[0].Name)
	}

	if results[0].Type != KindForwardingRule {
		t.Fatalf("expected type %s, got %s", KindForwardingRule, results[0].Type)
	}

	if results[0].Details["ipAddress"] != "35.1.2.3" {
		t.Fatalf("expected ipAddress 35.1.2.3, got %s", results[0].Details["ipAddress"])
	}

	if results[0].Details["portRange"] != "443" {
		t.Fatalf("expected portRange 443, got %s", results[0].Details["portRange"])
	}
}

func TestForwardingRuleProviderPropagatesErrors(t *testing.T) {
	provider := &ForwardingRuleProvider{NewClient: func(context.Context, string) (ForwardingRuleClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &ForwardingRuleProvider{NewClient: func(context.Context, string) (ForwardingRuleClient, error) {
		return &fakeForwardingRuleClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestForwardingRuleProviderNilProvider(t *testing.T) {
	var provider *ForwardingRuleProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

type fakeForwardingRuleClient struct {
	rules []*gcp.ForwardingRule
	err   error
}

func (f *fakeForwardingRuleClient) ListForwardingRules(context.Context) ([]*gcp.ForwardingRule, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rules, nil
}
