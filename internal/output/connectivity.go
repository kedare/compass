// Package output provides formatting functions for terminal display
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"cx/internal/gcp"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/mattn/go-runewidth"
)

const (
	forwardTableWidth      = 85
	separatorWidth         = 1
	leftPaddingWidth       = 4
	sideBySideSafetyMargin = 40
)

func detectTerminalWidth() (int, bool) {
	if width, ok := systemTerminalWidth(); ok {
		return width, true
	}

	if raw, ok := os.LookupEnv("COLUMNS"); ok {
		if width, err := strconv.Atoi(raw); err == nil && width > 0 {
			return width, true
		}
	}

	return 0, false
}

func canDisplaySideBySide(forwardLines, returnLines []string) bool {
	width, ok := detectTerminalWidth()
	if !ok {
		return false
	}

	forwardWidth := maxVisibleWidth(forwardLines)
	returnWidth := maxVisibleWidth(returnLines)
	required := leftPaddingWidth + forwardWidth + separatorWidth + returnWidth + sideBySideSafetyMargin*2

	return width >= required
}

// DisplayConnectivityTestResult formats and displays a connectivity test result.
func DisplayConnectivityTestResult(result *gcp.ConnectivityTestResult, format string) error {
	switch format {
	case "json":
		return displayJSON(result)
	case "detailed":
		return displayDetailed(result)
	default:
		return displayText(result)
	}
}

// DisplayConnectivityTestList formats and displays a list of connectivity tests.
func DisplayConnectivityTestList(results []*gcp.ConnectivityTestResult, format string) error {
	switch format {
	case "json":
		return displayJSON(results)
	case "table":
		return displayTable(results)
	default:
		return displayListText(results)
	}
}

// displayText displays a connectivity test result in human-readable format.
func displayText(result *gcp.ConnectivityTestResult) error {
	// Determine if test is reachable
	isReachable := false

	status := "UNKNOWN"
	if result.ReachabilityDetails != nil {
		status = result.ReachabilityDetails.Result
		isReachable = strings.Contains(strings.ToUpper(status), "REACHABLE") &&
			!strings.Contains(strings.ToUpper(status), "UNREACHABLE")
	}

	// Print header with status icon
	statusIcon := "✓"
	var formattedStatus string

	if !isReachable {
		statusIcon = "✗"
		formattedStatus = text.Colors{text.Bold, text.FgRed}.Sprint(status)
	} else {
		formattedStatus = text.Colors{text.Bold, text.FgGreen}.Sprint(status)
	}

	fmt.Printf("%s Connectivity Test: %s\n", statusIcon, result.DisplayName)
	fmt.Printf("  Status:        %s\n", formattedStatus)

	// Display source
	if result.Source != nil {
		sourceStr := formatEndpoint(result.Source, false)
		fmt.Printf("  Source:        %s\n", sourceStr)
	}

	// Display destination
	if result.Destination != nil {
		destStr := formatEndpoint(result.Destination, true)
		fmt.Printf("  Destination:   %s\n", destStr)
	}

	// Display protocol
	if result.Protocol != "" {
		fmt.Printf("  Protocol:      %s\n", result.Protocol)
	}

	// Display path analysis - combine forward and return traces
	if result.ReachabilityDetails != nil && len(result.ReachabilityDetails.Traces) > 0 {
		fmt.Println("\n  Path Analysis:")

		forwardTraces := result.ReachabilityDetails.Traces

		var returnTraces []*gcp.Trace
		if result.ReturnReachabilityDetails != nil {
			returnTraces = result.ReturnReachabilityDetails.Traces
		}

		displayForwardAndReturnPaths(forwardTraces, returnTraces, isReachable)
	}

	// Display result message
	fmt.Println()

	if isReachable {
		fmt.Println("  Result: Connection successful ✓")
	} else {
		fmt.Println("  Result: Connection failed ✗")

		if result.ReachabilityDetails != nil && result.ReachabilityDetails.Error != "" {
			fmt.Printf("  Error:  %s\n", result.ReachabilityDetails.Error)
		}
		// Display suggested fixes if available
		displaySuggestedFixes(result)
	}

	return nil
}

