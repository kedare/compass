package gcp

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/kedare/compass/internal/logger"
	"google.golang.org/api/compute/v1"
)

// IPAssociationKind describes the type of GCP resource that matched an IP lookup.
type IPAssociationKind string

const (
	// IPAssociationInstanceInternal indicates the IP belongs to the internal interface of a Compute Engine VM.
	IPAssociationInstanceInternal IPAssociationKind = "instance_internal"
	// IPAssociationInstanceExternal indicates the IP belongs to the external interface of a Compute Engine VM.
	IPAssociationInstanceExternal IPAssociationKind = "instance_external"
	// IPAssociationForwardingRule indicates the IP belongs to a forwarding rule (typically a load balancer).
	IPAssociationForwardingRule IPAssociationKind = "forwarding_rule"
	// IPAssociationAddress indicates the IP is reserved as a standalone address resource.
	IPAssociationAddress IPAssociationKind = "address"
)

// IPAssociation captures metadata about a resource that owns or references an IP address.
type IPAssociation struct {
	Project      string            `json:"project"`
	Kind         IPAssociationKind `json:"kind"`
	Resource     string            `json:"resource"`
	Location     string            `json:"location"`
	IPAddress    string            `json:"ip_address"`
	Details      string            `json:"details,omitempty"`
	ResourceLink string            `json:"resource_link,omitempty"`
}

// LookupIPAddress searches for resources in the current project that reference the provided IP address.
// It inspects Compute Engine instances, forwarding rules, and address reservations. The IP must be a
// valid IPv4 or IPv6 string. Returns all matching resources, sorted by project, kind, and resource name.
func (c *Client) LookupIPAddress(ctx context.Context, ip string) ([]IPAssociation, error) {
	if c == nil || c.service == nil {
		return nil, fmt.Errorf("client is not initialized")
	}

	target := strings.TrimSpace(ip)
	if target == "" {
		return nil, fmt.Errorf("ip address is required")
	}

	targetIP := net.ParseIP(target)
	if targetIP == nil {
		return nil, fmt.Errorf("invalid IP address %q", ip)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	results := make([]IPAssociation, 0)
	seen := make(map[string]struct{})
	canonicalIP := targetIP.String()

	if err := c.collectInstanceIPMatches(ctx, targetIP, canonicalIP, &results, seen); err != nil {
		return nil, fmt.Errorf("failed to inspect instances: %w", err)
	}

	if err := c.collectForwardingRuleMatches(ctx, targetIP, canonicalIP, &results, seen); err != nil {
		return nil, fmt.Errorf("failed to inspect forwarding rules: %w", err)
	}

	if err := c.collectAddressMatches(ctx, targetIP, canonicalIP, &results, seen); err != nil {
		return nil, fmt.Errorf("failed to inspect addresses: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Project == results[j].Project {
			if results[i].Kind == results[j].Kind {
				return results[i].Resource < results[j].Resource
			}

			return results[i].Kind < results[j].Kind
		}

		return results[i].Project < results[j].Project
	})

	return results, nil
}

func (c *Client) collectInstanceIPMatches(ctx context.Context, target net.IP, canonical string, results *[]IPAssociation, seen map[string]struct{}) error {
	call := c.service.Instances.AggregatedList(c.project).Context(ctx)

	return call.Pages(ctx, func(page *compute.InstanceAggregatedList) error {
		for scope, scopedList := range page.Items {
			if scopedList.Instances == nil {
				continue
			}

			location := locationFromScope(scope)
			for _, inst := range scopedList.Instances {
				if inst == nil {
					continue
				}

				if matches := instanceIPMatches(inst, target); len(matches) > 0 {
					for _, match := range matches {
						association := IPAssociation{
							Project:      c.project,
							Kind:         match.kind,
							Resource:     inst.Name,
							Location:     location,
							IPAddress:    canonical,
							Details:      match.details,
							ResourceLink: inst.SelfLink,
						}

						appendAssociation(results, seen, association)
					}
				}
			}
		}

		return nil
	})
}

func (c *Client) collectForwardingRuleMatches(ctx context.Context, target net.IP, canonical string, results *[]IPAssociation, seen map[string]struct{}) error {
	call := c.service.ForwardingRules.AggregatedList(c.project).Context(ctx)

	return call.Pages(ctx, func(page *compute.ForwardingRuleAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.ForwardingRules) == 0 {
				continue
			}

			location := locationFromScope(scope)
			for _, rule := range scopedList.ForwardingRules {
				if rule == nil {
					continue
				}

				if !equalIP(rule.IPAddress, target) {
					continue
				}

				association := IPAssociation{
					Project:      c.project,
					Kind:         IPAssociationForwardingRule,
					Resource:     rule.Name,
					Location:     location,
					IPAddress:    canonical,
					Details:      describeForwardingRule(rule),
					ResourceLink: rule.SelfLink,
				}

				appendAssociation(results, seen, association)
			}
		}

		return nil
	})
}

