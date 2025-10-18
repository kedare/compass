// Package output provides formatting functions for terminal display
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/kedare/compass/internal/gcp"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

func detectTerminalWidth() (int, bool) {
	if raw, ok := os.LookupEnv("COLUMNS"); ok {
		if width, err := strconv.Atoi(raw); err == nil && width > 0 {
			return width, true
		}
	}

	if width, ok := systemTerminalWidth(); ok {
		return width, true
	}

	return 0, false
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

type connectivityStatus struct {
	forwardStatus    string
	returnStatus     string
	overall          bool
	forwardReachable bool
	hasReturn        bool
	returnReachable  bool
}

func evaluateConnectivityStatus(result *gcp.ConnectivityTestResult) connectivityStatus {
	status := connectivityStatus{
		forwardStatus: normalizeStatusLabel(""),
	}

	if result == nil {
		return status
	}

	if result.ReachabilityDetails != nil {
		status.forwardStatus = normalizeStatusLabel(result.ReachabilityDetails.Result)
		status.forwardReachable = isStatusReachable(status.forwardStatus)
	} else {
		status.forwardStatus = normalizeStatusLabel("")
		status.forwardReachable = false
	}

	if result.ReturnReachabilityDetails != nil {
		status.hasReturn = true
		status.returnStatus = normalizeStatusLabel(result.ReturnReachabilityDetails.Result)
		status.returnReachable = isStatusReachable(status.returnStatus)
	}

	status.overall = status.forwardReachable && (!status.hasReturn || status.returnReachable)

	return status
}

func normalizeStatusLabel(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "UNKNOWN"
	}

	upper := strings.ToUpper(value)
	upper = strings.TrimPrefix(upper, "REACHABILITY_RESULT_")

	return upper
}

func isStatusReachable(status string) bool {
	if status == "" {
		return false
	}

	upper := strings.ToUpper(status)
	if strings.Contains(upper, "UNREACHABLE") ||
		strings.Contains(upper, "DROP") ||
		strings.Contains(upper, "BLOCK") ||
		strings.Contains(upper, "ERROR") ||
		strings.Contains(upper, "TIMEOUT") ||
		strings.Contains(upper, "AMBIGUOUS") {
		return false
	}

	return strings.Contains(upper, "REACHABLE")
}

func formatSingleStatus(status string, reachable bool, colorize bool) string {
	display := status
	if strings.TrimSpace(display) == "" {
		display = "UNKNOWN"
	}

	switch strings.ToUpper(display) {
	case "UNKNOWN", "N/A":
		if colorize {
			return text.Bold.Sprint(display)
		}

		return display
	}

	if !colorize {
		return display
	}

	if reachable {
		return text.Colors{text.Bold, text.FgGreen}.Sprint(display)
	}

	return text.Colors{text.Bold, text.FgRed}.Sprint(display)
}

func formatForwardStatus(status connectivityStatus, colorize bool) string {
	return formatSingleStatus(status.forwardStatus, status.forwardReachable, colorize)
}

func formatReturnStatus(status connectivityStatus, colorize bool) string {
	if !status.hasReturn {
		return formatSingleStatus("N/A", false, colorize)
	}

	return formatSingleStatus(status.returnStatus, status.returnReachable, colorize)
}