// displayDetailed displays a connectivity test with full details.
func displayDetailed(result *gcp.ConnectivityTestResult) error {
	if err := displayText(result); err != nil {
		return err
	}

	fmt.Println("\n  Additional Details:")
	fmt.Printf("  Test Name:     %s\n", result.Name)

	if result.Description != "" {
		fmt.Printf("  Description:   %s\n", result.Description)
	}

	if !result.CreateTime.IsZero() {
		fmt.Printf("  Created:       %s\n", result.CreateTime.Format(time.RFC3339))
	}

	if !result.UpdateTime.IsZero() {
		fmt.Printf("  Updated:       %s\n", result.UpdateTime.Format(time.RFC3339))
	}

	if result.ReachabilityDetails != nil && !result.ReachabilityDetails.VerifyTime.IsZero() {
		fmt.Printf("  Verified:      %s\n", result.ReachabilityDetails.VerifyTime.Format(time.RFC3339))
	}

	return nil
}

// displayJSON displays result as JSON.
func displayJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(data)
}

// displayListText displays a list of tests in text format.
func displayListText(results []*gcp.ConnectivityTestResult) error {
	if len(results) == 0 {
		fmt.Println("No connectivity tests found")

		return nil
	}

	fmt.Printf("Found %d connectivity test(s):\n\n", len(results))

	for _, result := range results {
		status := "UNKNOWN"
		statusIcon := "?"
		var formattedStatus string

		if result.ReachabilityDetails != nil {
			status = result.ReachabilityDetails.Result
			if strings.Contains(strings.ToUpper(status), "REACHABLE") &&
				!strings.Contains(strings.ToUpper(status), "UNREACHABLE") {
				statusIcon = "✓"
				formattedStatus = text.Colors{text.Bold, text.FgGreen}.Sprint(status)
			} else {
				statusIcon = "✗"
				formattedStatus = text.Colors{text.Bold, text.FgRed}.Sprint(status)
			}
		} else {
			formattedStatus = text.Bold.Sprint(status)
		}

		fmt.Printf("%s %s\n", statusIcon, result.DisplayName)
		fmt.Printf("  Status: %s\n", formattedStatus)

		if result.Source != nil {
			fmt.Printf("  Source: %s\n", formatEndpoint(result.Source, false))
		}

		if result.Destination != nil {
			fmt.Printf("  Dest:   %s\n", formatEndpoint(result.Destination, true))
		}

		fmt.Println()
	}

	return nil
}

// displayTable displays results in table format.
func displayTable(results []*gcp.ConnectivityTestResult) error {
	if len(results) == 0 {
		fmt.Println("No connectivity tests found")

		return nil
	}

	// Print header
	fmt.Printf("%-3s %-30s %-15s %-30s %-30s\n", "ST", "NAME", "STATUS", "SOURCE", "DESTINATION")
	fmt.Println(strings.Repeat("-", 110))

	// Print rows
	for _, result := range results {
		status := "UNKNOWN"
		statusIcon := "?"

		if result.ReachabilityDetails != nil {
			status = result.ReachabilityDetails.Result
			if strings.Contains(strings.ToUpper(status), "REACHABLE") &&
				!strings.Contains(strings.ToUpper(status), "UNREACHABLE") {
				statusIcon = "✓"
			} else {
				statusIcon = "✗"
			}
		}

		name := truncate(result.DisplayName, 30)
		statusStr := truncate(status, 15)
		source := truncate(formatEndpoint(result.Source, false), 30)
		dest := truncate(formatEndpoint(result.Destination, true), 30)

		fmt.Printf("%-3s %-30s %-15s %-30s %-30s\n", statusIcon, name, statusStr, source, dest)
	}

	return nil
}

// displayTraces displays network traces in a table layout.
func displayTraces(traces []*gcp.Trace, isReachable bool) {
	for traceIdx, trace := range traces {
		if len(trace.Steps) == 0 {
			continue
		}

		// If there are multiple traces, add a header to distinguish them
		if len(traces) > 1 {
			if traceIdx > 0 {
				fmt.Println() // Add spacing between traces
			}

			fmt.Printf("    Path %d of %d:\n", traceIdx+1, len(traces))
		}

		// Create table
		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.SetStyle(table.StyleLight)
		t.Style().Options.SeparateRows = true

		// Add header
		t.AppendHeader(table.Row{"#", "Step", "Type", "Resource", "Status"})

		// Add each step as a row
		for i, step := range trace.Steps {
			stepNum := fmt.Sprintf("%d", i+1)
			stepType, resource, status := formatTraceStepForTable(step)

			// Apply styling based on step state
			if step.CausesDrop {
				// Red bold for failures
				stepNum = text.Bold.Sprint(text.FgRed.Sprint(stepNum))
				stepType = text.Bold.Sprint(text.FgRed.Sprint(stepType))
				resource = text.Bold.Sprint(text.FgRed.Sprint(resource))
				status = text.Bold.Sprint(text.FgRed.Sprint(status))
			} else {
				// Green for successful steps
				status = text.FgGreen.Sprint(status)
			}

			t.AppendRow(table.Row{stepNum, getStepIcon(i, len(trace.Steps), step.CausesDrop), stepType, resource, status})
		}

		t.Render()
	}
}

