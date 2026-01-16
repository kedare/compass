package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/output"
	"github.com/rivo/tview"
)

// connectivityEntry represents a row in the connectivity tests view
type connectivityEntry struct {
	Name        string
	DisplayName string
	Source      string
	Destination string
	Protocol    string
	Result      string // Result of the test (REACHABLE, UNREACHABLE, etc.)
	State       string // State of the test (ACTIVE, UPDATING, etc.)
	UpdateTime  time.Time

	// Reference to actual data structure for detailed view
	Test *gcp.ConnectivityTestResult
}

// RunConnectivityView shows the connectivity tests view
// It first prompts the user to select a project, then loads connectivity tests for that project
func RunConnectivityView(ctx context.Context, c *cache.Cache, app *tview.Application, onBack func()) error {
	// Show project selector first
	ShowProjectSelector(app, c, " Select Project for Connectivity Tests ", func(result ProjectSelectorResult) {
		if result.Cancelled {
			onBack()
			return
		}

		selectedProject := result.Project

		// Now run the actual connectivity view with the selected project
		runConnectivityViewInternal(ctx, selectedProject, app, onBack)
	})

	return nil
}

// runConnectivityViewInternal runs the connectivity view after a project has been selected
func runConnectivityViewInternal(ctx context.Context, selectedProject string, app *tview.Application, onBack func()) {
	// Show loading screen first
	loadingCtx, cancelLoading := context.WithCancel(ctx)

	updateMsg, spinnerDone := showLoadingScreen(app, fmt.Sprintf(" Loading Connectivity Tests [%s] ", selectedProject), "Initializing...", func() {
		cancelLoading()
		onBack()
	})

	// Load data in background
	go func() {
		var allEntries []connectivityEntry
		var connClient *gcp.ConnectivityClient
		var loadError error

		updateMsg("Creating connectivity client...")

		// Create connectivity client
		connClient, loadError = gcp.NewConnectivityClient(loadingCtx, selectedProject)
		if loadError != nil {
			// Signal spinner to stop
			select {
			case spinnerDone <- true:
			default:
			}

			if loadingCtx.Err() == context.Canceled {
				return
			}

			app.QueueUpdateDraw(func() {
				// Input capture is already cleared by project selector
				modal := tview.NewModal().
					SetText(fmt.Sprintf("Failed to create connectivity client for project '%s':\n\n%v", selectedProject, loadError)).
					AddButtons([]string{"OK"}).
					SetDoneFunc(func(buttonIndex int, buttonLabel string) {
						onBack()
					})
				app.SetRoot(modal, true).SetFocus(modal)
			})
			return
		}

		updateMsg("Loading connectivity tests...")

		tests, err := connClient.ListTests(loadingCtx, "")
		if err != nil {
			allEntries = []connectivityEntry{{
				Name:        "error",
				DisplayName: "Failed to load connectivity tests",
				Result:      err.Error(),
			}}
		} else {
			allEntries = buildConnectivityEntries(tests)
		}

		// Signal spinner to stop
		select {
		case spinnerDone <- true:
		default:
		}

		// Check if cancelled
		if loadingCtx.Err() == context.Canceled {
			return
		}

		app.QueueUpdateDraw(func() {
			// Now show the actual connectivity view
			showConnectivityViewUI(ctx, connClient, selectedProject, app, allEntries, onBack)
		})
	}()
}

// buildConnectivityEntries converts test results to display entries
func buildConnectivityEntries(tests []*gcp.ConnectivityTestResult) []connectivityEntry {
	var allEntries []connectivityEntry

	for _, test := range tests {
		entry := connectivityEntry{
			Name:        test.Name,
			DisplayName: test.DisplayName,
			Protocol:    test.Protocol,
			State:       formatTestState(test),
			UpdateTime:  test.UpdateTime,
			Test:        test,
		}

		// Format source
		if test.Source != nil {
			entry.Source = formatEndpoint(test.Source)
		}

		// Format destination
		if test.Destination != nil {
			entry.Destination = formatEndpoint(test.Destination)
		}

		// Format result
		if test.ReachabilityDetails != nil {
			entry.Result = formatTestResult(test.ReachabilityDetails.Result)
		} else {
			entry.Result = "[gray]PENDING[-]"
		}

		allEntries = append(allEntries, entry)
	}

	return allEntries
}

