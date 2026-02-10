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

func TestFirewallRuleProviderMatchesByDescription(t *testing.T) {
	client := &fakeFirewallRuleClient{rules: []*gcp.FirewallRule{
		{Name: "allow-internal", Direction: "INGRESS", Network: "default", Priority: 1000, Description: "Allow internal traffic between subnets"},
		{Name: "deny-all", Direction: "INGRESS", Network: "default", Priority: 65535},
	}}

	provider := &FirewallRuleProvider{NewClient: func(ctx context.Context, project string) (FirewallRuleClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "internal traffic"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "allow-internal" {
		t.Fatalf("expected 1 result for description search, got %d", len(results))
	}
}

func TestFirewallRuleProviderMatchesBySourceRange(t *testing.T) {
	client := &fakeFirewallRuleClient{rules: []*gcp.FirewallRule{
		{Name: "allow-office", Direction: "INGRESS", Network: "default", Priority: 1000, SourceRanges: []string{"203.0.113.0/24"}},
		{Name: "allow-vpn", Direction: "INGRESS", Network: "default", Priority: 1000, SourceRanges: []string{"10.8.0.0/16"}},
	}}

	provider := &FirewallRuleProvider{NewClient: func(ctx context.Context, project string) (FirewallRuleClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "203.0.113"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "allow-office" {
		t.Fatalf("expected 1 result for source range search, got %d", len(results))
	}
}

func TestFirewallRuleProviderMatchesByTargetTag(t *testing.T) {
	client := &fakeFirewallRuleClient{rules: []*gcp.FirewallRule{
		{Name: "allow-http-tagged", Direction: "INGRESS", Network: "default", Priority: 1000, TargetTags: []string{"http-server", "https-server"}},
		{Name: "allow-ssh", Direction: "INGRESS", Network: "default", Priority: 1000, TargetTags: []string{"ssh-server"}},
	}}

	provider := &FirewallRuleProvider{NewClient: func(ctx context.Context, project string) (FirewallRuleClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "https-server"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "allow-http-tagged" {
		t.Fatalf("expected 1 result for target tag search, got %d", len(results))
	}
}

func TestFirewallRuleProviderMatchesByAllowed(t *testing.T) {
	client := &fakeFirewallRuleClient{rules: []*gcp.FirewallRule{
		{Name: "allow-http", Direction: "INGRESS", Network: "default", Priority: 1000, Allowed: []string{"tcp:80,443"}},
		{Name: "allow-icmp", Direction: "INGRESS", Network: "default", Priority: 1000, Allowed: []string{"icmp"}},
	}}

	provider := &FirewallRuleProvider{NewClient: func(ctx context.Context, project string) (FirewallRuleClient, error) {
		return client, nil
	}}

	results, err := provider.Search(context.Background(), "proj-a", Query{Term: "tcp:80"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "allow-http" {
		t.Fatalf("expected 1 result for allowed search, got %d", len(results))
	}
}

func TestFirewallRuleDetailsNewFields(t *testing.T) {
	rule := &gcp.FirewallRule{
		Name:         "test-rule",
		Direction:    "INGRESS",
		Network:      "my-vpc",
		Priority:     1000,
		Description:  "Allow HTTP from office",
		SourceRanges: []string{"203.0.113.0/24", "198.51.100.0/24"},
		TargetTags:   []string{"web-server"},
		Allowed:      []string{"tcp:80,443"},
	}

	details := firewallRuleDetails(rule)
	if details["description"] != "Allow HTTP from office" {
		t.Errorf("expected description, got %q", details["description"])
	}
	if details["sourceRanges"] != "203.0.113.0/24, 198.51.100.0/24" {
		t.Errorf("expected source ranges, got %q", details["sourceRanges"])
	}
	if details["targetTags"] != "web-server" {
		t.Errorf("expected target tags, got %q", details["targetTags"])
	}
	if details["allowed"] != "tcp:80,443" {
		t.Errorf("expected allowed, got %q", details["allowed"])
	}
}

func TestFirewallRuleDetailsEmptyNewFields(t *testing.T) {
	rule := &gcp.FirewallRule{Name: "minimal-rule"}
	details := firewallRuleDetails(rule)
	if _, ok := details["description"]; ok {
		t.Error("expected no description key for empty description")
	}
	if _, ok := details["sourceRanges"]; ok {
		t.Error("expected no sourceRanges key for empty source ranges")
	}
	if _, ok := details["targetTags"]; ok {
		t.Error("expected no targetTags key for empty target tags")
	}
	if _, ok := details["allowed"]; ok {
		t.Error("expected no allowed key for empty allowed")
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
