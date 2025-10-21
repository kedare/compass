package output

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/kedare/compass/internal/gcp"
)

// DisplayIPLookupResults renders IP lookup associations using the requested format.
// Supported formats:
//   - "json": raw JSON array with project, resource, and metadata
//   - "table": tabular summary suitable for terminals
//   - "text" (default): readable bullet list with contextual details
func DisplayIPLookupResults(results []gcp.IPAssociation, format string) error {
	switch strings.ToLower(format) {
	case "json":
		return displayJSON(results)
	case "table":
		return displayIPTable(results)
	default:
		return displayIPText(results)
	}
}

func displayIPTable(results []gcp.IPAssociation) error {
	if len(results) == 0 {
		fmt.Println("No IP associations found.")

		return nil
	}

	subnetCIDRs := buildSubnetCIDRMap(results)

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleLight)
	t.Style().Format.Header = text.FormatDefault
	t.AppendHeader(table.Row{"Project", "Type", "Resource", "Location", "IP", "Details", "Notes"})

	for _, assoc := range results {
		ipValue := formatIPWithMask(assoc, subnetCIDRs)
		if ipValue == "" {
			ipValue = assoc.IPAddress
		}

		detail := assoc.Details
		note := formatDetailNote(assoc, subnetCIDRs)
		if note == "" {
			note = assoc.Details
		}

		t.AppendRow(table.Row{
			assoc.Project,
			describeAssociationKind(assoc.Kind),
			assoc.Resource,
			assoc.Location,
			ipValue,
			detail,
			note,
		})
	}

	t.Render()

	return nil
}

func displayIPText(results []gcp.IPAssociation) error {
	if len(results) == 0 {
		fmt.Println("No IP associations found.")

		return nil
	}

	subnetCIDRs := buildSubnetCIDRMap(results)

	fmt.Printf("Found %d association(s):\n\n", len(results))
	for _, assoc := range results {
		fmt.Printf("- %s • %s\n", assoc.Project, describeAssociationKind(assoc.Kind))
		fmt.Printf("  Resource: %s\n", assoc.Resource)

		if ipValue := formatIPWithMask(assoc, subnetCIDRs); ipValue != "" {
			fmt.Printf("  IP:       %s\n", ipValue)
		}

		path := formatAssociationPath(assoc)
		if path != "" {
			fmt.Printf("  Path:     %s\n", path)
		}

		if assoc.Details != "" {
			fmt.Printf("  Details:  %s\n", assoc.Details)
		}

		if note := formatDetailNote(assoc, subnetCIDRs); note != "" {
			fmt.Printf("  Notes:    %s\n", note)
		}

		fmt.Println()
	}

	return nil
}

func describeAssociationKind(kind gcp.IPAssociationKind) string {
	switch kind {
	case gcp.IPAssociationInstanceInternal:
		return "Instance (internal)"
	case gcp.IPAssociationInstanceExternal:
		return "Instance (external)"
	case gcp.IPAssociationForwardingRule:
		return "Forwarding rule"
	case gcp.IPAssociationAddress:
		return "Reserved address"
	case gcp.IPAssociationSubnet:
		return "Subnet range"
	default:
		return string(kind)
	}
}

func buildSubnetCIDRMap(results []gcp.IPAssociation) map[string]string {
	m := make(map[string]string)
	for _, assoc := range results {
		if assoc.Kind != gcp.IPAssociationSubnet {
			continue
		}

		if assoc.Resource == "" {
			continue
		}

		cidr := extractDetailComponent(assoc.Details, "cidr")
		if cidr == "" {
			continue
		}

		keys := make([]string, 0, 3)
		keys = append(keys, makeSubnetKey(assoc.Project, assoc.Resource))
		if assoc.Location != "" {
			keys = append(keys, makeSubnetKey(assoc.Project, assoc.Location, assoc.Resource))
		}
		if assoc.ResourceLink != "" {
			if name := lastSegment(assoc.ResourceLink); name != "" {
				keys = append(keys, makeSubnetKey(assoc.Project, name))
			}
		}

		for _, key := range keys {
			if key != "" {
				m[key] = cidr
			}
		}
	}

	return m
}

