package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/rivo/tview"
)

// ipLookupEntry represents a row in the IP lookup results table
type ipLookupEntry struct {
	Kind         string
	Resource     string
	Project      string
	Location     string
	IPAddress    string
	Details      string
	ResourceLink string
}

// RunIPLookupView shows the IP lookup interface with progressive results
func RunIPLookupView(ctx context.Context, c *cache.Cache, app *tview.Application, parallelism int, onBack func()) error {
	var allResults []ipLookupEntry
	var resultsMu sync.Mutex
	var isSearching bool
	var searchCancel context.CancelFunc
	var modalOpen bool
	var currentFilter string
	var filterMode bool

	// Get projects from cache for lookup
	projects := c.GetProjectsByUsage()
	if len(projects) == 0 {
		return fmt.Errorf("no projects in cache")
	}

	// Create UI components
	ipInput := tview.NewInputField().
		SetLabel(" IP Address: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow).
		SetPlaceholder("Enter IP address and press Enter")

	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	table.SetBorder(true).SetTitle(" IP Lookup Results (0) ")

	// Add header
	headers := []string{"Kind", "Resource", "Project", "Location", "Details"}
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

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Enter[-] lookup  [yellow]Esc[-] back  [yellow]/[-] filter results  [yellow]d[-] details  [yellow]?[-] help")

	// Progress indicator
	progressText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignRight)

	// Layout - IP input at top, table in middle, progress/status at bottom
	statusFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(status, 0, 3, false).
		AddItem(progressText, 0, 1, false)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(ipInput, 1, 0, true).
		AddItem(table, 0, 1, false).
		AddItem(statusFlex, 1, 0, false)

	// Function to rebuild layout with or without filter
	rebuildLayout := func(showFilter bool, focusTable bool) {
		flex.Clear()
		flex.AddItem(ipInput, 1, 0, !showFilter && !focusTable)
		if showFilter {
			flex.AddItem(filterInput, 1, 0, true)
		}
		flex.AddItem(table, 0, 1, focusTable)
		flex.AddItem(statusFlex, 1, 0, false)
	}

	// Function to get kind color
	getKindColor := func(kind string) string {
		switch kind {
		case string(gcp.IPAssociationInstanceInternal), string(gcp.IPAssociationInstanceExternal):
			return "blue"
		case string(gcp.IPAssociationForwardingRule):
			return "cyan"
		case string(gcp.IPAssociationAddress):
			return "green"
		case string(gcp.IPAssociationSubnet):
			return "magenta"
		default:
			return "white"
		}
	}

	// Shared function to update table with given results
	var updateTableWithData = func(filter string, results []ipLookupEntry) {
		currentSelectedRow, _ := table.GetSelection()
		var selectedKey string
		if currentSelectedRow > 0 && currentSelectedRow < table.GetRowCount() {
			resourceCell := table.GetCell(currentSelectedRow, 1)
			projectCell := table.GetCell(currentSelectedRow, 2)
			locationCell := table.GetCell(currentSelectedRow, 3)
			if resourceCell != nil && projectCell != nil && locationCell != nil {
				selectedKey = resourceCell.Text + "|" + projectCell.Text + "|" + locationCell.Text
			}
		}

		for row := table.GetRowCount() - 1; row > 0; row-- {
			table.RemoveRow(row)
		}

		filterExpr := parseFilter(filter)
		currentRow := 1
		matchCount := 0
		newSelectedRow := -1

		for _, entry := range results {
			if !filterExpr.matches(entry.Resource, entry.Project, entry.Location, entry.Kind, entry.Details) {
				continue
			}

			kindColor := getKindColor(entry.Kind)
			// Truncate details for display
			displayDetails := entry.Details
			if len(displayDetails) > 50 {
				displayDetails = displayDetails[:47] + "..."
			}

			table.SetCell(currentRow, 0, tview.NewTableCell(fmt.Sprintf("[%s]%s[-]", kindColor, entry.Kind)).SetExpansion(1))
			table.SetCell(currentRow, 1, tview.NewTableCell(entry.Resource).SetExpansion(1))
			table.SetCell(currentRow, 2, tview.NewTableCell(entry.Project).SetExpansion(1))
			table.SetCell(currentRow, 3, tview.NewTableCell(entry.Location).SetExpansion(1))
			table.SetCell(currentRow, 4, tview.NewTableCell(displayDetails).SetExpansion(2))

			if selectedKey != "" && newSelectedRow == -1 {
				rowKey := entry.Resource + "|" + entry.Project + "|" + entry.Location
				if rowKey == selectedKey {
					newSelectedRow = currentRow
				}
			}

			currentRow++
			matchCount++
		}

		if filter != "" {
			table.SetTitle(fmt.Sprintf(" IP Lookup Results (%d/%d matched) ", matchCount, len(results)))
		} else {
			table.SetTitle(fmt.Sprintf(" IP Lookup Results (%d) ", len(results)))
		}

		if matchCount > 0 && table.GetRowCount() > 1 {
			if newSelectedRow > 0 {
				table.Select(newSelectedRow, 0)
			} else if currentSelectedRow > 0 && currentSelectedRow < table.GetRowCount() {
				table.Select(currentSelectedRow, 0)
			} else if currentSelectedRow >= table.GetRowCount() && table.GetRowCount() > 1 {
				table.Select(table.GetRowCount()-1, 0)
			} else if currentSelectedRow == 0 {
				table.Select(1, 0)
			}
		}
	}

	// Function to update table with current results (copies data to avoid holding lock)
	updateTable := func(filter string) {
		resultsMu.Lock()
		results := make([]ipLookupEntry, len(allResults))
		copy(results, allResults)
		resultsMu.Unlock()

		updateTableWithData(filter, results)
	}

	// Alias for updateTable - both copy data safely
	updateTableNoLock := updateTable

	// Helper function to get the currently selected entry
	getSelectedEntry := func() *ipLookupEntry {
		row, _ := table.GetSelection()
		if row <= 0 {
			return nil
		}

		resultsMu.Lock()
		defer resultsMu.Unlock()

		expr := parseFilter(currentFilter)
		visibleIdx := 0
		for i := range allResults {
			entry := &allResults[i]
			if !expr.matches(entry.Resource, entry.Project, entry.Location, entry.Kind, entry.Details) {
				continue
			}
			visibleIdx++
			if visibleIdx == row {
				return entry
			}
		}
		return nil
	}

	// Helper function to update status bar with context-aware actions
	updateStatusWithActions := func() {
		resultsMu.Lock()
		count := len(allResults)
		resultsMu.Unlock()

		entry := getSelectedEntry()

		// During search, show search-specific status
		if isSearching {
			status.SetText(" [yellow]Looking up IP...[-]  [yellow]Esc[-] cancel")
			return
		}

		if entry == nil {
			if count > 0 {
				status.SetText(fmt.Sprintf(" [green]%d results[-]  [yellow]Enter[-] lookup  [yellow]/[-] filter  [yellow]Esc[-] back  [yellow]?[-] help", count))
			} else {
				status.SetText(" [yellow]Enter[-] lookup  [yellow]/[-] filter  [yellow]Esc[-] back  [yellow]?[-] help")
			}
			return
		}

		actionStr := "[yellow]d[-] details  [yellow]b[-] browser"
		if currentFilter != "" {
			status.SetText(fmt.Sprintf(" [green]Filter: %s[-]  %s  [yellow]/[-] edit  [yellow]Esc[-] clear  [yellow]?[-] help", currentFilter, actionStr))
		} else {
			status.SetText(fmt.Sprintf(" %s  [yellow]Enter[-] lookup  [yellow]/[-] filter  [yellow]Esc[-] back  [yellow]?[-] help", actionStr))
		}
	}

	// Update status when selection changes
	table.SetSelectionChangedFunc(func(row, column int) {
		if !modalOpen && !filterMode {
			updateStatusWithActions()
		}
	})

	// Function to perform IP lookup across projects
	performLookup := func(ipAddress string) {
		if ipAddress == "" {
			return
		}

		// Cancel any existing lookup
		if searchCancel != nil {
			searchCancel()
		}

		// Clear previous results
		resultsMu.Lock()
		allResults = []ipLookupEntry{}
		resultsMu.Unlock()

		lookupCtx, cancel := context.WithCancel(ctx)
		searchCancel = cancel
		isSearching = true

		// Track progress
		var completedProjects int
		var totalProjects = len(projects)
		var progressMu sync.Mutex

		// Spinner animation
		spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinnerIdx := 0
		spinnerDone := make(chan bool, 2)

		// Start spinner goroutine
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					resultsMu.Lock()
					count := len(allResults)
					resultsMu.Unlock()
					progressMu.Lock()
					completed := completedProjects
					progressMu.Unlock()
					frame := spinnerFrames[spinnerIdx]
					spinnerIdx = (spinnerIdx + 1) % len(spinnerFrames)
					app.QueueUpdateDraw(func() {
						if isSearching {
							progressText.SetText(fmt.Sprintf("[yellow]%s %d/%d projects | %d results[-]", frame, completed, totalProjects, count))
							status.SetText(fmt.Sprintf(" [yellow]Looking up IP... (%d/%d projects)[-]  [yellow]Esc[-] cancel", completed, totalProjects))
						}
					})
				case <-spinnerDone:
					return
				case <-lookupCtx.Done():
					return
				}
			}
		}()

		// Update initial status
		app.QueueUpdateDraw(func() {
			updateStatusWithActions()
			progressText.SetText("[yellow]Starting IP lookup...[-]")
		})

		// Run lookup across all projects in parallel (limited concurrency)
		sem := make(chan struct{}, parallelism)
		var wg sync.WaitGroup

		for _, project := range projects {
			if lookupCtx.Err() != nil {
				break
			}

			wg.Add(1)
			go func(proj string) {
				defer wg.Done()

				sem <- struct{}{}
				defer func() { <-sem }()

				if lookupCtx.Err() != nil {
					return
				}

				client, err := gcp.NewClient(lookupCtx, proj)
				if err != nil {
					progressMu.Lock()
					completedProjects++
					progressMu.Unlock()
					return
				}

				associations, err := client.LookupIPAddress(lookupCtx, ipAddress)
				if err != nil {
					progressMu.Lock()
					completedProjects++
					progressMu.Unlock()
					return
				}

				if len(associations) > 0 {
					newEntries := make([]ipLookupEntry, 0, len(associations))
					for _, assoc := range associations {
						entry := ipLookupEntry{
							Kind:         string(assoc.Kind),
							Resource:     assoc.Resource,
							Project:      assoc.Project,
							Location:     assoc.Location,
							IPAddress:    assoc.IPAddress,
							Details:      assoc.Details,
							ResourceLink: assoc.ResourceLink,
						}
						newEntries = append(newEntries, entry)
					}

					resultsMu.Lock()
					allResults = append(allResults, newEntries...)
					resultsMu.Unlock()

					// Update UI
					filter := currentFilter
					app.QueueUpdateDraw(func() {
						updateTableNoLock(filter)
					})
				}

				progressMu.Lock()
				completedProjects++
				progressMu.Unlock()
			}(project)
		}

		// Wait for all lookups to complete
		wg.Wait()

		// Stop spinner
		select {
		case spinnerDone <- true:
		default:
		}

		isSearching = false

		// Sort results by project, then kind, then resource
		resultsMu.Lock()
		sort.Slice(allResults, func(i, j int) bool {
			if allResults[i].Project != allResults[j].Project {
				return allResults[i].Project < allResults[j].Project
			}
			if allResults[i].Kind != allResults[j].Kind {
				return allResults[i].Kind < allResults[j].Kind
			}
			return allResults[i].Resource < allResults[j].Resource
		})
		resultsMu.Unlock()

		// Final UI update
		filter := currentFilter
		app.QueueUpdateDraw(func() {
			progressText.SetText("")
			rebuildLayout(false, true)
			updateTableNoLock(filter)
			app.SetFocus(table)
			updateStatusWithActions()
		})
	}

	// IP input handler
	ipInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			ipAddress := strings.TrimSpace(ipInput.GetText())
			if ipAddress != "" {
				app.SetFocus(table)
				// Run lookup in goroutine to avoid blocking the event loop
				go performLookup(ipAddress)
			}
		case tcell.KeyEscape:
			if isSearching && searchCancel != nil {
				searchCancel()
			} else {
				onBack()
			}
		}
	})

	// Filter input handler
	filterInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			currentFilter = filterInput.GetText()
			updateTable(currentFilter)
			filterMode = false
			rebuildLayout(false, true)
			app.SetFocus(table)
			updateStatusWithActions()
		case tcell.KeyEscape:
			filterInput.SetText(currentFilter)
			filterMode = false
			rebuildLayout(false, true)
			app.SetFocus(table)
		}
	})

	// Setup keyboard handlers
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If a modal is open, let it handle all keys (except Ctrl+C)
		if modalOpen && event.Key() != tcell.KeyCtrlC {
			return event
		}

		// If in filter mode, let the input field handle it
		if filterMode {
			return event
		}

		// If IP input is focused, let it handle most keys
		if app.GetFocus() == ipInput {
			return event
		}

		switch event.Key() {
		case tcell.KeyEscape:
			if isSearching && searchCancel != nil {
				// Cancel ongoing lookup but stay in view
				searchCancel()
				return nil
			}
			if currentFilter != "" {
				// Clear filter
				currentFilter = ""
				filterInput.SetText("")
				updateTable("")
				status.SetText(" [yellow]Filter cleared[-]")
				time.AfterFunc(2*time.Second, func() {
					app.QueueUpdateDraw(func() {
						updateStatusWithActions()
					})
				})
				return nil
			}
			// Go back
			if searchCancel != nil {
				searchCancel()
			}
			onBack()
			return nil

		case tcell.KeyEnter:
			// Focus IP input
			app.SetFocus(ipInput)
			return nil

		case tcell.KeyRune:
			switch event.Rune() {
			case '/':
				// Enter filter mode
				filterMode = true
				filterInput.SetText(currentFilter)
				rebuildLayout(true, false)
				app.SetFocus(filterInput)
				status.SetText(" [yellow]Filter: spaces=AND  |=OR  -=NOT  (e.g. \"web|api -dev\")  Enter to apply, Esc to cancel[-]")
				return nil

			case 'd':
				// Show details for selected result
				selectedEntry := getSelectedEntry()
				if selectedEntry == nil {
					status.SetText(" [red]No result selected[-]")
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							updateStatusWithActions()
						})
					})
					return nil
				}

				showIPLookupDetail(app, table, flex, selectedEntry, &modalOpen, status, updateStatusWithActions)
				return nil

			case 'b':
				// Open in Cloud Console
				selectedEntry := getSelectedEntry()
				if selectedEntry == nil {
					return nil
				}

				url := getIPLookupConsoleURL(selectedEntry)
				if err := OpenInBrowser(url); err != nil {
					status.SetText(fmt.Sprintf(" [yellow]URL: %s[-]", url))
				} else {
					status.SetText(" [green]Opened in browser[-]")
				}
				time.AfterFunc(2*time.Second, func() {
					app.QueueUpdateDraw(func() {
						updateStatusWithActions()
					})
				})
				return nil

			case '?':
				// Show help
				showIPLookupHelp(app, table, flex, &modalOpen, status, updateStatusWithActions)
				return nil
			}
		}
		return event
	})

	app.SetRoot(flex, true).EnableMouse(true).SetFocus(ipInput)
	return nil
}

