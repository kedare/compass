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
	"github.com/kedare/compass/internal/gcp/search"
	"github.com/rivo/tview"
)

// searchEntry represents a row in the search results table
type searchEntry struct {
	Type     string
	Name     string
	Project  string
	Location string
	Details  map[string]string
	Result   *search.Result
}

// RunSearchView shows the search interface with progressive results
func RunSearchView(ctx context.Context, c *cache.Cache, app *tview.Application, onBack func()) error {
	var allResults []searchEntry
	var allWarnings []search.SearchWarning
	var searchError error
	var resultsMu sync.Mutex
	var isSearching bool
	var searchCancel context.CancelFunc
	var modalOpen bool
	var currentFilter string
	var filterMode bool

	// Get projects from cache
	projects := c.GetProjects()
	if len(projects) == 0 {
		return fmt.Errorf("no projects in cache")
	}

	// Create search engine with all providers
	engine := createSearchEngine()

	// Create UI components
	searchInput := tview.NewInputField().
		SetLabel(" Search: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow).
		SetPlaceholder("Enter search term and press Enter")

	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	table.SetBorder(true).SetTitle(" Search Results (0) ")

	// Add header
	headers := []string{"Type", "Name", "Project", "Location"}
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
		SetText(" [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter results  [yellow]d[-] details  [yellow]?[-] help")

	// Progress indicator
	progressText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignRight)

	// Errors/Warnings pane (initially hidden)
	warningsPane := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	warningsPane.SetBorder(true).
		SetTitle(" Errors/Warnings ").
		SetBorderColor(tcell.ColorYellow)

	// Layout - search input at top, table in middle, progress/status at bottom
	statusFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(status, 0, 3, false).
		AddItem(progressText, 0, 1, false)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(searchInput, 1, 0, true).
		AddItem(table, 0, 1, false).
		AddItem(statusFlex, 1, 0, false)

	// Function to rebuild layout with or without warnings pane
	rebuildLayout := func(showFilter bool, focusTable bool) {
		flex.Clear()
		flex.AddItem(searchInput, 1, 0, !showFilter && !focusTable)
		if showFilter {
			flex.AddItem(filterInput, 1, 0, true)
		}
		flex.AddItem(table, 0, 1, focusTable)

		// Show warnings pane if there are warnings or errors
		hasIssues := len(allWarnings) > 0 || searchError != nil
		if hasIssues {
			// Calculate height based on number of issues (min 3, max 8 lines)
			height := len(allWarnings) + 2 // +2 for border
			if searchError != nil {
				height++
			}
			if height < 3 {
				height = 3
			}
			if height > 8 {
				height = 8
			}
			flex.AddItem(warningsPane, height, 0, false)
		}

		flex.AddItem(statusFlex, 1, 0, false)
	}

	// Function to update the warnings pane content
	updateWarningsPane := func() {
		var content strings.Builder

		if searchError != nil {
			content.WriteString(fmt.Sprintf("[red]Error:[-] %v\n", searchError))
		}

		if len(allWarnings) > 0 {
			// Group warnings by provider
			providerErrors := make(map[search.ResourceKind][]string)
			for _, w := range allWarnings {
				providerErrors[w.Provider] = append(providerErrors[w.Provider], w.Project)
			}

			for provider, projects := range providerErrors {
				if len(projects) == 1 {
					content.WriteString(fmt.Sprintf("[yellow]%s:[-] failed for project %s\n", provider, projects[0]))
				} else {
					content.WriteString(fmt.Sprintf("[yellow]%s:[-] failed for %d projects (%s...)\n", provider, len(projects), projects[0]))
				}
			}
		}

		warningsPane.SetText(content.String())
	}

	// Shared function to update table with given results
	var updateTableWithData func(filter string, results []searchEntry)
	updateTableWithData = func(filter string, results []searchEntry) {
		// Clear existing rows (keep header)
		for row := table.GetRowCount() - 1; row > 0; row-- {
			table.RemoveRow(row)
		}

		filterLower := strings.ToLower(filter)
		currentRow := 1
		matchCount := 0

		for _, entry := range results {
			// Apply filter if active
			if filter != "" {
				if !strings.Contains(strings.ToLower(entry.Name), filterLower) &&
					!strings.Contains(strings.ToLower(entry.Project), filterLower) &&
					!strings.Contains(strings.ToLower(entry.Location), filterLower) &&
					!strings.Contains(strings.ToLower(entry.Type), filterLower) {
					continue
				}
			}

			typeColor := getTypeColor(entry.Type)
			table.SetCell(currentRow, 0, tview.NewTableCell(fmt.Sprintf("[%s]%s[-]", typeColor, entry.Type)).SetExpansion(1))
			table.SetCell(currentRow, 1, tview.NewTableCell(entry.Name).SetExpansion(1))
			table.SetCell(currentRow, 2, tview.NewTableCell(entry.Project).SetExpansion(1))
			table.SetCell(currentRow, 3, tview.NewTableCell(entry.Location).SetExpansion(1))
			currentRow++
			matchCount++
		}

		// Update title
		if filter != "" {
			table.SetTitle(fmt.Sprintf(" Search Results (%d/%d matched) ", matchCount, len(results)))
		} else {
			table.SetTitle(fmt.Sprintf(" Search Results (%d) ", len(results)))
		}

		// Select first data row if available
		if matchCount > 0 && table.GetRowCount() > 1 {
			table.Select(1, 0)
		}
	}

	// Function to update table with current results (copies data to avoid holding lock)
	updateTable := func(filter string) {
		resultsMu.Lock()
		results := make([]searchEntry, len(allResults))
		copy(results, allResults)
		resultsMu.Unlock()

		updateTableWithData(filter, results)
	}

	// Alias for updateTable - both copy data safely
	updateTableNoLock := updateTable

	// Function to perform search with streaming results
	// This function should be called from a goroutine to avoid blocking the event loop
	performSearch := func(query string) {
		if query == "" {
			return
		}

		// Cancel any existing search
		if searchCancel != nil {
			searchCancel()
		}

		// Clear previous results and warnings
		resultsMu.Lock()
		allResults = []searchEntry{}
		allWarnings = nil
		searchError = nil
		resultsMu.Unlock()

		searchCtx, cancel := context.WithCancel(ctx)
		searchCancel = cancel
		isSearching = true

		// Clear warnings pane at start
		app.QueueUpdateDraw(func() {
			warningsPane.SetText("")
			rebuildLayout(false, true)
		})

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
					frame := spinnerFrames[spinnerIdx]
					spinnerIdx = (spinnerIdx + 1) % len(spinnerFrames)
					app.QueueUpdateDraw(func() {
						if isSearching {
							progressText.SetText(fmt.Sprintf("[yellow]%s %d results[-]", frame, count))
						}
					})
				case <-spinnerDone:
					return
				case <-searchCtx.Done():
					return
				}
			}
		}()

		// Update initial status
		app.QueueUpdateDraw(func() {
			status.SetText(" [yellow]Searching...[-]  [yellow]Esc[-] cancel search  [yellow]d[-] details")
			progressText.SetText(fmt.Sprintf("[yellow]Searching %d projects...[-]", len(projects)))
		})

		// Run search
		searchQuery := search.Query{Term: query}

		callback := func(results []search.Result, progress string) error {
			// Check if cancelled
			if searchCtx.Err() != nil {
				return searchCtx.Err()
			}

			// Add new results
			newEntries := make([]searchEntry, 0, len(results))
			for _, r := range results {
				entry := searchEntry{
					Type:     string(r.Type),
					Name:     r.Name,
					Project:  r.Project,
					Location: r.Location,
					Details:  r.Details,
					Result:   &r,
				}
				newEntries = append(newEntries, entry)
			}

			resultsMu.Lock()
			allResults = append(allResults, newEntries...)
			resultsMu.Unlock()

			// Update UI - copy current filter to avoid race
			filter := currentFilter
			app.QueueUpdateDraw(func() {
				updateTableNoLock(filter)
			})

			return nil
		}

		output, err := engine.SearchStreaming(searchCtx, projects, searchQuery, callback)

		// Stop spinner
		select {
		case spinnerDone <- true:
		default:
		}

		isSearching = false

		// Store warnings and error
		resultsMu.Lock()
		count := len(allResults)
		if err != nil {
			searchError = err
		}
		if output != nil && len(output.Warnings) > 0 {
			allWarnings = output.Warnings
		}
		resultsMu.Unlock()

		// Final UI update
		filter := currentFilter
		app.QueueUpdateDraw(func() {
			progressText.SetText("")

			if searchCtx.Err() == context.Canceled {
				status.SetText(fmt.Sprintf(" [yellow]Search cancelled[-] (%d results)  [yellow]Enter[-] new search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details", count))
			} else if err != nil {
				status.SetText(fmt.Sprintf(" [green]Found %d results[-]  [yellow]Enter[-] new search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details", count))
			} else {
				warningMsg := ""
				if len(allWarnings) > 0 {
					warningMsg = fmt.Sprintf(" [yellow](%d warnings)[-]", len(allWarnings))
				}
				status.SetText(fmt.Sprintf(" [green]Found %d results[-]%s  [yellow]Enter[-] new search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details", count, warningMsg))
			}

			// Update warnings pane and rebuild layout to show/hide it
			updateWarningsPane()
			rebuildLayout(false, true)
			updateTableNoLock(filter)
			app.SetFocus(table)
		})
	}

	// Search input handler
	searchInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			query := strings.TrimSpace(searchInput.GetText())
			if query != "" {
				app.SetFocus(table)
				// Run search in goroutine to avoid blocking the event loop
				go performSearch(query)
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
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter: %s[-]  [yellow]Enter[-] search  [yellow]Esc[-] clear filter  [yellow]/[-] edit  [yellow]d[-] details", currentFilter))
			}
		case tcell.KeyEscape:
			filterInput.SetText(currentFilter)
			filterMode = false
			rebuildLayout(false, true)
			app.SetFocus(table)
		}
	})

	// Setup keyboard handlers
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If in filter mode, let the input field handle it
		if filterMode {
			return event
		}

		// If search input is focused, let it handle most keys
		if app.GetFocus() == searchInput {
			return event
		}

		// Don't handle ESC if a modal is open
		if modalOpen && event.Key() == tcell.KeyEscape {
			return event
		}

		switch event.Key() {
		case tcell.KeyEscape:
			if isSearching && searchCancel != nil {
				// Cancel ongoing search but stay in view
				searchCancel()
				return nil
			}
			if currentFilter != "" {
				// Clear filter
				currentFilter = ""
				filterInput.SetText("")
				updateTable("")
				status.SetText(" [yellow]Filter cleared[-]  [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details")
				time.AfterFunc(2*time.Second, func() {
					app.QueueUpdateDraw(func() {
						resultsMu.Lock()
						count := len(allResults)
						resultsMu.Unlock()
						if count > 0 {
							status.SetText(fmt.Sprintf(" [green]%d results[-]  [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details", count))
						} else {
							status.SetText(" [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter results  [yellow]d[-] details  [yellow]?[-] help")
						}
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
			// Focus search input
			app.SetFocus(searchInput)
			return nil

		case tcell.KeyRune:
			switch event.Rune() {
			case '/':
				// Enter filter mode
				filterMode = true
				filterInput.SetText(currentFilter)
				rebuildLayout(true, false)
				app.SetFocus(filterInput)
				status.SetText(" [yellow]Type to filter results, Enter to apply, Esc to cancel[-]")
				return nil

			case 'd':
				// Show details for selected result
				row, _ := table.GetSelection()
				if row <= 0 {
					status.SetText(" [red]No result selected[-]")
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							status.SetText(" [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details  [yellow]?[-] help")
						})
					})
					return nil
				}

				resultsMu.Lock()
				// Find the actual entry (accounting for filter)
				filterLower := strings.ToLower(currentFilter)
				visibleIdx := 0
				var selectedEntry *searchEntry
				for i := range allResults {
					entry := &allResults[i]
					if currentFilter != "" {
						if !strings.Contains(strings.ToLower(entry.Name), filterLower) &&
							!strings.Contains(strings.ToLower(entry.Project), filterLower) &&
							!strings.Contains(strings.ToLower(entry.Location), filterLower) &&
							!strings.Contains(strings.ToLower(entry.Type), filterLower) {
							continue
						}
					}
					visibleIdx++
					if visibleIdx == row {
						selectedEntry = entry
						break
					}
				}
				resultsMu.Unlock()

				if selectedEntry != nil {
					showSearchResultDetail(app, table, flex, selectedEntry, &modalOpen, status, currentFilter)
				}
				return nil

			case '?':
				// Show help
				showSearchHelp(app, table, flex, &modalOpen, currentFilter, status)
				return nil
			}
		}
		return event
	})

	app.SetRoot(flex, true).EnableMouse(true).SetFocus(searchInput)
	return nil
}

