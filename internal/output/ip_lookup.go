package output

import (
	"fmt"
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

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleLight)
	t.Style().Format.Header = text.FormatDefault
	t.AppendHeader(table.Row{"Project", "Type", "Resource", "Location", "IP", "Details"})

	for _, assoc := range results {
		t.AppendRow(table.Row{
			assoc.Project,
			describeAssociationKind(assoc.Kind),
			assoc.Resource,
			assoc.Location,
			assoc.IPAddress,
			assoc.Details,
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

	fmt.Printf("Found %d association(s):\n\n", len(results))
	for _, assoc := range results {
		fmt.Printf("- %s • %s\n", assoc.Project, describeAssociationKind(assoc.Kind))
		fmt.Printf("  Resource: %s\n", assoc.Resource)

		if assoc.IPAddress != "" {
			fmt.Printf("  IP:       %s\n", assoc.IPAddress)
		}

		if assoc.Location != "" {
			fmt.Printf("  Location: %s\n", assoc.Location)
		}

		if assoc.Details != "" {
			fmt.Printf("  Details:  %s\n", assoc.Details)
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