// showIPLookupDetail displays details for an IP lookup result
func showIPLookupDetail(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, entry *ipLookupEntry, modalOpen *bool, _ *tview.TextView, onRestoreStatus func()) {
	var detailText strings.Builder

	kindDisplay := entry.Kind
	switch entry.Kind {
	case string(gcp.IPAssociationInstanceInternal):
		kindDisplay = "Instance (Internal IP)"
	case string(gcp.IPAssociationInstanceExternal):
		kindDisplay = "Instance (External IP)"
	case string(gcp.IPAssociationForwardingRule):
		kindDisplay = "Forwarding Rule"
	case string(gcp.IPAssociationAddress):
		kindDisplay = "Reserved Address"
	case string(gcp.IPAssociationSubnet):
		kindDisplay = "Subnet Range"
	}

	detailText.WriteString(fmt.Sprintf("[yellow::b]%s[-:-:-]\n\n", kindDisplay))
	detailText.WriteString(fmt.Sprintf("[white::b]Resource:[-:-:-]   %s\n", entry.Resource))
	detailText.WriteString(fmt.Sprintf("[white::b]Project:[-:-:-]    %s\n", entry.Project))
	detailText.WriteString(fmt.Sprintf("[white::b]Location:[-:-:-]   %s\n", entry.Location))
	detailText.WriteString(fmt.Sprintf("[white::b]IP Address:[-:-:-] %s\n", entry.IPAddress))

	if entry.Details != "" {
		detailText.WriteString("\n[yellow::b]Details:[-:-:-]\n")
		// Parse key=value pairs from details
		pairs := strings.Split(entry.Details, ", ")
		for _, pair := range pairs {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				detailText.WriteString(fmt.Sprintf("  [white::b]%s:[-:-:-] %s\n", parts[0], parts[1]))
			} else {
				detailText.WriteString(fmt.Sprintf("  %s\n", pair))
			}
		}
	}

	if entry.ResourceLink != "" {
		detailText.WriteString(fmt.Sprintf("\n[white::b]Resource Link:[-:-:-]\n  %s\n", entry.ResourceLink))
	}

	detailText.WriteString("\n[darkgray]Press Esc to close[-]")

	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(detailText.String()).
		SetScrollable(true).
		SetWordWrap(true)
	detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", entry.Resource))

	// Create status bar for detail view
	detailStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Esc[-] back  [yellow]↑/↓[-] scroll")

	// Create fullscreen detail layout
	detailFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(detailView, 0, 1, true).
		AddItem(detailStatus, 1, 0, false)

	// Set up input handler
	detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			*modalOpen = false
			app.SetRoot(mainFlex, true)
			app.SetFocus(table)
			if onRestoreStatus != nil {
				onRestoreStatus()
			}
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(detailFlex, true).SetFocus(detailView)
}