// displayForwardAndReturnPaths displays forward and return traces, pairing them when possible.
func displayForwardAndReturnPaths(forwardTraces []*gcp.Trace, returnTraces []*gcp.Trace, isReachable bool) {
	// If we have both forward and return traces, try to pair them
	if len(forwardTraces) > 0 && len(returnTraces) > 0 {
		// For simplicity, pair the first forward with the first return
		// In more complex scenarios, you might want to match by endpoint or other criteria
		total := len(forwardTraces)
		for i := 0; i < total && i < len(returnTraces); i++ {
			if i > 0 {
				fmt.Println()
			}

			if displayed := displayForwardAndReturnSideBySide(forwardTraces[i], returnTraces[i], total, i); !displayed {
				forwardTitle := "Forward Path"
				if total > 1 {
					forwardTitle = fmt.Sprintf("Forward Path %d of %d", i+1, total)
				}

				returnTitle := "Return Path"
				if len(returnTraces) > 1 {
					returnTitle = fmt.Sprintf("Return Path %d of %d", i+1, len(returnTraces))
				}

				displaySingleTrace(forwardTraces[i], forwardTitle)
				fmt.Println()
				displaySingleTrace(returnTraces[i], returnTitle)
			}
		}

		// Display any extra forward traces without return
		for i := len(returnTraces); i < len(forwardTraces); i++ {
			if i > 0 {
				fmt.Println()
			}

			fmt.Printf("    Path %d of %d:\n", i+1, len(forwardTraces))
			displaySingleTrace(forwardTraces[i], "Forward Path (no return path)")
		}

		// Display any extra return traces without forward
		for i := len(forwardTraces); i < len(returnTraces); i++ {
			if i > 0 {
				fmt.Println()
			}

			displaySingleTrace(returnTraces[i], "Return Path (no forward path)")
		}
	} else if len(forwardTraces) > 0 {
		// Only forward traces
		for i, trace := range forwardTraces {
			if i > 0 {
				fmt.Println()
			}

			if len(forwardTraces) > 1 {
				fmt.Printf("    Path %d of %d:\n", i+1, len(forwardTraces))
			}

			displaySingleTrace(trace, "Forward Path")
		}
	} else if len(returnTraces) > 0 {
		// Only return traces (unusual)
		for i, trace := range returnTraces {
			if i > 0 {
				fmt.Println()
			}

			if len(returnTraces) > 1 {
				fmt.Printf("    Path %d of %d:\n", i+1, len(returnTraces))
			}

			displaySingleTrace(trace, "Return Path")
		}
	}
}

