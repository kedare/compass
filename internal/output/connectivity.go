// Package output provides formatting functions for terminal display
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"cx/internal/gcp"
)

// DisplayConnectivityTestResult formats and displays a connectivity test result
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

// DisplayConnectivityTestList formats and displays a list of connectivity tests
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

// displayText displays a connectivity test result in human-readable format
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
	if !isReachable {
		statusIcon = "✗"
	}

	fmt.Printf("%s Connectivity Test: %s\n", statusIcon, result.DisplayName)
	fmt.Printf("  Status:        %s\n", status)

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

	// Display path analysis
	if result.ReachabilityDetails != nil && len(result.ReachabilityDetails.Traces) > 0 {
		fmt.Println("\n  Path Analysis:")
		displayTraces(result.ReachabilityDetails.Traces, isReachable)
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

// displayDetailed displays a connectivity test with full details
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

// displayJSON displays result as JSON
func displayJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// displayListText displays a list of tests in text format
func displayListText(results []*gcp.ConnectivityTestResult) error {
	if len(results) == 0 {
		fmt.Println("No connectivity tests found")
		return nil
	}

	fmt.Printf("Found %d connectivity test(s):\n\n", len(results))
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

		fmt.Printf("%s %s\n", statusIcon, result.DisplayName)
		fmt.Printf("  Name:   %s\n", result.Name)
		fmt.Printf("  Status: %s\n", status)
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

// displayTable displays results in table format
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

// displayTraces displays network traces
func displayTraces(traces []*gcp.Trace, isReachable bool) {
	for _, trace := range traces {
		if len(trace.Steps) == 0 {
			continue
		}

		fmt.Print("  └─")
		for i, step := range trace.Steps {
			if i > 0 {
				fmt.Print(" → ")
			}

			// Format step description
			desc := formatTraceStep(step)
			fmt.Print(desc)

			// Show failure indicator
			if step.CausesDrop {
				fmt.Print(" ✗")
			}
		}
		fmt.Println()
	}
}

// formatTraceStep formats a trace step for display
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

// displaySuggestedFixes displays suggested fixes for failed tests
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

// formatEndpoint formats an endpoint for display
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

// extractResourceName extracts the resource name from a full resource path
func extractResourceName(resourcePath string) string {
	parts := strings.Split(resourcePath, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return resourcePath
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