// showIPLookupHelp displays help for the IP lookup view
func showIPLookupHelp(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, modalOpen *bool, _ *tview.TextView, onRestoreStatus func()) {
	helpText := `[yellow::b]IP Lookup View - Keyboard Shortcuts[-:-:-]

[yellow]Lookup[-]
  [white]Enter[-]         Start lookup / Focus IP input
  [white]Esc[-]           Cancel lookup (if running) / Clear filter / Go back

[yellow]Navigation[-]
  [white]↑/k[-]           Move selection up
  [white]↓/j[-]           Move selection down
  [white]Home/g[-]        Jump to first result
  [white]End/G[-]         Jump to last result

[yellow]Actions[-]
  [white]d[-]             Show details for selected result
  [white]b[-]             Open in Cloud Console (browser)
  [white]/[-]             Filter displayed results

[yellow]Resource Types[-]
  [blue]instance_internal[-]   VM internal IP
  [blue]instance_external[-]   VM external/NAT IP
  [cyan]forwarding_rule[-]     Load balancer IP
  [green]address[-]             Reserved IP address
  [magenta]subnet_range[-]        IP within subnet CIDR

[yellow]Features[-]
  • Searches across all cached projects
  • Finds instances, load balancers, reserved addresses, and subnet ranges
  • Private IPs also match subnet CIDR ranges

[darkgray]Press Esc or ? to close this help[-]`

	helpView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)
	helpView.SetBorder(true).
		SetTitle(" IP Lookup Help ").
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
			*modalOpen = false
			app.SetRoot(mainFlex, true)
			app.SetFocus(table)
			if onRestoreStatus != nil {
				onRestoreStatus()
			}
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(helpFlex, true).SetFocus(helpView)
}