// displayForwardAndReturnSideBySide displays forward and return traces in two columns.
func displayForwardAndReturnSideBySide(forward *gcp.Trace, returnTrace *gcp.Trace, total int, index int) bool {
	// Create two tables side by side
	forwardTable := table.NewWriter()
	forwardTable.SetStyle(table.StyleLight)
	forwardTable.Style().Options.SeparateRows = true
	forwardTable.AppendHeader(table.Row{"#", "Step", "Type", "Resource", "Status"})

	returnTable := table.NewWriter()
	returnTable.SetStyle(table.StyleLight)
	returnTable.Style().Options.SeparateRows = true
	returnTable.AppendHeader(table.Row{"#", "Step", "Type", "Resource", "Status"})

	// Populate forward table
	for i, step := range forward.Steps {
		stepNum := fmt.Sprintf("%d", i+1)
		stepType, resource, status := formatTraceStepForTable(step)

		if step.CausesDrop {
			stepNum = text.Bold.Sprint(text.FgRed.Sprint(stepNum))
			stepType = text.Bold.Sprint(text.FgRed.Sprint(stepType))
			resource = text.Bold.Sprint(text.FgRed.Sprint(resource))
			status = text.Bold.Sprint(text.FgRed.Sprint(status))
		} else {
			status = text.FgGreen.Sprint(status)
		}

		forwardTable.AppendRow(table.Row{stepNum, getStepIcon(i, len(forward.Steps), step.CausesDrop), stepType, resource, status})
	}

	// Populate return table
	for i, step := range returnTrace.Steps {
		stepNum := fmt.Sprintf("%d", i+1)
		stepType, resource, status := formatTraceStepForTable(step)

		if step.CausesDrop {
			stepNum = text.Bold.Sprint(text.FgRed.Sprint(stepNum))
			stepType = text.Bold.Sprint(text.FgRed.Sprint(stepType))
			resource = text.Bold.Sprint(text.FgRed.Sprint(resource))
			status = text.Bold.Sprint(text.FgRed.Sprint(status))
		} else {
			status = text.FgGreen.Sprint(status)
		}

		returnTable.AppendRow(table.Row{stepNum, getStepIcon(i, len(returnTrace.Steps), step.CausesDrop), stepType, resource, status})
	}

	forwardLines := strings.Split(forwardTable.Render(), "\n")
	returnLines := strings.Split(returnTable.Render(), "\n")

	if !canDisplaySideBySide(forwardLines, returnLines) {
		return false
	}

	forwardWidth := maxVisibleWidth(forwardLines)

	if total > 1 {
		fmt.Printf("    Path %d of %d:\n", index+1, total)
	}

	// Print headers - pad the forward header to align with the return header
	forwardHeader := text.Bold.Sprint("Forward Path")
	returnHeader := text.Bold.Sprint("Return Path")

	paddingNeeded := forwardWidth - len(stripAnsiCodes(forwardHeader)) + separatorWidth
	if paddingNeeded < 1 {
		paddingNeeded = 1
	}

	fmt.Println("     " + forwardHeader + strings.Repeat(" ", paddingNeeded) + returnHeader)

	// Print tables side by side
	maxLines := len(forwardLines)
	if len(returnLines) > maxLines {
		maxLines = len(returnLines)
	}

	for i := 0; i < maxLines; i++ {
		forwardLine := ""
		if i < len(forwardLines) {
			forwardLine = forwardLines[i]
		}

		returnLine := ""
		if i < len(returnLines) {
			returnLine = returnLines[i]
		}

		forwardLine = padRight(forwardLine, forwardWidth)
		fmt.Printf("    %s %s\n", forwardLine, returnLine)
	}

	return true
}

// displaySingleTrace displays a single trace with a title.
func displaySingleTrace(trace *gcp.Trace, title string) {
	fmt.Println("    " + text.Bold.Sprint(title))

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleLight)
	t.Style().Options.SeparateRows = true
	t.AppendHeader(table.Row{"#", "Step", "Type", "Resource", "Status"})

	for i, step := range trace.Steps {
		stepNum := fmt.Sprintf("%d", i+1)
		stepType, resource, status := formatTraceStepForTable(step)

		if step.CausesDrop {
			stepNum = text.Bold.Sprint(text.FgRed.Sprint(stepNum))
			stepType = text.Bold.Sprint(text.FgRed.Sprint(stepType))
			resource = text.Bold.Sprint(text.FgRed.Sprint(resource))
			status = text.Bold.Sprint(text.FgRed.Sprint(status))
		} else {
			status = text.FgGreen.Sprint(status)
		}

		t.AppendRow(table.Row{stepNum, getStepIcon(i, len(trace.Steps), step.CausesDrop), stepType, resource, status})
	}

	t.Render()
}

func maxVisibleWidth(lines []string) int {
	max := 0
	for _, line := range lines {
		if width := visibleWidth(line); width > max {
			max = width
		}
	}

	return max
}

// padRight pads a string to the right with spaces, accounting for ANSI color codes.
func padRight(s string, length int) string {
	// Remove ANSI codes to calculate visible length
	visibleLen := visibleWidth(s)
	if visibleLen >= length {
		return s
	}

	return s + strings.Repeat(" ", length-visibleLen)
}

func visibleWidth(s string) int {
	return runewidth.StringWidth(stripAnsiCodes(s))
}

// stripAnsiCodes removes ANSI escape codes from a string for length calculation.
func stripAnsiCodes(s string) string {
	// Simple regex to strip ANSI codes: \x1b\[[0-9;]*m
	result := ""
	inEscape := false

	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			inEscape = true
			i++ // skip '['

			continue
		}

		if inEscape {
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				inEscape = false
			}

			continue
		}
		result += string(s[i])
	}

	return result
}

