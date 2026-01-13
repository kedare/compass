package search

import (
	"context"
	"errors"
	"testing"

	"github.com/kedare/compass/internal/gcp"
)

func TestFirewallRuleProviderReturnsMatches(t *testing.T) {
	client := &fakeFirewallRuleClient{rules: []*gcp.FirewallRule{
		{Name: "prod-allow-http", Direction: "INGRESS", Network: "default", Priority: 1000, Disabled: false},
		{Name: "dev-deny-all", Direction: "EGRESS", Network: "dev-vpc", Priority: 65535, Disabled: true},
	}}

	provider := &FirewallRuleProvider{NewClient: func(ctx context.Context, project string) (FirewallRuleClient, error) {
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

	if results[0].Name != "prod-allow-http" {
		t.Fatalf("expected prod-allow-http, got %s", results[0].Name)
	}

	if results[0].Type != KindFirewallRule {
		t.Fatalf("expected type %s, got %s", KindFirewallRule, results[0].Type)
	}

	if results[0].Location != "global" {
		t.Fatalf("expected location global, got %s", results[0].Location)
	}

	if results[0].Details["direction"] != "INGRESS" {
		t.Fatalf("expected direction INGRESS, got %s", results[0].Details["direction"])
	}

	if results[0].Details["priority"] != "1000" {
		t.Fatalf("expected priority 1000, got %s", results[0].Details["priority"])
	}
}

func TestFirewallRuleProviderPropagatesErrors(t *testing.T) {
	provider := &FirewallRuleProvider{NewClient: func(context.Context, string) (FirewallRuleClient, error) {
		return nil, errors.New("client boom")
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected error")
	}

	provider = &FirewallRuleProvider{NewClient: func(context.Context, string) (FirewallRuleClient, error) {
		return &fakeFirewallRuleClient{err: errors.New("list boom")}, nil
	}}

	if _, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestFirewallRuleProviderNilProvider(t *testing.T) {
	var provider *FirewallRuleProvider
	_, err := provider.Search(context.Background(), "proj-a", Query{Term: "foo"})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestFirewallRuleDetailsDisabled(t *testing.T) {
	rule := &gcp.FirewallRule{
		Name:     "test-rule",
		Disabled: true,
	}

	details := firewallRuleDetails(rule)
	if details["disabled"] != "true" {
		t.Errorf("expected disabled 'true', got %q", details["disabled"])
	}
}

type fakeFirewallRuleClient struct {
	rules []*gcp.FirewallRule
	err   error
}

func (f *fakeFirewallRuleClient) ListFirewallRules(context.Context) ([]*gcp.FirewallRule, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rules, nil
}
