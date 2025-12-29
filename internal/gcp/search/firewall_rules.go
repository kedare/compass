package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// FirewallRuleClientFactory creates a Compute Engine client scoped to a project.
type FirewallRuleClientFactory func(ctx context.Context, project string) (FirewallRuleClient, error)

// FirewallRuleClient exposes the subset of gcp.Client used by the firewall rule searcher.
type FirewallRuleClient interface {
	ListFirewallRules(ctx context.Context) ([]*gcp.FirewallRule, error)
}

// FirewallRuleProvider searches firewall rules for query matches.
type FirewallRuleProvider struct {
	NewClient FirewallRuleClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *FirewallRuleProvider) Kind() ResourceKind {
	return KindFirewallRule
}

// Search implements the Provider interface.
func (p *FirewallRuleProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	rules, err := client.ListFirewallRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list firewall rules in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(rules))
	for _, rule := range rules {
		if rule == nil || !query.Matches(rule.Name) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindFirewallRule,
			Name:     rule.Name,
			Project:  project,
			Location: "global",
			Details:  firewallRuleDetails(rule),
		})
	}

	return matches, nil
}

// firewallRuleDetails extracts display metadata for a firewall rule.
func firewallRuleDetails(rule *gcp.FirewallRule) map[string]string {
	details := make(map[string]string)

	if rule.Direction != "" {
		details["direction"] = rule.Direction
	}

	if rule.Network != "" {
		details["network"] = rule.Network
	}

	if rule.Priority > 0 {
		details["priority"] = fmt.Sprintf("%d", rule.Priority)
	}

	if rule.Disabled {
		details["disabled"] = "true"
	}

	return details
}
