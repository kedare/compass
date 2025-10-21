package gcp

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"golang.org/x/sync/errgroup"
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
	// IPAssociationSubnet indicates the IP falls within a subnet CIDR range.
	IPAssociationSubnet IPAssociationKind = "subnet_range"
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

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(3)

	canonicalIP := targetIP.String()

	var (
		instanceResults   []IPAssociation
		forwardingResults []IPAssociation
		addressResults    []IPAssociation
		subnetResults     []IPAssociation
	)

	group.Go(func() error {
		results, err := c.collectInstanceIPMatches(groupCtx, targetIP, canonicalIP)
		if err != nil {
			return fmt.Errorf("failed to inspect instances: %w", err)
		}
		instanceResults = results

		return nil
	})

	group.Go(func() error {
		results, err := c.collectForwardingRuleMatches(groupCtx, targetIP, canonicalIP)
		if err != nil {
			return fmt.Errorf("failed to inspect forwarding rules: %w", err)
		}
		forwardingResults = results

		return nil
	})

	group.Go(func() error {
		results, err := c.collectAddressMatches(groupCtx, targetIP, canonicalIP)
		if err != nil {
			return fmt.Errorf("failed to inspect addresses: %w", err)
		}
		addressResults = results

		return nil
	})

	if targetIP.IsPrivate() {
		group.Go(func() error {
			results, err := c.collectSubnetMatches(groupCtx, targetIP, canonicalIP)
			if err != nil {
				return fmt.Errorf("failed to inspect subnets: %w", err)
			}
			subnetResults = results

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	results := make([]IPAssociation, 0, len(instanceResults)+len(forwardingResults)+len(addressResults)+len(subnetResults))
	seen := make(map[string]struct{}, len(results))

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

// collectInstanceIPMatches gathers instance-level IP matches across all zones in the project.
func (c *Client) collectInstanceIPMatches(ctx context.Context, target net.IP, canonical string) ([]IPAssociation, error) {
	results := make([]IPAssociation, 0)
	seen := make(map[string]struct{})

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
			for _, inst := range scopedList.Instances {
				if err := ctx.Err(); err != nil {
					return err
				}

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

// collectForwardingRuleMatches adds forwarding rule associations that match the target IP.
func (c *Client) collectForwardingRuleMatches(ctx context.Context, target net.IP, canonical string) ([]IPAssociation, error) {
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
			for _, rule := range scopedList.ForwardingRules {
				if err := ctx.Err(); err != nil {
					return err
				}

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

// collectAddressMatches appends reserved addresses that map to the target IP.
func (c *Client) collectAddressMatches(ctx context.Context, target net.IP, canonical string) ([]IPAssociation, error) {
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
			for _, addr := range scopedList.Addresses {
				if err := ctx.Err(); err != nil {
					return err
				}

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

func (c *Client) collectSubnetMatches(ctx context.Context, target net.IP, canonical string) ([]IPAssociation, error) {
	results := make([]IPAssociation, 0)
	seen := make(map[string]struct{})

	call := c.service.Subnetworks.AggregatedList(c.project).Context(ctx)

	err := call.Pages(ctx, func(page *compute.SubnetworkAggregatedList) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		for scope, scopedList := range page.Items {
			if err := ctx.Err(); err != nil {
				return err
			}

			if scopedList.Subnetworks == nil {
				continue
			}

			location := locationFromScope(scope)
			for _, subnet := range scopedList.Subnetworks {
				if err := ctx.Err(); err != nil {
					return err
				}

				if subnet == nil {
					continue
				}

				if c.cache != nil {
					secondary := make([]cache.SubnetSecondaryRange, 0, len(subnet.SecondaryIpRanges))
					for _, sr := range subnet.SecondaryIpRanges {
						if sr == nil {
							continue
						}

						cidr := strings.TrimSpace(sr.IpCidrRange)
						if cidr == "" {
							continue
						}

						secondary = append(secondary, cache.SubnetSecondaryRange{
							Name: strings.TrimSpace(sr.RangeName),
							CIDR: cidr,
						})
					}

					entry := &cache.SubnetEntry{
						Project:         c.project,
						Network:         lastComponent(subnet.Network),
						Region:          lastComponent(subnet.Region),
						Name:            subnet.Name,
						SelfLink:        strings.TrimSpace(subnet.SelfLink),
						PrimaryCIDR:     strings.TrimSpace(subnet.IpCidrRange),
						SecondaryRanges: secondary,
						IPv6CIDR:        strings.TrimSpace(subnet.Ipv6CidrRange),
						Gateway:         strings.TrimSpace(subnet.GatewayAddress),
					}

					if err := c.cache.RememberSubnet(entry); err != nil {
						logger.Log.Debugf("Failed to cache subnet %s in project %s: %v", subnet.Name, c.project, err)
					}
				}

				matched, detail := subnetMatchDetails(subnet, target)
				if !matched {
					continue
				}

				association := IPAssociation{
					Project:      c.project,
					Kind:         IPAssociationSubnet,
					Resource:     subnet.Name,
					Location:     location,
					IPAddress:    canonical,
					Details:      detail,
					ResourceLink: subnet.SelfLink,
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

// ipMatch captures the association kind and descriptive text for a matched interface.
type ipMatch struct {
	kind    IPAssociationKind
	details string
}

// instanceIPMatches returns instance interface matches for the provided IP address.
func instanceIPMatches(inst *compute.Instance, target net.IP) []ipMatch {
	if inst == nil {
		return nil
	}

	matches := make([]ipMatch, 0)
	descParts := []string{}

	for _, nic := range inst.NetworkInterfaces {
		if nic == nil {
			continue
		}

		if equalIP(nic.NetworkIP, target) || equalIP(nic.Ipv6Address, target) {
			if nic.Name != "" {
				descParts = append(descParts, fmt.Sprintf("Internal interface %s", nic.Name))
			} else {
				descParts = append(descParts, "Internal interface")
			}

			networkName := lastComponent(nic.Network)
			if networkName != "" {
				descParts = append(descParts, fmt.Sprintf("network=%s", networkName))
			}

			subnetName := lastComponent(nic.Subnetwork)
			if subnetName != "" {
				descParts = append(descParts, fmt.Sprintf("subnet=%s", subnetName))
			}

			subnetLink := strings.TrimSpace(nic.Subnetwork)
			if subnetLink != "" {
				descParts = append(descParts, fmt.Sprintf("subnet_link=%s", subnetLink))
			}

			matches = append(matches, ipMatch{
				kind:    IPAssociationInstanceInternal,
				details: strings.Join(descParts, ", "),
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

// describeForwardingRule summarizes forwarding rule metadata for display.
func describeForwardingRule(rule *compute.ForwardingRule) string {
	if rule == nil {
		return ""
	}

	parts := make([]string, 0, 6)
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

	if rule.Network != "" {
		parts = append(parts, fmt.Sprintf("network=%s", lastComponent(rule.Network)))
	}

	if rule.Subnetwork != "" {
		parts = append(parts, fmt.Sprintf("subnet=%s", lastComponent(rule.Subnetwork)))
	}

	if len(parts) == 0 {
		return "Forwarding rule"
	}

	return strings.Join(parts, ", ")
}

// describeAddress creates a printable summary of an address resource.
func describeAddress(addr *compute.Address) string {
	if addr == nil {
		return ""
	}

	parts := make([]string, 0, 8)
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

	if addr.Region != "" {
		parts = append(parts, fmt.Sprintf("region=%s", lastComponent(addr.Region)))
	}

	if addr.Network != "" {
		parts = append(parts, fmt.Sprintf("network=%s", lastComponent(addr.Network)))
	}

	if addr.Subnetwork != "" {
		parts = append(parts, fmt.Sprintf("subnet=%s", lastComponent(addr.Subnetwork)))
	}

	if len(parts) == 0 {
		return "Reserved address"
	}

	return strings.Join(parts, ", ")
}

// locationFromScope extracts a human-friendly location from an aggregated API scope.
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

// lastComponent returns the trailing segment of a resource URL.
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

// equalIP compares a string IP to the provided parsed IP value.
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

func ipInCIDR(target net.IP, cidr string) bool {
	if target == nil {
		return false
	}

	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return false
	}

	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}

	return network.Contains(target)
}

func subnetMatchDetails(subnet *compute.Subnetwork, target net.IP) (bool, string) {
	if subnet == nil || target == nil {
		return false, ""
	}

	var (
		matchedCIDR string
		source      string
	)

	if cidr := strings.TrimSpace(subnet.IpCidrRange); cidr != "" && ipInCIDR(target, cidr) {
		matchedCIDR = cidr
		source = "primary"
	}

	if matchedCIDR == "" {
		for _, sec := range subnet.SecondaryIpRanges {
			if sec == nil {
				continue
			}

			if cidr := strings.TrimSpace(sec.IpCidrRange); cidr != "" && ipInCIDR(target, cidr) {
				matchedCIDR = cidr
				if sec.RangeName != "" {
					source = fmt.Sprintf("secondary:%s", sec.RangeName)
				} else {
					source = "secondary"
				}
				break
			}
		}
	}

	if matchedCIDR == "" {
		if cidr := strings.TrimSpace(subnet.Ipv6CidrRange); cidr != "" && ipInCIDR(target, cidr) {
			matchedCIDR = cidr
			source = "ipv6"
		}
	}

	if matchedCIDR == "" {
		return false, ""
	}

	detailParts := make([]string, 0, 4)
	if networkName := lastComponent(subnet.Network); networkName != "" {
		detailParts = append(detailParts, fmt.Sprintf("network=%s", networkName))
	}

	detailParts = append(detailParts, fmt.Sprintf("cidr=%s", matchedCIDR))
	if source != "" {
		detailParts = append(detailParts, fmt.Sprintf("range=%s", source))
	}

	gateway := strings.TrimSpace(subnet.GatewayAddress)
	if gateway != "" {
		detailParts = append(detailParts, fmt.Sprintf("gateway_ip=%s", gateway))
		if equalIP(gateway, target) {
			detailParts = append(detailParts, "gateway=true")
		}
	} else if subnet.GatewayAddress != "" && equalIP(subnet.GatewayAddress, target) {
		detailParts = append(detailParts, "gateway=true")
	}

	return true, strings.Join(detailParts, ", ")
}

// appendAssociation adds a unique association to the results slice while preventing duplicates.
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