// createSearchEngine creates a search engine with all providers
func createSearchEngine() *search.Engine {
	return search.NewEngine(
		&search.InstanceProvider{
			NewClient: func(ctx context.Context, project string) (search.InstanceClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.MIGProvider{
			NewClient: func(ctx context.Context, project string) (search.MIGClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.InstanceTemplateProvider{
			NewClient: func(ctx context.Context, project string) (search.InstanceTemplateClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.AddressProvider{
			NewClient: func(ctx context.Context, project string) (search.AddressClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.DiskProvider{
			NewClient: func(ctx context.Context, project string) (search.DiskClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.SnapshotProvider{
			NewClient: func(ctx context.Context, project string) (search.SnapshotClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.BucketProvider{
			NewClient: func(ctx context.Context, project string) (search.BucketClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.ForwardingRuleProvider{
			NewClient: func(ctx context.Context, project string) (search.ForwardingRuleClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.BackendServiceProvider{
			NewClient: func(ctx context.Context, project string) (search.BackendServiceClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.TargetPoolProvider{
			NewClient: func(ctx context.Context, project string) (search.TargetPoolClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.HealthCheckProvider{
			NewClient: func(ctx context.Context, project string) (search.HealthCheckClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.URLMapProvider{
			NewClient: func(ctx context.Context, project string) (search.URLMapClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.CloudSQLProvider{
			NewClient: func(ctx context.Context, project string) (search.CloudSQLClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.GKEClusterProvider{
			NewClient: func(ctx context.Context, project string) (search.GKEClusterClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.GKENodePoolProvider{
			NewClient: func(ctx context.Context, project string) (search.GKENodePoolClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.VPCNetworkProvider{
			NewClient: func(ctx context.Context, project string) (search.VPCNetworkClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.SubnetProvider{
			NewClient: func(ctx context.Context, project string) (search.SubnetClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.CloudRunProvider{
			NewClient: func(ctx context.Context, project string) (search.CloudRunClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.FirewallRuleProvider{
			NewClient: func(ctx context.Context, project string) (search.FirewallRuleClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.SecretProvider{
			NewClient: func(ctx context.Context, project string) (search.SecretClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
	)
}

// getTypeColor returns the color for a resource type
func getTypeColor(resourceType string) string {
	switch {
	case strings.HasPrefix(resourceType, "compute."):
		return "blue"
	case strings.HasPrefix(resourceType, "storage."):
		return "green"
	case strings.HasPrefix(resourceType, "container."):
		return "cyan"
	case strings.HasPrefix(resourceType, "sqladmin."):
		return "magenta"
	case strings.HasPrefix(resourceType, "run."):
		return "yellow"
	case strings.HasPrefix(resourceType, "secretmanager."):
		return "red"
	default:
		return "white"
	}
}

// showSearchResultDetail displays details for a search result
func showSearchResultDetail(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, entry *searchEntry, modalOpen *bool, status *tview.TextView, currentFilter string) {
	var detailText strings.Builder

	detailText.WriteString(fmt.Sprintf("[yellow::b]%s[-:-:-]\n\n", entry.Type))
	detailText.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]     %s\n", entry.Name))
	detailText.WriteString(fmt.Sprintf("[white::b]Project:[-:-:-]  %s\n", entry.Project))
	detailText.WriteString(fmt.Sprintf("[white::b]Location:[-:-:-] %s\n", entry.Location))

	if len(entry.Details) > 0 {
		detailText.WriteString("\n[yellow::b]Details:[-:-:-]\n")

		// Sort keys for consistent display
		keys := make([]string, 0, len(entry.Details))
		for k := range entry.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := entry.Details[k]
			if v != "" {
				detailText.WriteString(fmt.Sprintf("  [white::b]%s:[-:-:-] %s\n", k, v))
			}
		}
	}

	detailText.WriteString("\n[darkgray]Press Esc to close[-]")

	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(detailText.String()).
		SetScrollable(true).
		SetWordWrap(true)
	detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", entry.Name))

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
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter: %s[-]  [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details", currentFilter))
			} else {
				status.SetText(" [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details  [yellow]?[-] help")
			}
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(detailFlex, true).SetFocus(detailView)
}

// showSearchHelp displays help for the search view
func showSearchHelp(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, modalOpen *bool, currentFilter string, status *tview.TextView) {
	helpText := `[yellow::b]Search View - Keyboard Shortcuts[-:-:-]

[yellow]Search[-]
  [white]Enter[-]         Start search / Focus search input
  [white]Esc[-]           Cancel search (if running) / Clear filter / Go back

[yellow]Navigation[-]
  [white]↑/k[-]           Move selection up
  [white]↓/j[-]           Move selection down
  [white]Home/g[-]        Jump to first result
  [white]End/G[-]         Jump to last result

[yellow]Actions[-]
  [white]d[-]             Show details for selected result
  [white]/[-]             Filter displayed results

[yellow]Search Features[-]
  • Results appear progressively as they're found
  • Search can be cancelled at any time with Esc
  • Cancelled searches keep existing results
  • Filter (/) narrows displayed results without new search

[darkgray]Press Esc or ? to close this help[-]`

	helpView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)
	helpView.SetBorder(true).
		SetTitle(" Search Help ").
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
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(" [green]Filter: %s[-]  [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details", currentFilter))
			} else {
				status.SetText(" [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter  [yellow]d[-] details  [yellow]?[-] help")
			}
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(helpFlex, true).SetFocus(helpView)
}
