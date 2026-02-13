package gcp

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/kedare/compass/internal/cache"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/compute/v1"
)

// SubnetHint provides cached subnet information to optimize IP lookup
type SubnetHint struct {
	Region  string
	Network string
	Subnet  string
}

// LookupIPAddressFast performs an optimized IP lookup using cached subnet information
// to narrow the search scope and reduce API calls. Uses StrategySmartFallback by default:
// tries cache-optimized search first, falls back to full scan if no results found.
func (c *Client) LookupIPAddressFast(ctx context.Context, ip string) ([]IPAssociation, error) {
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

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Use smart fallback strategy
	return c.LookupIPAddressWithStrategy(ctx, ip, StrategySmartFallback)
}

// getSubnetHintsFromCache retrieves cached subnet information for the given IP
func (c *Client) getSubnetHintsFromCache(targetIP net.IP) []SubnetHint {
	if c.cache == nil {
		return nil
	}

	entries := c.cache.FindSubnetsForIP(targetIP)
	if len(entries) == 0 {
		return nil
	}

	// Filter to only subnets in the current project
	hints := make([]SubnetHint, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.Project != c.project {
			continue
		}

		hints = append(hints, SubnetHint{
			Region:  entry.Region,
			Network: entry.Network,
			Subnet:  entry.Name,
		})
	}

	return hints
}