// showConnectivityViewUI displays the connectivity view UI after data is loaded
func showConnectivityViewUI(ctx context.Context, connClient *gcp.ConnectivityClient, selectedProject string, app *tview.Application, initialEntries []connectivityEntry, onBack func()) {
	var allEntries = initialEntries
	var modalOpen bool
	var filterMode bool
	var currentFilter string

	// Function to load connectivity tests (for refresh)
	loadTests := func() {
		tests, err := connClient.ListTests(ctx, "")
		if err != nil {
			allEntries = []connectivityEntry{{
				Name:        "error",
				DisplayName: "Failed to load connectivity tests",
				Result:      err.Error(),
			}}
			return
		}
		allEntries = buildConnectivityEntries(tests)
	}

	// Create table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	table.SetBorder(true).SetTitle(fmt.Sprintf(" Connectivity Tests [yellow][%s][-] (%d) ", selectedProject, len(allEntries)))

	// Add header
	headers := []string{"Name", "Source", "Destination", "Protocol", "Result"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorBlack).
			SetBackgroundColor(tcell.ColorDarkCyan).
			SetSelectable(false).
			SetExpansion(1)
		table.SetCell(0, col, cell)
	}

	// Filter input
	filterInput := tview.NewInputField().
		SetLabel(" Filter: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow)

	// Function to update table
	updateTable := func(filter string) {
		// Clear existing rows (keep header)
		for row := table.GetRowCount() - 1; row > 0; row-- {
			table.RemoveRow(row)
		}

		// Filter entries
		filterLower := strings.ToLower(filter)
		var filteredEntries []connectivityEntry
		if filter == "" {
			filteredEntries = allEntries
		} else {
			for _, entry := range allEntries {
				// Match against name, source, destination, protocol, result
				if strings.Contains(strings.ToLower(entry.DisplayName), filterLower) ||
					strings.Contains(strings.ToLower(entry.Name), filterLower) ||
					strings.Contains(strings.ToLower(entry.Source), filterLower) ||
					strings.Contains(strings.ToLower(entry.Destination), filterLower) ||
					strings.Contains(strings.ToLower(entry.Protocol), filterLower) ||
					strings.Contains(strings.ToLower(entry.Result), filterLower) {
					filteredEntries = append(filteredEntries, entry)
				}
			}
		}

		currentRow := 1
		for _, entry := range filteredEntries {
			table.SetCell(currentRow, 0, tview.NewTableCell(entry.DisplayName).SetExpansion(1))
			table.SetCell(currentRow, 1, tview.NewTableCell(entry.Source).SetExpansion(1))
			table.SetCell(currentRow, 2, tview.NewTableCell(entry.Destination).SetExpansion(1))
			table.SetCell(currentRow, 3, tview.NewTableCell(entry.Protocol).SetExpansion(1))
			table.SetCell(currentRow, 4, tview.NewTableCell(entry.Result).SetExpansion(1))
			currentRow++
		}

		// Update title
		titleSuffix := ""
		if filter != "" {
			titleSuffix = fmt.Sprintf(" [filtered: %d/%d]", len(filteredEntries), len(allEntries))
		}
		table.SetTitle(fmt.Sprintf(" Connectivity Tests [yellow][%s][-] (%d)%s ", selectedProject, len(allEntries), titleSuffix))

		// Select first data row if available
		if len(filteredEntries) > 0 {
			table.Select(1, 0)
		}
	}

	// Initial table population
	updateTable("")

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")

	// Create pages for modal overlays
	pages := tview.NewPages()

	// Layout
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true).
		AddItem(status, 1, 0, false)

	// Setup filter input handlers
	filterInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			// Apply filter and exit filter mode
			currentFilter = filterInput.GetText()
			updateTable(currentFilter)
			filterMode = false
			flex.RemoveItem(filterInput)
			flex.Clear()
			flex.AddItem(table, 0, 1, true)
			flex.AddItem(status, 1, 0, false)
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
			} else {
				status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
			}
		case tcell.KeyEscape:
			// Cancel filter mode without applying
			filterInput.SetText(currentFilter)
			filterMode = false
			flex.RemoveItem(filterInput)
			flex.Clear()
			flex.AddItem(table, 0, 1, true)
			flex.AddItem(status, 1, 0, false)
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
			} else {
				status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
			}
		}
	})

	pages.AddPage("main", flex, true, true)

	// Function to show test details fullscreen
	showTestDetails := func(entry *connectivityEntry) {
		if entry.Test == nil {
			return
		}

		test := entry.Test
		details := fmt.Sprintf(`[yellow::b]Test Details[-:-:-]

[white::b]Name:[-:-:-] %s
[white::b]Display Name:[-:-:-] %s
[white::b]Description:[-:-:-] %s
[white::b]Protocol:[-:-:-] %s
[white::b]State:[-:-:-] %s

[yellow::b]Source Endpoint:[-:-:-]
%s

[yellow::b]Destination Endpoint:[-:-:-]
%s

[yellow::b]Reachability:[-:-:-]
%s

[white::b]Created:[-:-:-] %s
[white::b]Updated:[-:-:-] %s

[darkgray]Press Esc to go back[-]`,
			test.Name,
			test.DisplayName,
			test.Description,
			test.Protocol,
			formatTestState(test),
			formatEndpointDetails(test.Source),
			formatEndpointDetails(test.Destination),
			formatReachabilityDetails(test.ReachabilityDetails),
			test.CreateTime.Format("2006-01-02 15:04:05"),
			test.UpdateTime.Format("2006-01-02 15:04:05"),
		)

		detailView := tview.NewTextView().
			SetDynamicColors(true).
			SetText(details).
			SetScrollable(true).
			SetWordWrap(true)
		detailView.SetBorder(true).SetTitle(fmt.Sprintf(" Test: %s ", test.DisplayName))

		// Create status bar for detail view
		detailStatus := tview.NewTextView().
			SetDynamicColors(true).
			SetText(" [yellow]Esc[-] back  [yellow]up/down[-] scroll")

		// Create fullscreen detail layout
		detailFlex := tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(detailView, 0, 1, true).
			AddItem(detailStatus, 1, 0, false)

		// Set up input handler
		detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyEscape {
				app.SetRoot(flex, true)
				app.SetFocus(table)
				modalOpen = false
				if currentFilter != "" {
					status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
				} else {
					status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
				}
				return nil
			}
			return event
		})

		modalOpen = true
		app.SetRoot(detailFlex, true).SetFocus(detailView)
	}

	// Function to show help fullscreen
	showHelp := func() {
		helpText := `[yellow::b]Connectivity Tests - Keyboard Shortcuts[-:-:-]

[yellow]Navigation:[-]
  up/down, j/k     Navigate list

[yellow]Test Actions:[-]
  d            Show test details
  r            Rerun selected test
  Del          Delete selected test
  /            Enter filter/search mode
  Shift+R      Refresh test list

[yellow]General:[-]
  ?            Show this help
  Esc          Clear filter (if active) or go back to instance view
  Ctrl+C, q    Quit application

[darkgray]Press Esc or ? to go back[-]`

		helpView := tview.NewTextView().
			SetDynamicColors(true).
			SetText(helpText).
			SetScrollable(true)
		helpView.SetBorder(true).
			SetTitle(" Connectivity Tests Help ").
			SetTitleAlign(tview.AlignCenter)

		// Create status bar for help view
		helpStatus := tview.NewTextView().
			SetDynamicColors(true).
			SetText(" [yellow]Esc[-] back  [yellow]?[-] close help")

		// Create fullscreen help layout
		helpFlex := tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(helpView, 0, 1, true).
			AddItem(helpStatus, 1, 0, false)

		// Set up input handler
		helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == '?') {
				app.SetRoot(flex, true)
				app.SetFocus(table)
				modalOpen = false
				if currentFilter != "" {
					status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
				} else {
					status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
				}
				return nil
			}
			return event
		})

		modalOpen = true
		app.SetRoot(helpFlex, true).SetFocus(helpView)
	}

	// Function to rerun test
	rerunTest := func(entry *connectivityEntry) {
		if entry.Test == nil {
			return
		}

		status.SetText(fmt.Sprintf(" [yellow]Rerunning test '%s'...[-]", entry.DisplayName))

		go func() {
			result, err := connClient.RunTest(ctx, entry.Name)
			app.QueueUpdateDraw(func() {
				if err != nil {
					status.SetText(fmt.Sprintf(" [red]Error rerunning test: %v[-]", err))
				} else {
					status.SetText(fmt.Sprintf(" [green]Test '%s' completed: %s[-]", entry.DisplayName, formatTestResult(result.ReachabilityDetails.Result)))
					// Reload tests
					loadTests()
					updateTable(currentFilter)
				}
				// Reset status after 3 seconds
				time.AfterFunc(3*time.Second, func() {
					app.QueueUpdateDraw(func() {
						if currentFilter != "" {
							status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
						} else {
							status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
						}
					})
				})
			})
		}()
	}

	// Function to delete test
	deleteTest := func(entry *connectivityEntry) {
		if entry.Test == nil {
			return
		}

		// Show confirmation modal
		confirmModal := tview.NewModal().
			SetText(fmt.Sprintf("Delete connectivity test '%s'?\n\nThis action cannot be undone.", entry.DisplayName)).
			AddButtons([]string{"Delete", "Cancel"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				pages.HidePage("confirm")
				modalOpen = false
				app.SetFocus(table)

				if buttonIndex == 0 { // Delete
					status.SetText(fmt.Sprintf(" [yellow]Deleting test '%s'...[-]", entry.DisplayName))

					go func() {
						err := connClient.DeleteTest(ctx, entry.Name)
						app.QueueUpdateDraw(func() {
							if err != nil {
								status.SetText(fmt.Sprintf(" [red]Error deleting test: %v[-]", err))
							} else {
								status.SetText(fmt.Sprintf(" [green]Test '%s' deleted[-]", entry.DisplayName))
								// Reload tests
								loadTests()
								updateTable(currentFilter)
							}
							// Reset status after 3 seconds
							time.AfterFunc(3*time.Second, func() {
								app.QueueUpdateDraw(func() {
									if currentFilter != "" {
										status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
									} else {
										status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
									}
								})
							})
						})
					}()
				}
			})

		pages.AddPage("confirm", confirmModal, true, true)
		modalOpen = true
		app.SetFocus(confirmModal)
	}

	// Keyboard handler
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If in filter mode, let filter input handle it
		if filterMode {
			return event
		}

		// If modal is open, let modal handle input
		if modalOpen {
			return event
		}

		switch event.Key() {
		case tcell.KeyEscape:
			// Clear filter if active, otherwise go back
			if currentFilter != "" {
				currentFilter = ""
				filterInput.SetText("")
				updateTable("")
				status.SetText(" [yellow]Filter cleared[-]")
				time.AfterFunc(2*time.Second, func() {
					app.QueueUpdateDraw(func() {
						status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
					})
				})
				return nil
			}
			// Go back to instance view
			onBack()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'R':
				// Refresh with loading screen
				refreshCtx, cancelRefresh := context.WithCancel(ctx)

				_, spinnerDone := showLoadingScreen(app, " Refreshing Connectivity Tests ", "Loading connectivity tests...", func() {
					cancelRefresh()
					app.SetRoot(flex, true).SetFocus(table)
					status.SetText(" [yellow]Refresh cancelled[-]")
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							if currentFilter != "" {
								status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
							} else {
								status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
							}
						})
					})
				})

				go func() {
					loadTests()
					app.QueueUpdateDraw(func() {
						select {
						case spinnerDone <- true:
						default:
						}
						if refreshCtx.Err() == context.Canceled {
							return
						}
						app.SetRoot(flex, true).SetFocus(table)
						updateTable(currentFilter)
						status.SetText(" [green]Refreshed![-]")
						time.AfterFunc(2*time.Second, func() {
							app.QueueUpdateDraw(func() {
								if currentFilter != "" {
									status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
								} else {
									status.SetText(" [yellow]d[-] details  [yellow]r[-] rerun  [yellow]Del[-] delete  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]Esc[-] back  [yellow]?[-] help")
								}
							})
						})
					})
				}()
				return nil
			case '/':
				// Enter filter mode
				filterMode = true
				filterInput.SetText(currentFilter)
				flex.Clear()
				flex.AddItem(filterInput, 1, 0, true)
				flex.AddItem(table, 0, 1, false)
				flex.AddItem(status, 1, 0, false)
				app.SetFocus(filterInput)
				return nil
			case 'd':
				// Show details
				row, _ := table.GetSelection()
				if row > 0 && row <= len(allEntries) {
					entry := allEntries[row-1]
					showTestDetails(&entry)
				}
				return nil
			case 'r':
				// Rerun test
				row, _ := table.GetSelection()
				if row > 0 && row <= len(allEntries) {
					entry := allEntries[row-1]
					rerunTest(&entry)
				}
				return nil
			case '?':
				// Show help
				showHelp()
				return nil
			}
		case tcell.KeyDelete:
			// Delete test
			row, _ := table.GetSelection()
			if row > 0 && row <= len(allEntries) {
				entry := allEntries[row-1]
				deleteTest(&entry)
			}
			return nil
		}

		return event
	})

	// Set up the application
	app.SetRoot(pages, true).SetFocus(table)
}