func formatIPWithMask(assoc gcp.IPAssociation, subnetCIDRs map[string]string) string {
	ip := strings.TrimSpace(assoc.IPAddress)
	if ip == "" {
		return ""
	}

	var cidr string
	switch assoc.Kind {
	case gcp.IPAssociationSubnet:
		cidr = extractDetailComponent(assoc.Details, "cidr")
	default:
		subnet := extractDetailComponent(assoc.Details, "subnet")
		subnetLink := extractDetailComponent(assoc.Details, "subnet_link")
		cidr = lookupSubnetCIDR(subnetCIDRs, assoc.Project, assoc.Location, subnet, subnetLink)
		if cidr == "" {
			cidr = extractDetailComponent(assoc.Details, "cidr")
		}
	}

	if cidr == "" {
		return ip
	}

	if mask := maskBits(cidr); mask != "" {
		return fmt.Sprintf("%s/%s", ip, mask)
	}

	return ip
}

func maskBits(cidr string) string {
	_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil || network == nil {
		return ""
	}

	ones, _ := network.Mask.Size()
	if ones < 0 {
		return ""
	}

	return fmt.Sprintf("%d", ones)
}

func formatAssociationPath(assoc gcp.IPAssociation) string {
	segments := make([]string, 0, 4)
	if assoc.Project != "" {
		segments = append(segments, assoc.Project)
	}

	if network := extractDetailComponent(assoc.Details, "network"); network != "" {
		segments = append(segments, network)
	}

	if assoc.Location != "" {
		segments = append(segments, assoc.Location)
	}

	switch assoc.Kind {
	case gcp.IPAssociationSubnet:
		if assoc.Resource != "" {
			segments = append(segments, assoc.Resource)
		}
	default:
		if subnet := extractDetailComponent(assoc.Details, "subnet"); subnet != "" {
			segments = append(segments, subnet)
		}
	}

	return strings.Join(filterSegments(segments), " > ")
}

func formatDetailNote(assoc gcp.IPAssociation, subnetCIDRs map[string]string) string {
	detail := strings.TrimSpace(assoc.Details)
	switch assoc.Kind {
	case gcp.IPAssociationSubnet:
		if strings.Contains(detail, "gateway=true") {
			cidr := extractDetailComponent(detail, "cidr")
			if cidr != "" {
				return fmt.Sprintf("Google Cloud default gateway for subnet %s (%s)", assoc.Resource, cidr)
			}
			return fmt.Sprintf("Google Cloud default gateway for subnet %s", assoc.Resource)
		}

		cidr := extractDetailComponent(detail, "cidr")
		if cidr != "" {
			rangeType := humanizeRange(extractDetailComponent(detail, "range"))
			if rangeType != "" {
				note := fmt.Sprintf("Subnet range %s (%s)", cidr, rangeType)
				if extra := classifySpecialIP(assoc, subnetCIDRs); extra != "" {
					note = note + "; " + extra
				}
				return note
			}
			note := fmt.Sprintf("Subnet range %s", cidr)
			if extra := classifySpecialIP(assoc, subnetCIDRs); extra != "" {
				note = note + "; " + extra
			}
			return note
		}

		note := stripDetailKeys(detail, []string{"network", "cidr", "range"})
		if extra := classifySpecialIP(assoc, subnetCIDRs); extra != "" {
			if note != "" {
				note = note + "; " + extra
			} else {
				note = extra
			}
		}
		return note
	default:
		clean := stripDetailKeys(detail, []string{"network", "subnet", "cidr"})
		clean = strings.ReplaceAll(clean, "gateway=true", "Google Cloud default gateway for this subnet")
		extra := classifySpecialIP(assoc, subnetCIDRs)
		if extra != "" {
			if clean != "" {
				clean = clean + "; " + extra
			} else {
				clean = extra
			}
		}
		return strings.TrimSpace(clean)
	}
}