// lookupWithHints performs a targeted lookup using subnet hints
func (c *Client) lookupWithHints(ctx context.Context, target net.IP, canonical string, hints []SubnetHint) ([]IPAssociation, error) {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(3)

	var (
		instanceResults   []IPAssociation
		forwardingResults []IPAssociation
		addressResults    []IPAssociation
		subnetResults     []IPAssociation
	)

	// Extract unique regions from hints
	regions := make(map[string]struct{})
	networks := make(map[string]struct{})
	for _, hint := range hints {
		regions[hint.Region] = struct{}{}
		networks[hint.Network] = struct{}{}
	}

	// Search instances in target regions only
	group.Go(func() error {
		results, err := c.collectInstanceIPMatchesTargeted(groupCtx, target, canonical, regions, networks)
		if err != nil {
			return fmt.Errorf("failed to inspect instances: %w", err)
		}
		instanceResults = results
		return nil
	})

	// Search forwarding rules in target regions only
	group.Go(func() error {
		results, err := c.collectForwardingRuleMatchesTargeted(groupCtx, target, canonical, regions)
		if err != nil {
			return fmt.Errorf("failed to inspect forwarding rules: %w", err)
		}
		forwardingResults = results
		return nil
	})

	// Search addresses in target regions only
	group.Go(func() error {
		results, err := c.collectAddressMatchesTargeted(groupCtx, target, canonical, regions)
		if err != nil {
			return fmt.Errorf("failed to inspect addresses: %w", err)
		}
		addressResults = results
		return nil
	})

	// Add subnet matches from hints (already have this info)
	group.Go(func() error {
		results := c.buildSubnetAssociationsFromHints(target, canonical, hints)
		subnetResults = results
		return nil
	})

	if err := group.Wait(); err != nil {
		return nil, err
	}

	// Combine and deduplicate results
	results := make([]IPAssociation, 0, len(instanceResults)+len(forwardingResults)+len(addressResults)+len(subnetResults))
	seen := make(map[string]struct{})

	for _, association := range instanceResults {
		appendAssociation(&results, seen, association)
	}

	for _, association := range forwardingResults {
		appendAssociation(&results, seen, association)
	}

	for _, association := range addressResults {
		appendAssociation(&results, seen, association)
	}

	for _, association := range subnetResults {
		appendAssociation(&results, seen, association)
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

// collectInstanceIPMatchesTargeted searches instances only in specified regions
func (c *Client) collectInstanceIPMatchesTargeted(ctx context.Context, target net.IP, canonical string, regions, networks map[string]struct{}) ([]IPAssociation, error) {
	results := make([]IPAssociation, 0)
	seen := make(map[string]struct{})

	// Use aggregated list but filter early
	call := c.service.Instances.AggregatedList(c.project).Context(ctx)

	err := call.Pages(ctx, func(page *compute.InstanceAggregatedList) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		for scope, scopedList := range page.Items {
			if err := ctx.Err(); err != nil {
				return err
			}

			if scopedList.Instances == nil {
				continue
			}

			location := locationFromScope(scope)

			// Skip if not in target regions
			if len(regions) > 0 {
				if _, ok := regions[location]; !ok {
					continue
				}
			}

			for _, inst := range scopedList.Instances {
				if err := ctx.Err(); err != nil {
					return err
				}

				if inst == nil {
					continue
				}

				// Pre-filter by network if we have network hints
				if len(networks) > 0 {
					hasMatchingNetwork := false
					for _, nic := range inst.NetworkInterfaces {
						if nic != nil {
							networkName := lastComponent(nic.Network)
							if _, ok := networks[networkName]; ok {
								hasMatchingNetwork = true
								break
							}
						}
					}
					if !hasMatchingNetwork {
						continue
					}
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
						appendAssociation(&results, seen, association)
					}
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}

// collectForwardingRuleMatchesTargeted searches forwarding rules only in specified regions
func (c *Client) collectForwardingRuleMatchesTargeted(ctx context.Context, target net.IP, canonical string, regions map[string]struct{}) ([]IPAssociation, error) {
	results := make([]IPAssociation, 0)
	seen := make(map[string]struct{})

	call := c.service.ForwardingRules.AggregatedList(c.project).Context(ctx)

	err := call.Pages(ctx, func(page *compute.ForwardingRuleAggregatedList) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		for scope, scopedList := range page.Items {
			if err := ctx.Err(); err != nil {
				return err
			}

			if len(scopedList.ForwardingRules) == 0 {
				continue
			}

			location := locationFromScope(scope)

			// Skip if not in target regions
			if len(regions) > 0 {
				if _, ok := regions[location]; !ok {
					continue
				}
			}

			for _, rule := range scopedList.ForwardingRules {
				if err := ctx.Err(); err != nil {
					return err
				}

				if rule == nil || !equalIP(rule.IPAddress, target) {
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
				appendAssociation(&results, seen, association)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}

// collectAddressMatchesTargeted searches addresses only in specified regions
func (c *Client) collectAddressMatchesTargeted(ctx context.Context, target net.IP, canonical string, regions map[string]struct{}) ([]IPAssociation, error) {
	results := make([]IPAssociation, 0)
	seen := make(map[string]struct{})

	call := c.service.Addresses.AggregatedList(c.project).Context(ctx)

	err := call.Pages(ctx, func(page *compute.AddressAggregatedList) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		for scope, scopedList := range page.Items {
			if err := ctx.Err(); err != nil {
				return err
			}

			if len(scopedList.Addresses) == 0 {
				continue
			}

			location := locationFromScope(scope)

			// Skip if not in target regions
			if len(regions) > 0 {
				if _, ok := regions[location]; !ok {
					continue
				}
			}

			for _, addr := range scopedList.Addresses {
				if err := ctx.Err(); err != nil {
					return err
				}

				if addr == nil || !equalIP(addr.Address, target) {
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
				appendAssociation(&results, seen, association)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}

// buildSubnetAssociationsFromHints creates subnet associations from cached hints
func (c *Client) buildSubnetAssociationsFromHints(target net.IP, canonical string, hints []SubnetHint) []IPAssociation {
	results := make([]IPAssociation, 0, len(hints))
	seen := make(map[string]struct{})

	for _, hint := range hints {
		// Get full subnet details from cache if available
		if c.cache != nil {
			if entry := c.getSubnetEntryFromCache(hint); entry != nil {
				matched, detail := c.subnetMatchDetailsFromEntry(entry, target)
				if matched {
					association := IPAssociation{
						Project:      c.project,
						Kind:         IPAssociationSubnet,
						Resource:     entry.Name,
						Location:     entry.Region,
						IPAddress:    canonical,
						Details:      detail,
						ResourceLink: entry.SelfLink,
					}
					appendAssociation(&results, seen, association)
				}
			}
		}
	}

	return results
}

// getSubnetEntryFromCache retrieves a specific subnet entry from cache
func (c *Client) getSubnetEntryFromCache(hint SubnetHint) *cache.SubnetEntry {
	if c.cache == nil {
		return nil
	}

	// Search through cached subnets for the IP again, but filter by hint
	// This is still fast because it's using the cache
	entries := c.cache.FindSubnetsForIP(net.ParseIP("0.0.0.0")) // Placeholder - we need all subnets
	for _, entry := range entries {
		if entry != nil &&
			entry.Project == c.project &&
			entry.Region == hint.Region &&
			entry.Network == hint.Network &&
			entry.Name == hint.Subnet {
			return entry
		}
	}

	return nil
}

// subnetMatchDetailsFromEntry checks if IP matches subnet entry and returns details
func (c *Client) subnetMatchDetailsFromEntry(entry *cache.SubnetEntry, target net.IP) (bool, string) {
	if entry == nil || target == nil {
		return false, ""
	}

	var (
		matchedCIDR string
		source      string
	)

	// Check primary CIDR
	if entry.PrimaryCIDR != "" && ipInCIDR(target, entry.PrimaryCIDR) {
		matchedCIDR = entry.PrimaryCIDR
		source = "primary"
	}

	// Check secondary ranges
	if matchedCIDR == "" {
		for _, sec := range entry.SecondaryRanges {
			if sec.CIDR != "" && ipInCIDR(target, sec.CIDR) {
				matchedCIDR = sec.CIDR
				if sec.Name != "" {
					source = fmt.Sprintf("secondary:%s", sec.Name)
				} else {
					source = "secondary"
				}
				break
			}
		}
	}

	// Check IPv6
	if matchedCIDR == "" && entry.IPv6CIDR != "" && ipInCIDR(target, entry.IPv6CIDR) {
		matchedCIDR = entry.IPv6CIDR
		source = "ipv6"
	}

	if matchedCIDR == "" {
		return false, ""
	}

	detailParts := make([]string, 0, 4)
	if entry.Network != "" {
		detailParts = append(detailParts, fmt.Sprintf("network=%s", entry.Network))
	}

	detailParts = append(detailParts, fmt.Sprintf("cidr=%s", matchedCIDR))
	if source != "" {
		detailParts = append(detailParts, fmt.Sprintf("range=%s", source))
	}

	if entry.Gateway != "" {
		detailParts = append(detailParts, fmt.Sprintf("gateway_ip=%s", entry.Gateway))
		if equalIP(entry.Gateway, target) {
			detailParts = append(detailParts, "gateway=true")
		}
	}

	return true, strings.Join(detailParts, ", ")
}