// formatEndpoint formats an endpoint for display
func formatEndpoint(endpoint *gcp.EndpointInfo) string {
	if endpoint == nil {
		return ""
	}

	parts := []string{}
	if endpoint.Instance != "" {
		// Extract instance name from full path
		parts := strings.Split(endpoint.Instance, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	if endpoint.IPAddress != "" {
		parts = append(parts, endpoint.IPAddress)
	}
	if len(parts) == 0 {
		return "[gray]N/A[-]"
	}
	return strings.Join(parts, " ")
}

// formatEndpointDetails formats endpoint details for the detail modal
func formatEndpointDetails(endpoint *gcp.EndpointInfo) string {
	if endpoint == nil {
		return "  [gray]Not specified[-]"
	}

	details := []string{}
	if endpoint.Instance != "" {
		details = append(details, fmt.Sprintf("  Instance: %s", endpoint.Instance))
	}
	if endpoint.IPAddress != "" {
		details = append(details, fmt.Sprintf("  IP Address: %s", endpoint.IPAddress))
	}
	if endpoint.Network != "" {
		details = append(details, fmt.Sprintf("  Network: %s", endpoint.Network))
	}
	if endpoint.ProjectID != "" {
		details = append(details, fmt.Sprintf("  Project: %s", endpoint.ProjectID))
	}
	if endpoint.Port > 0 {
		details = append(details, fmt.Sprintf("  Port: %d", endpoint.Port))
	}

	if len(details) == 0 {
		return "  [gray]Not specified[-]"
	}

	return strings.Join(details, "\n")
}

// formatTestResult formats the test result with color coding
func formatTestResult(result string) string {
	switch result {
	case "REACHABLE":
		return "[green]REACHABLE[-]"
	case "UNREACHABLE":
		return "[red]UNREACHABLE[-]"
	case "AMBIGUOUS":
		return "[yellow]AMBIGUOUS[-]"
	default:
		return fmt.Sprintf("[gray]%s[-]", result)
	}
}

// formatTestState formats the test state
func formatTestState(test *gcp.ConnectivityTestResult) string {
	if test.ReachabilityDetails != nil {
		return formatTestResult(test.ReachabilityDetails.Result)
	}
	return "[gray]PENDING[-]"
}

// convertAnsiToTview converts ANSI escape codes to tview color tags
func convertAnsiToTview(s string) string {
	var result strings.Builder
	i := 0

	for i < len(s) {
		// Look for ANSI escape sequence
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2 // skip '\x1b['

			// Parse the color code
			var code strings.Builder
			for i < len(s) && s[i] != 'm' {
				code.WriteByte(s[i])
				i++
			}
			i++ // skip 'm'

			// Convert ANSI code to tview tag
			codeStr := code.String()
			switch codeStr {
			case "0": // Reset
				result.WriteString("[-]")
			case "1": // Bold
				result.WriteString("[::b]")
			case "31": // Red
				result.WriteString("[red]")
			case "32": // Green
				result.WriteString("[green]")
			case "33": // Yellow
				result.WriteString("[yellow]")
			case "34": // Blue
				result.WriteString("[blue]")
			case "35": // Magenta
				result.WriteString("[magenta]")
			case "36": // Cyan
				result.WriteString("[cyan]")
			case "37": // White
				result.WriteString("[white]")
			case "90": // Bright black (gray)
				result.WriteString("[gray]")
			case "91": // Bright red
				result.WriteString("[red]")
			case "92": // Bright green
				result.WriteString("[green]")
			case "93": // Bright yellow
				result.WriteString("[yellow]")
			case "94": // Bright blue
				result.WriteString("[blue]")
			case "95": // Bright magenta
				result.WriteString("[magenta]")
			case "96": // Bright cyan
				result.WriteString("[cyan]")
			case "97": // Bright white
				result.WriteString("[white]")
			case "1;31": // Bold red
				result.WriteString("[red::b]")
			case "1;32": // Bold green
				result.WriteString("[green::b]")
			case "1;33": // Bold yellow
				result.WriteString("[yellow::b]")
			case "1;36": // Bold cyan
				result.WriteString("[cyan::b]")
			default:
				// Unknown code, ignore
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}

	return result.String()
}

// formatReachabilityDetails formats reachability details for display
func formatReachabilityDetails(details *gcp.ReachabilityDetails) string {
	if details == nil {
		return "  [gray]No results available[-]"
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("  [white::b]Result:[-:-:-] %s\n", formatTestResult(details.Result)))
	if !details.VerifyTime.IsZero() {
		result.WriteString(fmt.Sprintf("  [white::b]Verified:[-:-:-] %s\n", details.VerifyTime.Format("2006-01-02 15:04:05")))
	}
	if details.Error != "" {
		result.WriteString(fmt.Sprintf("  [white::b]Error:[-:-:-] [red]%s[-]\n", details.Error))
	}

	// Add detailed trace information using the reusable output package function
	if len(details.Traces) > 0 {
		result.WriteString(fmt.Sprintf("\n  [yellow::b]Network Traces (%d):[-:-:-]\n\n", len(details.Traces)))
		for i, trace := range details.Traces {
			// Use the output package's FormatSingleTrace function for consistent rendering
			traceOutput := output.FormatSingleTrace(trace, fmt.Sprintf("Trace #%d", i+1))
			// Convert ANSI codes to tview format
			traceOutput = convertAnsiToTview(traceOutput)
			result.WriteString(traceOutput)
			if i < len(details.Traces)-1 {
				result.WriteString("\n")
			}
		}
	}

	return result.String()
}