// classifySpecialIP identifies whether the IP is a subnet network or broadcast address.
func classifySpecialIP(assoc gcp.IPAssociation, subnetCIDRs map[string]string) string {
	if assoc.IPAddress == "" {
		return ""
	}

	var cidr string
	switch assoc.Kind {
	case gcp.IPAssociationSubnet:
		cidr = extractDetailComponent(assoc.Details, "cidr")
	default:
		subnet := extractDetailComponent(assoc.Details, "subnet")
		if subnet == "" {
			return ""
		}
		subnetLink := extractDetailComponent(assoc.Details, "subnet_link")
		cidr = lookupSubnetCIDR(subnetCIDRs, assoc.Project, assoc.Location, subnet, subnetLink)
		if cidr == "" {
			cidr = extractDetailComponent(assoc.Details, "cidr")
		}
	}

	if cidr == "" {
		return ""
	}

	_, network, err := net.ParseCIDR(cidr)
	if err != nil || network == nil {
		return ""
	}

	ip := net.ParseIP(strings.TrimSpace(assoc.IPAddress))
	if ip == nil {
		return ""
	}

	first := network.IP
	last := broadcastAddr(network)
	if first != nil && first.Equal(ip) {
		return "Subnet network address (reserved)"
	}

	if last != nil && last.Equal(ip) {
		return "Subnet broadcast address (reserved)"
	}

	return ""
}

func broadcastAddr(network *net.IPNet) net.IP {
	if network == nil {
		return nil
	}

	ip := make(net.IP, len(network.IP))
	copy(ip, network.IP)

	for i := range ip {
		ip[i] |= ^network.Mask[i]
	}

	return ip
}

func extractDetailComponent(detail, key string) string {
	if detail == "" {
		return ""
	}

	lowerKey := strings.ToLower(strings.TrimSpace(key))
	for _, part := range strings.Split(detail, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if idx := strings.Index(part, "="); idx != -1 {
			keyPart := strings.ToLower(strings.TrimSpace(part[:idx]))
			if keyPart == lowerKey {
				return strings.TrimSpace(part[idx+1:])
			}
		}
	}

	return ""
}

func humanizeRange(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if strings.HasPrefix(strings.ToLower(value), "secondary:") {
		return fmt.Sprintf("secondary (%s)", strings.TrimPrefix(value, "secondary:"))
	}

	return value
}

func stripDetailKeys(detail string, keys []string) string {
	if detail == "" {
		return ""
	}

	if len(keys) == 0 {
		return detail
	}

	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}

	parts := make([]string, 0)
	for _, part := range strings.Split(detail, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		if idx := strings.Index(trimmed, "="); idx != -1 {
			key := strings.ToLower(strings.TrimSpace(trimmed[:idx]))
			if _, found := keySet[key]; found {
				continue
			}
		} else {
			if _, found := keySet[strings.ToLower(trimmed)]; found {
				continue
			}
		}

		parts = append(parts, trimmed)
	}

	return strings.Join(parts, ", ")
}

func filterSegments(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, val := range values {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		filtered = append(filtered, val)
	}

	return filtered
}

// lookupSubnetCIDR finds the cached CIDR for a given subnet name or self-link.
func lookupSubnetCIDR(subnetCIDRs map[string]string, project, location, subnetName, subnetLink string) string {
	keys := make([]string, 0, 4)
	if subnetName != "" {
		keys = append(keys, makeSubnetKey(project, subnetName))
		if location != "" {
			keys = append(keys, makeSubnetKey(project, location, subnetName))
		}
	}

	if subnetLink != "" {
		if name := lastSegment(subnetLink); name != "" {
			keys = append(keys, makeSubnetKey(project, name))
		}
	}

	for _, key := range keys {
		if cidr, ok := subnetCIDRs[key]; ok {
			return cidr
		}
	}

	return ""
}

// makeSubnetKey normalizes subnet identifiers for cache lookups.
func makeSubnetKey(project string, parts ...string) string {
	segments := []string{strings.ToLower(strings.TrimSpace(project))}
	for _, part := range parts {
		p := strings.ToLower(strings.TrimSpace(part))
		if p != "" {
			segments = append(segments, p)
		}
	}

	return strings.Join(segments, "|")
}

// lastSegment extracts the trailing component of a self-link or path.
func lastSegment(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	if idx := strings.LastIndex(path, "/"); idx != -1 && idx+1 < len(path) {
		return path[idx+1:]
	}

	return path
}