// getStepIcon returns the appropriate icon for a step based on its position and status.
func getStepIcon(index, total int, causesDrop bool) string {
	if causesDrop {
		return "✗"
	}

	if index == 0 {
		return "→"
	}

	if index == total-1 {
		return "✓"
	}

	return "→"
}

// formatTraceStep formats a trace step for display.
func formatTraceStep(step *gcp.TraceStep) string {
	if step.Instance != "" {
		return fmt.Sprintf("VM Instance (%s)", extractResourceName(step.Instance))
	}

	if step.Firewall != "" {
		status := "allow"
		if step.CausesDrop {
			status = "BLOCKED"
		}

		return fmt.Sprintf("Firewall (%s: %s)", step.Firewall, status)
	}

	if step.Route != "" {
		return fmt.Sprintf("Route (%s)", step.Route)
	}

	if step.VPC != "" {
		return fmt.Sprintf("VPC (%s)", step.VPC)
	}

	if step.LoadBalancer != "" {
		return fmt.Sprintf("Load Balancer (%s)", step.LoadBalancer)
	}

	if step.Description != "" {
		return step.Description
	}

	return step.State
}

// formatTraceStepForTable formats a trace step for table display, returning type, resource, and status.
func formatTraceStepForTable(step *gcp.TraceStep) (stepType string, resource string, status string) {
	if step.Instance != "" {
		return "VM Instance", extractResourceName(step.Instance), "OK"
	}

	if step.Firewall != "" {
		if step.CausesDrop {
			return "Firewall", step.Firewall, "BLOCKED"
		}

		return "Firewall", step.Firewall, "ALLOWED"
	}

	if step.Route != "" {
		return "Route", step.Route, "OK"
	}

	if step.VPC != "" {
		return "VPC", step.VPC, "OK"
	}

	if step.LoadBalancer != "" {
		return "Load Balancer", step.LoadBalancer, "OK"
	}

	if step.Description != "" {
		return "Step", step.Description, step.State
	}

	return "Step", "-", step.State
}

// displaySuggestedFixes displays suggested fixes for failed tests.
func displaySuggestedFixes(result *gcp.ConnectivityTestResult) {
	if result.ReachabilityDetails == nil || len(result.ReachabilityDetails.Traces) == 0 {
		return
	}

	// Find the step that caused the drop
	for _, trace := range result.ReachabilityDetails.Traces {
		for _, step := range trace.Steps {
			if step.CausesDrop {
				if step.Firewall != "" {
					fmt.Println("\n  Suggested Fix:")
					fmt.Printf("  Add firewall rule allowing %s traffic", result.Protocol)

					if result.Source != nil && result.Source.IPAddress != "" {
						fmt.Printf(" from %s", result.Source.IPAddress)
					}

					if result.Destination != nil {
						if result.Destination.IPAddress != "" {
							fmt.Printf(" to %s", result.Destination.IPAddress)
						}

						if result.Destination.Port > 0 {
							fmt.Printf(":%d", result.Destination.Port)
						}
					}

					fmt.Println()
				} else if step.Route != "" {
					fmt.Println("\n  Suggested Fix:")
					fmt.Println("  Check routing configuration and ensure proper route exists")
				}

				return
			}
		}
	}
}

// formatEndpoint formats an endpoint for display.
func formatEndpoint(endpoint *gcp.EndpointInfo, includePort bool) string {
	if endpoint == nil {
		return "N/A"
	}

	var parts []string

	// Add instance name if available
	if endpoint.Instance != "" {
		instanceName := extractResourceName(endpoint.Instance)
		parts = append(parts, instanceName)
	}

	// Add IP address
	if endpoint.IPAddress != "" {
		ipStr := endpoint.IPAddress
		if includePort && endpoint.Port > 0 {
			ipStr = fmt.Sprintf("%s:%d", ipStr, endpoint.Port)
		}

		if len(parts) > 0 {
			parts = append(parts, fmt.Sprintf("(%s)", ipStr))
		} else {
			parts = append(parts, ipStr)
		}
	} else if includePort && endpoint.Port > 0 {
		parts = append(parts, fmt.Sprintf("port %d", endpoint.Port))
	}

	if len(parts) == 0 {
		return "N/A"
	}

	return strings.Join(parts, " ")
}

// extractResourceName extracts the resource name from a full resource path.
func extractResourceName(resourcePath string) string {
	parts := strings.Split(resourcePath, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return resourcePath
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return s[:maxLen]
	}

	return s[:maxLen-3] + "..."
}