// displayText displays a connectivity test result in human-readable format.
func displayText(result *gcp.ConnectivityTestResult) error {
	statusInfo := evaluateConnectivityStatus(result)

	// Print header with status icon
	statusIcon := "✗"
	if statusInfo.overall {
		statusIcon = "✓"
	}

	fmt.Printf("%s Connectivity Test: %s\n", statusIcon, result.DisplayName)

	if result.Name != "" && result.ProjectID != "" {
		consoleURL := fmt.Sprintf("https://console.cloud.google.com/net-intelligence/connectivity/tests/details/%s?project=%s",
			result.Name, result.ProjectID)
		fmt.Printf("  Console URL:   %s\n", consoleURL)
	}

	fmt.Printf("  %-15s %s\n", "Forward Status:", formatForwardStatus(statusInfo, true))
	fmt.Printf("  %-15s %s\n", "Return Status:", formatReturnStatus(statusInfo, true))

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

		displayForwardAndReturnPaths(forwardTraces, returnTraces, statusInfo.overall)
	}

	// Display result message
	fmt.Println()

	if statusInfo.overall {
		fmt.Println("  Result: Connection successful ✓")
	} else {
		fmt.Println("  Result: Connection failed ✗")

		if result.ReachabilityDetails != nil && result.ReachabilityDetails.Error != "" {
			fmt.Printf("  Error:  %s\n", result.ReachabilityDetails.Error)
		}

		if statusInfo.hasReturn && result.ReturnReachabilityDetails != nil && result.ReturnReachabilityDetails.Error != "" {
			fmt.Printf("  Return Error: %s\n", result.ReturnReachabilityDetails.Error)
		}

		if statusInfo.forwardReachable && statusInfo.hasReturn && !statusInfo.returnReachable {
			fmt.Println("  Note: Forward path succeeded but return path failed.")
		}

		// Display suggested fixes if available
		displaySuggestedFixes(result, statusInfo)
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
		statusInfo := evaluateConnectivityStatus(result)
		statusIcon := "✗"

		if statusInfo.overall {
			statusIcon = "✓"
		}

		fmt.Printf("%s %s\n", statusIcon, result.DisplayName)
		fmt.Printf("  %-15s %s\n", "Forward Status:", formatForwardStatus(statusInfo, true))
		fmt.Printf("  %-15s %s\n", "Return Status:", formatReturnStatus(statusInfo, true))

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

	const (
		nameWidth    = 30
		forwardWidth = 25
		returnWidth  = 25
		sourceWidth  = 30
		destWidth    = 30
	)

	colorize := term.IsTerminal(int(os.Stdout.Fd()))

	// Print header
	fmt.Printf("%-3s %s %s %s %s %s\n",
		"ST",
		padRight("NAME", nameWidth),
		padRight("FORWARD STATUS", forwardWidth),
		padRight("RETURN STATUS", returnWidth),
		padRight("SOURCE", sourceWidth),
		padRight("DESTINATION", destWidth),
	)

	totalWidth := 3 + 1 + nameWidth + 1 + forwardWidth + 1 + returnWidth + 1 + sourceWidth + 1 + destWidth
	fmt.Println(strings.Repeat("-", totalWidth))

	// Print rows
	for _, result := range results {
		statusInfo := evaluateConnectivityStatus(result)
		statusIcon := "✗"

		if statusInfo.overall {
			statusIcon = "✓"
		}

		nameCell := padRight(truncate(result.DisplayName, nameWidth), nameWidth)

		forwardPlain := formatForwardStatus(statusInfo, false)

		forwardDisplay := forwardPlain
		if colorize {
			forwardDisplay = formatForwardStatus(statusInfo, true)
		}
		forwardCell := padRightWithPlain(forwardDisplay, forwardPlain, forwardWidth)

		returnPlain := formatReturnStatus(statusInfo, false)

		returnDisplay := returnPlain
		if colorize {
			returnDisplay = formatReturnStatus(statusInfo, true)
		}
		returnCell := padRightWithPlain(returnDisplay, returnPlain, returnWidth)

		sourceCell := padRight(truncate(formatEndpoint(result.Source, false), sourceWidth), sourceWidth)
		destCell := padRight(truncate(formatEndpoint(result.Destination, true), destWidth), destWidth)

		fmt.Printf("%-3s %s %s %s %s %s\n",
			statusIcon, nameCell, forwardCell, returnCell, sourceCell, destCell)
	}

	return nil
}

// displayForwardAndReturnPaths displays forward and return traces, pairing them when possible.
func displayForwardAndReturnPaths(forwardTraces []*gcp.Trace, returnTraces []*gcp.Trace, isReachable bool) {
	pairs := len(forwardTraces)
	if len(returnTraces) < pairs {
		pairs = len(returnTraces)
	}

	displayCombined := func(forward, backward *gcp.Trace, index int) bool {
		combined := renderCombinedTrace(forward, backward, index, pairs)
		if combined == "" {
			return false
		}

		if fitsTerminalWidth(combined) {
			fmt.Print(combined)

			return true
		}

		return false
	}

	for i := range pairs {
		if i > 0 {
			fmt.Println()
		}

		if displayCombined(forwardTraces[i], returnTraces[i], i) {
			continue
		}

		forwardTitle := "Forward Path"
		if pairs > 1 {
			forwardTitle = fmt.Sprintf("Forward Path %d of %d", i+1, pairs)
		}

		returnTitle := "Return Path"
		if len(returnTraces) > 1 {
			returnTitle = fmt.Sprintf("Return Path %d of %d", i+1, len(returnTraces))
		}

		displaySingleTrace(forwardTraces[i], forwardTitle)
		fmt.Println()
		displaySingleTrace(returnTraces[i], returnTitle)
	}

	for i := pairs; i < len(forwardTraces); i++ {
		fmt.Println()
		displaySingleTrace(forwardTraces[i], fmt.Sprintf("Forward Path %d of %d (no return path)", i+1, len(forwardTraces)))
	}

	for i := pairs; i < len(returnTraces); i++ {
		fmt.Println()
		displaySingleTrace(returnTraces[i], fmt.Sprintf("Return Path %d of %d (no forward path)", i+1, len(returnTraces)))
	}

	if pairs > 0 {
		return
	}

	if len(forwardTraces) > 0 {
		for i, trace := range forwardTraces {
			if i > 0 {
				fmt.Println()
			}

			title := "Forward Path"
			if len(forwardTraces) > 1 {
				title = fmt.Sprintf("Forward Path %d of %d", i+1, len(forwardTraces))
			}

			displaySingleTrace(trace, title)
		}

		return
	}

	for i, trace := range returnTraces {
		if i > 0 {
			fmt.Println()
		}

		title := "Return Path"
		if len(returnTraces) > 1 {
			title = fmt.Sprintf("Return Path %d of %d", i+1, len(returnTraces))
		}

		displaySingleTrace(trace, title)
	}
}

