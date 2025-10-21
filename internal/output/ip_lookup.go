package output

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/kedare/compass/internal/gcp"
	"github.com/mattn/go-runewidth"
	"github.com/pterm/pterm"
	"golang.org/x/term"
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

	// Build table data
	tableData := pterm.TableData{
		{"Project", "Type", "Resource", "Location", "IP", "Details", "Notes"},
	}

	for _, assoc := range results {
		ipValue := formatIPWithMask(assoc, subnetCIDRs)
		if ipValue == "" {
			ipValue = assoc.IPAddress
		}

		detail := detailForDisplay(assoc)
		note, hasNote := formatDetailNote(assoc, subnetCIDRs)
		if !hasNote {
			note = ""
		}

		tableData = append(tableData, []string{
			assoc.Project,
			describeAssociationKind(assoc.Kind),
			assoc.Resource,
			assoc.Location,
			ipValue,
			detail,
			note,
		})
	}

	return pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
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

		pairs := make([]labelValue, 0, 6)

		if resource := strings.TrimSpace(assoc.Resource); resource != "" {
			pairs = append(pairs, labelValue{Label: "Resource", Value: resource})
		}

		if assoc.Kind == gcp.IPAssociationSubnet {
			if subnet := summarizeSubnet(assoc); subnet != "" {
				pairs = append(pairs, labelValue{Label: "Subnet", Value: subnet})
			}
		} else if ipValue := formatIPWithMask(assoc, subnetCIDRs); ipValue != "" {
			pairs = append(pairs, labelValue{Label: "IP", Value: ipValue})
		}

		if path := formatAssociationPath(assoc); path != "" {
			pairs = append(pairs, labelValue{Label: "Path", Value: path})
		}

		if detail := detailForDisplay(assoc); detail != "" {
			pairs = append(pairs, labelValue{Label: "Details", Value: detail})
		}

		if note, hasNote := formatDetailNote(assoc, subnetCIDRs); hasNote {
			pairs = append(pairs, labelValue{Label: "Notes", Value: note})
		}

		if block := renderAssociationPanels(pairs); block != "" {
			fmt.Print(block)
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
	var cidr string
	switch assoc.Kind {
	case gcp.IPAssociationSubnet:
		cidr = extractDetailComponent(assoc.Details, "cidr")
		if cidr != "" {
			return cidr
		}
		if assoc.Resource != "" {
			return assoc.Resource
		}
		return strings.TrimSpace(assoc.IPAddress)
	default:
		ip := strings.TrimSpace(assoc.IPAddress)
		if ip == "" {
			return ""
		}

		subnet := extractDetailComponent(assoc.Details, "subnet")
		subnetLink := extractDetailComponent(assoc.Details, "subnet_link")
		cidr = lookupSubnetCIDR(subnetCIDRs, assoc.Project, assoc.Location, subnet, subnetLink)
		if cidr == "" {
			cidr = extractDetailComponent(assoc.Details, "cidr")
		}
		if cidr == "" {
			return ip
		}

		if mask := maskBits(cidr); mask != "" {
			return fmt.Sprintf("%s/%s", ip, mask)
		}

		return ip
	}
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

func formatDetailNote(assoc gcp.IPAssociation, subnetCIDRs map[string]string) (string, bool) {
	detail := strings.TrimSpace(assoc.Details)
	displayDetail := detailForDisplay(assoc)
	var note string

	switch assoc.Kind {
	case gcp.IPAssociationSubnet:
		if strings.Contains(detail, "gateway=true") {
			cidr := extractDetailComponent(detail, "cidr")
			if cidr != "" {
				note = fmt.Sprintf("Google Cloud default gateway for subnet %s (%s)", assoc.Resource, cidr)
			} else {
				note = fmt.Sprintf("Google Cloud default gateway for subnet %s", assoc.Resource)
			}
			break
		}

		cidr := extractDetailComponent(detail, "cidr")
		if cidr != "" {
			rangeType := humanizeRange(extractDetailComponent(detail, "range"))
			if rangeType != "" {
				note = fmt.Sprintf("Subnet range %s (%s)", cidr, rangeType)
			} else {
				note = fmt.Sprintf("Subnet range %s", cidr)
			}
			if extra := classifySpecialIP(assoc, subnetCIDRs); extra != "" {
				note = note + "; " + extra
			}
			break
		}

		note = stripDetailKeys(detail, []string{"network", "cidr", "range", "subnet_link", "region"})
		if extra := classifySpecialIP(assoc, subnetCIDRs); extra != "" {
			if note != "" {
				note = note + "; " + extra
			} else {
				note = extra
			}
		}
	default:
		clean := stripDetailKeys(detail, []string{"network", "subnet", "cidr", "subnet_link", "region"})
		clean = strings.ReplaceAll(clean, "gateway=true", "Google Cloud default gateway for this subnet")
		extra := classifySpecialIP(assoc, subnetCIDRs)
		if extra != "" {
			if clean != "" {
				clean = clean + "; " + extra
			} else {
				clean = extra
			}
		}
		note = strings.TrimSpace(clean)
	}

	note = strings.TrimSpace(note)
	if note == "" {
		return "", false
	}

	if normalizeDetailValue(displayDetail) == normalizeDetailValue(note) {
		return "", false
	}

	if segment := firstDetailSegment(displayDetail); segment != "" && normalizeDetailValue(segment) == normalizeDetailValue(note) {
		return "", false
	}

	if detail != "" && normalizeDetailValue(detail) == normalizeDetailValue(note) {
		return "", false
	}

	return note, true
}

func normalizeDetailValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	replacer := strings.NewReplacer(
		";", ",",
		" ", "",
		"\t", "",
		"\n", "",
		"\r", "",
	)

	value = replacer.Replace(strings.ToLower(value))

	return value
}