// getIPLookupConsoleURL returns the Cloud Console URL for an IP lookup result
func getIPLookupConsoleURL(entry *ipLookupEntry) string {
	baseURL := "https://console.cloud.google.com"

	switch entry.Kind {
	case string(gcp.IPAssociationInstanceInternal), string(gcp.IPAssociationInstanceExternal):
		// Link to the instance
		return fmt.Sprintf("%s/compute/instancesDetail/zones/%s/instances/%s?project=%s",
			baseURL, entry.Location, entry.Resource, entry.Project)
	case string(gcp.IPAssociationForwardingRule):
		// Link to the forwarding rule (load balancing)
		return fmt.Sprintf("%s/net-services/loadbalancing/details/forwardingRules/%s/%s?project=%s",
			baseURL, entry.Location, entry.Resource, entry.Project)
	case string(gcp.IPAssociationAddress):
		// Link to IP addresses
		return fmt.Sprintf("%s/networking/addresses/list?project=%s", baseURL, entry.Project)
	case string(gcp.IPAssociationSubnet):
		// Link to VPC subnets
		return fmt.Sprintf("%s/networking/subnetworks/details/%s/%s?project=%s",
			baseURL, entry.Location, entry.Resource, entry.Project)
	default:
		return fmt.Sprintf("%s/home/dashboard?project=%s", baseURL, entry.Project)
	}
}