// displayForwardAndReturnSideBySide displays forward and return traces in two columns.

func renderCombinedTrace(forward, backward *gcp.Trace, index, total int) string {
	if forward == nil || backward == nil {
		return ""
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.Style().Options.SeparateRows = true
	t.SuppressEmptyColumns()

	t.AppendRow(table.Row{
		text.Bold.Sprint("Forward Path"), "", "", "", "",
		text.Bold.Sprint("Return Path"), "", "", "", "",
	}, table.RowConfig{AutoMerge: true})

	t.AppendRow(table.Row{"#", "Step", "Type", "Resource", "Status", "#", "Step", "Type", "Resource", "Status"})

	maxSteps := len(forward.Steps)
	if len(backward.Steps) > maxSteps {
		maxSteps = len(backward.Steps)
	}

	for i := range maxSteps {
		f := traceStepCells(forward, i)
		r := traceStepCells(backward, i)
		t.AppendRow(table.Row{f[0], f[1], f[2], f[3], f[4], r[0], r[1], r[2], r[3], r[4]})
	}

	lines := strings.Split(t.Render(), "\n")
	var builder strings.Builder

	if total > 1 {
		builder.WriteString(fmt.Sprintf("    Path %d of %d:\n", index+1, total))
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		builder.WriteString("    ")
		builder.WriteString(line)
		builder.WriteByte('\n')
	}

	return builder.String()
}

func traceStepCells(trace *gcp.Trace, index int) [5]string {
	if trace == nil || index >= len(trace.Steps) {
		return [5]string{"", "", "", "", ""}
	}

	step := trace.Steps[index]
	stepNum := strconv.Itoa(index + 1)
	stepType, resource, status := formatTraceStepForTable(step)

	if step.CausesDrop {
		stepNum = text.Bold.Sprint(text.FgRed.Sprint(stepNum))
		stepType = text.Bold.Sprint(text.FgRed.Sprint(stepType))
		resource = text.Bold.Sprint(text.FgRed.Sprint(resource))
		status = text.Bold.Sprint(text.FgRed.Sprint(status))
	} else {
		status = text.FgGreen.Sprint(status)
	}

	return [5]string{stepNum, getStepIcon(index, len(trace.Steps), step.CausesDrop), stepType, resource, status}
}

func fitsTerminalWidth(block string) bool {
	width, ok := detectTerminalWidth()
	if !ok {
		return true
	}

	maxWidth := 0

	for _, line := range strings.Split(block, "\n") {
		if line == "" {
			continue
		}

		if w := visibleWidth(line); w > maxWidth {
			maxWidth = w
		}
	}

	return maxWidth <= width
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
		stepNum := strconv.Itoa(i + 1)
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

// padRight pads a string to the right with spaces, accounting for ANSI color codes.

func visibleWidth(s string) int {
	return runewidth.StringWidth(stripAnsiCodes(s))
}

func padRight(s string, width int) string {
	current := visibleWidth(s)
	if current >= width {
		return s
	}

	return s + strings.Repeat(" ", width-current)
}

func padRightWithPlain(display, plain string, width int) string {
	plainWidth := runewidth.StringWidth(plain)
	if plainWidth >= width {
		return display
	}

	return display + strings.Repeat(" ", width-plainWidth)
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
func displaySuggestedFixes(result *gcp.ConnectivityTestResult, status connectivityStatus) {
	if result == nil || status.overall {
		return
	}

	if !status.forwardReachable {
		if displaySuggestedFixesForDetails(result, result.ReachabilityDetails, false) {
			return
		}
	}

	if status.hasReturn && !status.returnReachable {
		displaySuggestedFixesForDetails(result, result.ReturnReachabilityDetails, true)
	}
}

func displaySuggestedFixesForDetails(result *gcp.ConnectivityTestResult, details *gcp.ReachabilityDetails, reverse bool) bool {
	if details == nil || len(details.Traces) == 0 {
		return false
	}

	source := result.Source
	destination := result.Destination

	if reverse {
		source, destination = destination, source
	}

	// Find the step that caused the drop
	for _, trace := range details.Traces {
		for _, step := range trace.Steps {
			if step.CausesDrop {
				if step.Firewall != "" {
					fmt.Println("\n  Suggested Fix:")
					fmt.Printf("  Add firewall rule allowing %s traffic", result.Protocol)

					if source != nil && source.IPAddress != "" {
						fmt.Printf(" from %s", source.IPAddress)
					}

					if destination != nil {
						if destination.IPAddress != "" {
							fmt.Printf(" to %s", destination.IPAddress)
						}

						if destination.Port > 0 {
							fmt.Printf(":%d", destination.Port)
						}
					}

					fmt.Println()
				} else if step.Route != "" {
					fmt.Println("\n  Suggested Fix:")

					if reverse {
						fmt.Println("  Check return-path routing configuration and ensure proper route exists")
					} else {
						fmt.Println("  Check routing configuration and ensure proper route exists")
					}
				}

				return true
			}
		}
	}

	return false
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