func detailForDisplay(assoc gcp.IPAssociation) string {
	detail := strings.TrimSpace(assoc.Details)
	if detail == "" {
		return ""
	}

	if assoc.Kind == gcp.IPAssociationSubnet {
		return detailForSubnet(assoc, detail)
	}

	cleaned := stripDetailKeys(detail, []string{"subnet_link", "network", "subnet", "region"})

	return strings.TrimSpace(cleaned)
}

func detailForSubnet(assoc gcp.IPAssociation, detail string) string {
	parts := make([]string, 0, 4)

	rangeComponent := strings.TrimSpace(extractDetailComponent(detail, "range"))
	if rangeComponent != "" {
		if human := humanizeRange(rangeComponent); human != "" {
			rangeComponent = human
		}
		parts = append(parts, fmt.Sprintf("range=%s", rangeComponent))
	}

	if usable := usableRangeString(extractDetailComponent(detail, "cidr")); usable != "" {
		parts = append(parts, fmt.Sprintf("usable=%s", usable))
	}

	gateway := extractDetailComponent(detail, "gateway_ip")
	if gateway == "" && strings.Contains(detail, "gateway=true") {
		gateway = strings.TrimSpace(assoc.IPAddress)
	}
	if gateway != "" {
		parts = append(parts, fmt.Sprintf("gateway=%s", gateway))
	}

	extra := stripDetailKeys(detail, []string{
		"subnet_link",
		"network",
		"subnet",
		"region",
		"cidr",
		"gateway_ip",
		"range",
	})

	for _, segment := range strings.Split(extra, ",") {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		if strings.EqualFold(trimmed, "gateway=true") {
			continue
		}
		parts = append(parts, trimmed)
	}

	return strings.Join(parts, ", ")
}

func usableRangeString(cidr string) string {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return ""
	}

	_, network, err := net.ParseCIDR(cidr)
	if err != nil || network == nil {
		return ""
	}

	baseIP := network.IP
	if baseIP == nil {
		return ""
	}

	if ipv4 := baseIP.To4(); ipv4 != nil {
		start := cloneIP(ipv4)
		end := broadcastAddr(network)
		if end == nil {
			return ""
		}
		end = cloneIP(end.To4())
		if end == nil {
			return ""
		}

		ones, bits := network.Mask.Size()
		if bits != net.IPv4len*8 || ones < 0 {
			return ""
		}

		switch {
		case ones <= 30:
			first := incrementIP(start)
			last := decrementIP(end)
			if first == nil || last == nil || compareIPs(first, last) > 0 {
				return ""
			}
			if compareIPs(first, last) == 0 {
				return net.IP(first).String()
			}

			return fmt.Sprintf("%s-%s", net.IP(first).String(), net.IP(last).String())
		case ones == 31:
			return fmt.Sprintf("%s-%s", net.IP(start).String(), net.IP(end).String())
		default: // /32
			return net.IP(start).String()
		}
	}

	return ""
}

