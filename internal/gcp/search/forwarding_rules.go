package search

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// ForwardingRuleClientFactory creates a Compute Engine client scoped to a project.
type ForwardingRuleClientFactory func(ctx context.Context, project string) (ForwardingRuleClient, error)

// ForwardingRuleClient exposes the subset of gcp.Client used by the forwarding rule searcher.
type ForwardingRuleClient interface {
	ListForwardingRules(ctx context.Context) ([]*gcp.ForwardingRule, error)
}

// ForwardingRuleProvider searches forwarding rules for query matches.
type ForwardingRuleProvider struct {
	NewClient ForwardingRuleClientFactory
}

// Kind returns the resource kind this provider handles.
func (p *ForwardingRuleProvider) Kind() ResourceKind {
	return KindForwardingRule
}

// Search implements the Provider interface.
func (p *ForwardingRuleProvider) Search(ctx context.Context, project string, query Query) ([]Result, error) {
	if p == nil || p.NewClient == nil {
		return nil, fmt.Errorf("%s: %w", project, ErrNoProviders)
	}

	client, err := p.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", project, err)
	}

	rules, err := client.ListForwardingRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list forwarding rules in %s: %w", project, err)
	}

	matches := make([]Result, 0, len(rules))
	for _, rule := range rules {
		if rule == nil || !query.MatchesAny(rule.Name, rule.IPAddress, rule.IPProtocol, rule.LoadBalancingScheme) {
			continue
		}

		matches = append(matches, Result{
			Type:     KindForwardingRule,
			Name:     rule.Name,
			Project:  project,
			Location: rule.Region,
			Details:  forwardingRuleDetails(rule),
		})
	}

	return matches, nil
}

// forwardingRuleDetails extracts display metadata for a forwarding rule.
func forwardingRuleDetails(rule *gcp.ForwardingRule) map[string]string {
	details := make(map[string]string)

	if rule.IPAddress != "" {
		details["ipAddress"] = rule.IPAddress
	}

	if rule.IPProtocol != "" {
		details["protocol"] = rule.IPProtocol
	}

	if rule.PortRange != "" {
		details["portRange"] = rule.PortRange
	}

	if rule.LoadBalancingScheme != "" {
		details["scheme"] = rule.LoadBalancingScheme
	}

	return details
}