func (c *Client) collectAddressMatches(ctx context.Context, target net.IP, canonical string, results *[]IPAssociation, seen map[string]struct{}) error {
	call := c.service.Addresses.AggregatedList(c.project).Context(ctx)

	return call.Pages(ctx, func(page *compute.AddressAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.Addresses) == 0 {
				continue
			}

			location := locationFromScope(scope)
			for _, addr := range scopedList.Addresses {
				if addr == nil {
					continue
				}

				if !equalIP(addr.Address, target) {
					continue
				}

				association := IPAssociation{
					Project:      c.project,
					Kind:         IPAssociationAddress,
					Resource:     addr.Name,
					Location:     location,
					IPAddress:    canonical,
					Details:      describeAddress(addr),
					ResourceLink: addr.SelfLink,
				}

				appendAssociation(results, seen, association)
			}
		}

		return nil
	})
}

type ipMatch struct {
	kind    IPAssociationKind
	details string
}

func instanceIPMatches(inst *compute.Instance, target net.IP) []ipMatch {
	if inst == nil {
		return nil
	}

	matches := make([]ipMatch, 0)

	for _, nic := range inst.NetworkInterfaces {
		if nic == nil {
			continue
		}

		if equalIP(nic.NetworkIP, target) || equalIP(nic.Ipv6Address, target) {
			desc := "Internal interface"
			if nic.Name != "" {
				desc = fmt.Sprintf("Internal interface %s", nic.Name)
			}

			matches = append(matches, ipMatch{
				kind:    IPAssociationInstanceInternal,
				details: desc,
			})
		}

		for _, cfg := range nic.AccessConfigs {
			if cfg == nil {
				continue
			}

			if equalIP(cfg.NatIP, target) {
				desc := "External interface"
				if cfg.Name != "" {
					desc = fmt.Sprintf("External access config %s", cfg.Name)
				}

				matches = append(matches, ipMatch{
					kind:    IPAssociationInstanceExternal,
					details: desc,
				})
			}
		}
	}

	return matches
}

func describeForwardingRule(rule *compute.ForwardingRule) string {
	if rule == nil {
		return ""
	}

	parts := make([]string, 0, 3)
	if scheme := strings.ToLower(strings.TrimSpace(rule.LoadBalancingScheme)); scheme != "" {
		parts = append(parts, fmt.Sprintf("scheme=%s", scheme))
	}

	if rule.PortRange != "" {
		parts = append(parts, fmt.Sprintf("ports=%s", rule.PortRange))
	} else if len(rule.Ports) > 0 {
		parts = append(parts, fmt.Sprintf("ports=%s", strings.Join(rule.Ports, ",")))
	}

	if rule.Target != "" {
		parts = append(parts, fmt.Sprintf("target=%s", lastComponent(rule.Target)))
	} else if rule.BackendService != "" {
		parts = append(parts, fmt.Sprintf("backend=%s", lastComponent(rule.BackendService)))
	}

	if len(parts) == 0 {
		return "Forwarding rule"
	}

	return strings.Join(parts, ", ")
}

func describeAddress(addr *compute.Address) string {
	if addr == nil {
		return ""
	}

	parts := make([]string, 0, 3)
	if addr.Status != "" {
		parts = append(parts, fmt.Sprintf("status=%s", strings.ToLower(addr.Status)))
	}

	if addr.Purpose != "" {
		parts = append(parts, fmt.Sprintf("purpose=%s", strings.ToLower(addr.Purpose)))
	}

	if addr.NetworkTier != "" {
		parts = append(parts, fmt.Sprintf("tier=%s", strings.ToLower(addr.NetworkTier)))
	}

	if addr.AddressType != "" {
		parts = append(parts, fmt.Sprintf("type=%s", strings.ToLower(addr.AddressType)))
	}

	if len(parts) == 0 {
		return "Reserved address"
	}

	return strings.Join(parts, ", ")
}

func locationFromScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}

	switch {
	case strings.HasPrefix(scope, "zones/"):
		return extractZoneFromScope(scope)
	case strings.HasPrefix(scope, "regions/"):
		return extractRegionFromScope(scope)
	default:
		if idx := strings.LastIndex(scope, "/"); idx >= 0 && idx+1 < len(scope) {
			return scope[idx+1:]
		}
	}

	return scope
}

func lastComponent(resourceURL string) string {
	resourceURL = strings.TrimSpace(resourceURL)
	if resourceURL == "" {
		return ""
	}

	if idx := strings.LastIndex(resourceURL, "/"); idx >= 0 && idx+1 < len(resourceURL) {
		return resourceURL[idx+1:]
	}

	return resourceURL
}

func equalIP(value string, target net.IP) bool {
	if target == nil {
		return false
	}

	parsed := net.ParseIP(strings.TrimSpace(value))
	if parsed == nil {
		return false
	}

	return parsed.Equal(target)
}

func appendAssociation(results *[]IPAssociation, seen map[string]struct{}, association IPAssociation) {
	key := fmt.Sprintf("%s|%s|%s|%s", association.Project, association.Kind, association.Resource, association.Location)
	if _, exists := seen[key]; exists {
		return
	}

	seen[key] = struct{}{}
	*results = append(*results, association)

	logger.Log.Tracef("Matched IP %s with %s %s in project %s (%s)",
		association.IPAddress, association.Kind, association.Resource, association.Project, association.Location)
}