func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}

	res := make(net.IP, len(ip))
	copy(res, ip)

	return res
}

func incrementIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}

	res := cloneIP(ip)
	for i := len(res) - 1; i >= 0; i-- {
		res[i]++
		if res[i] != 0 {
			break
		}
	}

	return res
}

func decrementIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}

	res := cloneIP(ip)
	for i := len(res) - 1; i >= 0; i-- {
		if res[i] == 0 {
			res[i] = 0xff
			continue
		}
		res[i]--
		break
	}

	return res
}

func compareIPs(a, b net.IP) int {
	if a == nil || b == nil {
		switch {
		case a == nil && b == nil:
			return 0
		case a == nil:
			return -1
		default:
			return 1
		}
	}

	if len(a) != len(b) {
		if a4 := a.To4(); a4 != nil {
			a = a4
		} else {
			a = a.To16()
		}

		if b4 := b.To4(); b4 != nil {
			b = b4
		} else {
			b = b.To16()
		}
	}

	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}

		return 1
	}

	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return -1
		}

		if a[i] > b[i] {
			return 1
		}
	}

	return 0
}

type labelValue struct {
	Label string
	Value string
}

func summarizeSubnet(assoc gcp.IPAssociation) string {
	if assoc.Kind != gcp.IPAssociationSubnet {
		return ""
	}

	cidr := extractDetailComponent(assoc.Details, "cidr")
	subnet := strings.TrimSpace(assoc.Resource)

	switch {
	case subnet != "" && cidr != "":
		return fmt.Sprintf("%s (%s)", subnet, cidr)
	case subnet != "":
		return subnet
	case cidr != "":
		return cidr
	default:
		return ""
	}
}

func renderAssociationPanels(pairs []labelValue) string {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		plain := renderPlainKeyValues(pairs)
		if plain == "" {
			return ""
		}

		return indentBlock(plain, "  ") + "\n"
	}

	panels := make(pterm.Panels, 0, len(pairs))

	for _, pair := range pairs {
		label := strings.TrimSpace(pair.Label)
		value := strings.TrimSpace(pair.Value)
		if label == "" || value == "" {
			continue
		}

		styledLabel := pterm.NewStyle(pterm.FgLightBlue).Sprint(label + ":")
		panels = append(panels, []pterm.Panel{
			{Data: styledLabel},
			{Data: value},
		})
	}

	if len(panels) == 0 {
		return ""
	}

	rendered, err := pterm.DefaultPanel.
		WithPanels(panels).
		WithPadding(1).
		WithBottomPadding(0).
		WithSameColumnWidth(true).
		Srender()
	if err != nil {
		rendered = renderPlainKeyValues(pairs)
	}

	rendered = strings.TrimRight(rendered, "\n")
	if rendered == "" {
		return ""
	}

	indented := indentBlock(rendered, "  ")

	return indented + "\n"
}

func indentBlock(block, prefix string) string {
	block = strings.TrimRight(block, "\n")
	if block == "" {
		return ""
	}

	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}

	return strings.Join(lines, "\n")
}

func renderPlainKeyValues(pairs []labelValue) string {
	filtered := make([]labelValue, 0, len(pairs))
	maxWidth := 0

	for _, pair := range pairs {
		label := strings.TrimSpace(pair.Label)
		value := strings.TrimSpace(pair.Value)
		if label == "" || value == "" {
			continue
		}

		if w := runewidth.StringWidth(label); w > maxWidth {
			maxWidth = w
		}

		filtered = append(filtered, labelValue{Label: label, Value: value})
	}

	if len(filtered) == 0 {
		return ""
	}

	var b strings.Builder
	for _, pair := range filtered {
		fmt.Fprintf(&b, "%-*s %s\n", maxWidth+1, pair.Label+":", pair.Value)
	}

	return strings.TrimRight(b.String(), "\n")
}

func firstDetailSegment(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}

	if idx := strings.Index(detail, ","); idx != -1 {
		return strings.TrimSpace(detail[:idx])
	}

	return detail
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
