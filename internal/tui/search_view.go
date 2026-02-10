package tui

import (
	"context"
	"fmt"
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
// RunSearchView displays an interactive search interface for finding GCP resources.
// It provides progressive search results, filtering, and resource-specific actions.
//
// The view supports:
//   - Progressive search across multiple projects and resource types
//   - Search history navigation with ↑/↓ keys
//   - Fuzzy matching toggle with Tab key
//   - Real-time filtering with '/' key
//   - Context-aware actions (SSH, details, browser) based on resource type
//   - Project affinity learning (searches high-priority projects first)
//
// Key bindings are resource-type aware and displayed in the status bar.
func RunSearchView(ctx context.Context, c *cache.Cache, app *tview.Application, outputRedir *outputRedirector, parallelism int, onBack func()) error {
	// Initialize state management
	state := NewSearchViewState()

	// Load search history from cache
	if history, err := c.GetSearchHistory(20); err == nil {
		state.SearchHistory = history
	}

	// Get initial projects from cache (will be overridden per-search with affinity data)
	initialProjects := c.GetProjectsByUsage()
	if len(initialProjects) == 0 {
		return fmt.Errorf("no projects in cache")
	}

	// Create search engine with all providers
	engine := createSearchEngine(parallelism)

	// Create UI components
	searchInput := tview.NewInputField().
		SetLabel(" Search: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow).
		SetPlaceholder("Search by name, IP, or resource... (↑/↓ for history)")

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

	// Create table updater
	tableUpdater := NewTableUpdater(table)

	// Filter input
	filterInput := tview.NewInputField().
		SetLabel(" Filter: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow)

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Tab[-] fuzzy  [yellow]Enter[-] search  [yellow]Esc[-] back  [yellow]/[-] filter results  [yellow]d[-] details  [yellow]?[-] help")

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
		hasIssues := len(state.AllWarnings) > 0 || state.SearchError != nil
		if hasIssues {
			// Calculate height based on number of issues (min 3, max 8 lines)
			height := len(state.AllWarnings) + 2 // +2 for border
			if state.SearchError != nil {
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

		if state.SearchError != nil {
			content.WriteString(fmt.Sprintf("[red]Error:[-] %v\n", state.SearchError))
		}

		if len(state.AllWarnings) > 0 {
			// Group warnings by provider
			providerErrors := make(map[search.ResourceKind][]string)
			for _, w := range state.AllWarnings {
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
	// Function to update table with current results (copies data to avoid holding lock)
	updateTable := func(filter string) {
		state.ResultsMu.Lock()
		results := make([]searchEntry, len(state.AllResults))
		copy(results, state.AllResults)
		state.ResultsMu.Unlock()

		// Update table updater's search term for highlighting
		tableUpdater.CurrentSearchTerm = state.CurrentSearchTerm
		tableUpdater.UpdateWithData(filter, results)
	}

	// Alias for updateTable - both copy data safely
	updateTableNoLock := updateTable

	// Helper function to get the currently selected entry
	getSelectedEntry := func() *searchEntry {
		row, _ := table.GetSelection()
		if row <= 0 {
			return nil
		}

		state.ResultsMu.Lock()
		defer state.ResultsMu.Unlock()

		expr := parseFilter(state.CurrentFilter)
		visibleIdx := 0
		for i := range state.AllResults {
			entry := &state.AllResults[i]
			if !expr.matches(entry.Name, entry.Project, entry.Location, entry.Type) {
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
		state.ResultsMu.Lock()
		count := len(state.AllResults)
		state.ResultsMu.Unlock()

		entry := getSelectedEntry()
		var actionStr string
		if entry != nil {
			actions := GetActionsForResourceType(entry.Type)
			actionStr = FormatActionsStatusBar(actions)
		}

		// Build fuzzy indicator and toggle hint
		fuzzyBadge := ""
		fuzzyToggle := "[yellow]Tab[-] fuzzy"
		if state.FuzzyMode {
			fuzzyBadge = " [green::b]FUZZY[-:-:-] "
			fuzzyToggle = "[yellow]Tab[-] exact"
		}

		// During search, show search-specific status with context actions
		if state.IsSearching {
			if actionStr != "" {
				status.SetText(fmt.Sprintf("%s [yellow]Searching...[-]  %s  [yellow]Esc[-] cancel", fuzzyBadge, actionStr))
			} else {
				status.SetText(fmt.Sprintf("%s [yellow]Searching...[-]  [yellow]Esc[-] cancel", fuzzyBadge))
			}
			return
		}

		if entry == nil {
			if count > 0 {
				status.SetText(fmt.Sprintf("%s[green]%d results[-]  %s  [yellow]Enter[-] search  [yellow]/[-] filter  [yellow]Esc[-] back  [yellow]?[-] help", fuzzyBadge, count, fuzzyToggle))
			} else {
				status.SetText(fmt.Sprintf("%s%s  [yellow]Enter[-] search  [yellow]/[-] filter  [yellow]Esc[-] back  [yellow]?[-] help", fuzzyBadge, fuzzyToggle))
			}
			return
		}

		if state.CurrentFilter != "" {
			status.SetText(fmt.Sprintf("%s[green]Filter: %s[-]  %s  [yellow]/[-] edit  [yellow]Esc[-] clear  [yellow]?[-] help", fuzzyBadge, state.CurrentFilter, actionStr))
		} else {
			status.SetText(fmt.Sprintf("%s%s  %s  [yellow]Enter[-] search  [yellow]/[-] filter  [yellow]Esc[-] back  [yellow]?[-] help", fuzzyBadge, actionStr, fuzzyToggle))
		}
	}

	// Update status when selection changes
	table.SetSelectionChangedFunc(func(row, column int) {
		if !state.ModalOpen && !state.FilterMode {
			updateStatusWithActions()
		}
	})

	// Function to perform search with streaming results
	// This function should be called from a goroutine to avoid blocking the event loop
	performSearch := func(query string) {
		if query == "" {
			return
		}

		// Cancel any existing search
		if state.SearchCancel != nil {
			state.SearchCancel()
		}

		// Clear previous results and warnings
		state.ResultsMu.Lock()
		state.AllResults = []searchEntry{}
		state.AllWarnings = nil
		state.SearchError = nil
		state.ResultsMu.Unlock()

		searchCtx, cancel := context.WithCancel(ctx)
		state.SearchCancel = cancel
		state.IsSearching = true

		// Track this search term for affinity reinforcement and reset recorded tracking
		state.CurrentSearchTerm = query
		state.RecordedAffinity = make(map[string]struct{})

		// Add to search history
		go func() {
			_ = c.AddSearchHistory(query)
			// Reload history for next time
			if history, err := c.GetSearchHistory(20); err == nil {
				state.SearchHistory = history
				state.HistoryIndex = -1
			}
		}()

		// Get projects prioritized for this search term using learned affinity
		searchProjects := c.GetProjectsForSearch(query, nil)
		if len(searchProjects) == 0 {
			searchProjects = initialProjects
		}

		// Track progress
		var currentProgress search.SearchProgress
		var progressMu sync.Mutex

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
					state.ResultsMu.Lock()
					count := len(state.AllResults)
					state.ResultsMu.Unlock()
					progressMu.Lock()
					prog := currentProgress
					progressMu.Unlock()
					frame := spinnerFrames[spinnerIdx]
					spinnerIdx = (spinnerIdx + 1) % len(spinnerFrames)
					app.QueueUpdateDraw(func() {
						if state.IsSearching {
							if prog.TotalRequests > 0 {
								// Calculate project progress
								projectsSearched := prog.CompletedRequests / len(engine.GetProviderKinds())
								if projectsSearched > len(searchProjects) {
									projectsSearched = len(searchProjects)
								}

								// Calculate percentage for calls
								callPercent := 0
								if prog.TotalRequests > 0 {
									callPercent = (prog.CompletedRequests * 100) / prog.TotalRequests
								}

								// Build progress message
								var progressMsg string
								if prog.CurrentProject != "" {
									// Shorten long project names
									projectName := prog.CurrentProject
									if len(projectName) > 25 {
										projectName = projectName[:22] + "..."
									}

									// Show current project with progress
									if count > 0 {
										progressMsg = fmt.Sprintf("[yellow]%s [cyan]%s[-] [dim]• %d/%d projects • %d/%d calls (%d%%)[:-:-] • [green]%d found[-]",
											frame, projectName, projectsSearched, len(searchProjects),
											prog.CompletedRequests, prog.TotalRequests, callPercent, count)
									} else {
										progressMsg = fmt.Sprintf("[yellow]%s [cyan]%s[-] [dim]• %d/%d projects • %d/%d calls (%d%%)[:-:-]",
											frame, projectName, projectsSearched, len(searchProjects),
											prog.CompletedRequests, prog.TotalRequests, callPercent)
									}
								} else {
									// Fallback if no current project
									if count > 0 {
										progressMsg = fmt.Sprintf("[yellow]%s Searching [dim]• %d/%d projects • %d/%d calls (%d%%)[:-:-] • [green]%d found[-]",
											frame, projectsSearched, len(searchProjects),
											prog.CompletedRequests, prog.TotalRequests, callPercent, count)
									} else {
										progressMsg = fmt.Sprintf("[yellow]%s Searching [dim]• %d/%d projects • %d/%d calls (%d%%)[:-:-]",
											frame, projectsSearched, len(searchProjects),
											prog.CompletedRequests, prog.TotalRequests, callPercent)
									}
								}

								progressText.SetText(progressMsg)
							} else {
								progressText.SetText(fmt.Sprintf("[yellow]%s Starting search...[-]", frame))
							}
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
			updateStatusWithActions()
			progressText.SetText("[yellow]Starting search...[-]")
		})

		// Run search
		searchQuery := search.Query{Term: query, Fuzzy: state.FuzzyMode}

		callback := func(results []search.Result, progress search.SearchProgress) error {
			// Check if cancelled
			if searchCtx.Err() != nil {
				return searchCtx.Err()
			}

			// Update progress
			progressMu.Lock()
			currentProgress = progress
			progressMu.Unlock()

			// Add new results (if any)
			if len(results) > 0 {
				newEntries := make([]searchEntry, 0, len(results))
				// Track new affinities to record
				newProjectResults := make(map[string]int)
				newProjectTypeResults := make(map[string]map[string]int)

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

					// Track for real-time affinity recording (only new project+type combos)
					affinityKey := r.Project + "|" + string(r.Type)
					if _, exists := state.RecordedAffinity[affinityKey]; !exists {
						state.RecordedAffinity[affinityKey] = struct{}{}
						newProjectResults[r.Project]++
						if string(r.Type) != "" {
							if newProjectTypeResults[r.Project] == nil {
								newProjectTypeResults[r.Project] = make(map[string]int)
							}
							newProjectTypeResults[r.Project][string(r.Type)]++
						}
					}
				}

				state.ResultsMu.Lock()
				state.AllResults = append(state.AllResults, newEntries...)
				state.ResultsMu.Unlock()

				// Record affinity in real-time (in background to not block)
				if len(newProjectResults) > 0 {
					go func(term string, projResults map[string]int, projTypeResults map[string]map[string]int) {
						// Record general affinity
						_ = c.RecordSearchAffinity(term, projResults, "")
						// Record per-resource-type affinities
						for proj, typeCounts := range projTypeResults {
							for resType, count := range typeCounts {
								_ = c.RecordSearchAffinity(term, map[string]int{proj: count}, resType)
							}
						}
					}(query, newProjectResults, newProjectTypeResults)
				}
			}

			// Update UI - copy current filter to avoid race
			filter := state.CurrentFilter
			app.QueueUpdateDraw(func() {
				updateTableNoLock(filter)
			})

			return nil
		}

		output, err := engine.SearchStreaming(searchCtx, searchProjects, searchQuery, callback)

		// Stop spinner
		select {
		case spinnerDone <- true:
		default:
		}

		state.IsSearching = false

		// Store warnings and error
		state.ResultsMu.Lock()
		if err != nil {
			state.SearchError = err
		}
		if output != nil && len(output.Warnings) > 0 {
			state.AllWarnings = output.Warnings
		}
		state.ResultsMu.Unlock()

		// Note: Search affinity is now recorded in real-time during the search callback

		// Final UI update
		filter := state.CurrentFilter
		app.QueueUpdateDraw(func() {
			progressText.SetText("")

			// Update warnings pane and rebuild layout to show/hide it
			updateWarningsPane()
			rebuildLayout(false, true)
			updateTableNoLock(filter)
			app.SetFocus(table)

			// Update status with context-aware actions for selected item
			updateStatusWithActions()
		})
	}

	// Search input history navigation
	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			// Navigate backward in history
			if len(state.SearchHistory) > 0 {
				if state.HistoryIndex == -1 {
					state.HistoryIndex = 0
				} else if state.HistoryIndex < len(state.SearchHistory)-1 {
					state.HistoryIndex++
				}
				if state.HistoryIndex >= 0 && state.HistoryIndex < len(state.SearchHistory) {
					searchInput.SetText(state.SearchHistory[state.HistoryIndex])
				}
			}
			return nil
		case tcell.KeyDown:
			// Navigate forward in history
			if len(state.SearchHistory) > 0 && state.HistoryIndex > 0 {
				state.HistoryIndex--
				if state.HistoryIndex >= 0 {
					searchInput.SetText(state.SearchHistory[state.HistoryIndex])
				}
			} else if state.HistoryIndex == 0 {
				state.HistoryIndex = -1
				searchInput.SetText("")
			}
			return nil
		}
		return event
	})

	// Search input handler
	searchInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			query := strings.TrimSpace(searchInput.GetText())
			if query != "" {
				state.HistoryIndex = -1 // Reset history navigation
				app.SetFocus(table)
				// Run search in goroutine to avoid blocking the event loop
				go performSearch(query)
			}
		case tcell.KeyEscape:
			if state.IsSearching && state.SearchCancel != nil {
				state.SearchCancel()
			} else {
				onBack()
			}
		}
	})

	// Filter input handler
	filterInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			state.CurrentFilter = filterInput.GetText()
			updateTable(state.CurrentFilter)
			state.FilterMode = false
			rebuildLayout(false, true)
			app.SetFocus(table)
			updateStatusWithActions()
		case tcell.KeyEscape:
			filterInput.SetText(state.CurrentFilter)
			state.FilterMode = false
			rebuildLayout(false, true)
			app.SetFocus(table)
		}
	})

	// Setup keyboard handlers
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If a modal is open, let it handle all keys (except Ctrl+C)
		if state.ModalOpen && event.Key() != tcell.KeyCtrlC {
			return event
		}

		// Tab toggles fuzzy mode globally, regardless of focus
		if event.Key() == tcell.KeyTab {
			state.FuzzyMode = !state.FuzzyMode
			if state.FuzzyMode {
				searchInput.SetLabel(" Search (fuzzy): ")
			} else {
				searchInput.SetLabel(" Search: ")
			}
			updateStatusWithActions()
			// Re-run current search if there is one
			if state.CurrentSearchTerm != "" {
				go performSearch(state.CurrentSearchTerm)
			}
			return nil
		}

		// If in filter mode, let the input field handle it
		if state.FilterMode {
			return event
		}

		// If search input is focused, let it handle most keys
		if app.GetFocus() == searchInput {
			return event
		}

		switch event.Key() {
		case tcell.KeyEscape:
			if state.IsSearching && state.SearchCancel != nil {
				// Cancel ongoing search but stay in view
				state.SearchCancel()
				return nil
			}
			if state.CurrentFilter != "" {
				// Clear filter
				state.CurrentFilter = ""
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
			if state.SearchCancel != nil {
				state.SearchCancel()
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
				state.FilterMode = true
				filterInput.SetText(state.CurrentFilter)
				rebuildLayout(true, false)
				app.SetFocus(filterInput)
				status.SetText(" [yellow]Filter: spaces=AND  |=OR  -=NOT  (e.g. \"web|api -dev\")  Enter to apply, Esc to cancel[-]")
				return nil

			case 's':
				// SSH to instance or MIG
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

				isInstance := selectedEntry.Type == string(search.KindComputeInstance)
				isMIG := selectedEntry.Type == string(search.KindManagedInstanceGroup)

				if !isInstance && !isMIG {
					status.SetText(" [red]SSH only available for compute instances and MIGs[-]")
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							updateStatusWithActions()
						})
					})
					return nil
				}

				// Reinforce search affinity when user SSHs to an instance
				if state.CurrentSearchTerm != "" {
					go func(term, proj, resType string) {
						_ = c.TouchSearchAffinity(term, proj, resType)
						_ = c.TouchSearchAffinity(term, proj, "") // Also touch general affinity
					}(state.CurrentSearchTerm, selectedEntry.Project, selectedEntry.Type)
				}

				// Capture values for callbacks
				resourceName := selectedEntry.Name
				resourceProject := selectedEntry.Project
				resourceLocation := selectedEntry.Location

				// For MIG, resolve to an actual instance first
				if isMIG {
					status.SetText(fmt.Sprintf(" [yellow]Resolving MIG %s...[-]", resourceName))
					go func() {
						projectClient, err := gcp.NewClient(ctx, resourceProject)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error creating client: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						// Get all instances in the MIG
						instances, _, err := projectClient.ListMIGInstances(ctx, resourceName, resourceLocation)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error resolving MIG: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						if len(instances) == 0 {
							app.QueueUpdateDraw(func() {
								status.SetText(" [red]No instances found in MIG[-]")
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						// Helper to connect to a specific instance
						connectToInstance := func(instRef gcp.ManagedInstanceRef) {
							instance, err := projectClient.FindInstance(ctx, instRef.Name, instRef.Zone)
							if err != nil {
								app.QueueUpdateDraw(func() {
									status.SetText(fmt.Sprintf(" [red]Error fetching instance: %v[-]", err))
									time.AfterFunc(3*time.Second, func() {
										app.QueueUpdateDraw(func() {
											updateStatusWithActions()
										})
									})
								})
								return
							}

							app.QueueUpdateDraw(func() {
								cachedIAP := LoadIAPPreference(instance.Name)
								cachedSSHFlags := LoadSSHFlags(instance.Name, resourceProject)
								defaultUseIAP := instance.CanUseIAP

								state.ModalOpen = true
								ShowSSHOptionsModal(app, instance.Name, defaultUseIAP, cachedIAP, cachedSSHFlags,
									func(opts SSHOptions) {
										app.SetRoot(flex, true)
										app.SetFocus(table)
										state.ModalOpen = false

										RunSSHSession(app, instance.Name, resourceProject, instance.Zone, opts, outputRedir)

										status.SetText(fmt.Sprintf(" [green]Disconnected from %s[-]", instance.Name))
										time.AfterFunc(3*time.Second, func() {
											app.QueueUpdateDraw(func() {
												updateStatusWithActions()
											})
										})
									},
									func() {
										app.SetRoot(flex, true)
										app.SetFocus(table)
										state.ModalOpen = false
										updateStatusWithActions()
									},
								)
							})
						}

						// If only one instance, connect directly
						if len(instances) == 1 {
							connectToInstance(instances[0])
							return
						}

						// Multiple instances - show selection modal
						app.QueueUpdateDraw(func() {
							state.ModalOpen = true
							ShowMIGInstanceSelectionModal(app, resourceName, instances,
								func(selected MIGInstanceSelection) {
									status.SetText(fmt.Sprintf(" [yellow]Connecting to %s...[-]", selected.Name))
									go connectToInstance(gcp.ManagedInstanceRef{
										Name:   selected.Name,
										Zone:   selected.Zone,
										Status: selected.Status,
									})
								},
								func() {
									// User cancelled
									app.SetRoot(flex, true)
									app.SetFocus(table)
									state.ModalOpen = false
									updateStatusWithActions()
								},
							)
						})
					}()
				} else {
					// Regular instance - show SSH modal directly
					cachedIAP := LoadIAPPreference(resourceName)
					cachedSSHFlags := LoadSSHFlags(resourceName, resourceProject)

					state.ModalOpen = true
					ShowSSHOptionsModal(app, resourceName, false, cachedIAP, cachedSSHFlags,
						func(opts SSHOptions) {
							app.SetRoot(flex, true)
							app.SetFocus(table)
							state.ModalOpen = false

							RunSSHSession(app, resourceName, resourceProject, resourceLocation, opts, outputRedir)

							status.SetText(fmt.Sprintf(" [green]Disconnected from %s[-]", resourceName))
							time.AfterFunc(3*time.Second, func() {
								app.QueueUpdateDraw(func() {
									updateStatusWithActions()
								})
							})
						},
						func() {
							app.SetRoot(flex, true)
							app.SetFocus(table)
							state.ModalOpen = false
							updateStatusWithActions()
						},
					)
				}
				return nil

			case 'd':
				// Show details for selected result
				// Note: This handler dispatches to type-specific detail views.
				// Future refactoring: Consider implementing a registry pattern
				// (see search_actions.go) to reduce this if/else chain.
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

				// Reinforce search affinity when user views details
				if state.CurrentSearchTerm != "" {
					go func(term, proj, resType string) {
						_ = c.TouchSearchAffinity(term, proj, resType)
						_ = c.TouchSearchAffinity(term, proj, "") // Also touch general affinity
					}(state.CurrentSearchTerm, selectedEntry.Project, selectedEntry.Type)
				}

				// For instances, fetch live details from GCP
				if selectedEntry.Type == string(search.KindComputeInstance) {
					// Set status directly (not via QueueUpdateDraw) since we're in input handler
					status.SetText(" [yellow]Loading instance details...[-]")

					// Capture values for closure
					entryName := selectedEntry.Name

					executor := NewInstanceActionExecutor(
						selectedEntry.Name,
						selectedEntry.Project,
						selectedEntry.Location,
						false,
					)

					actionCtx := &ActionContext{
						App:         app,
						Ctx:         ctx,
						OutputRedir: outputRedir,
						OnError: func(err error) {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
						},
					}

					executor.ExecuteDetails(actionCtx, func(details string) {
						showInstanceDetailModal(app, table, flex, entryName, details, &state.ModalOpen, status, state.CurrentFilter, updateStatusWithActions)
					})
				} else if selectedEntry.Type == string(search.KindInstanceTemplate) {
					// For instance templates, use formatted details
					details := FormatInstanceTemplateDetails(selectedEntry.Name, selectedEntry.Project, selectedEntry.Location, selectedEntry.Details)
					showInstanceDetailModal(app, table, flex, selectedEntry.Name, details, &state.ModalOpen, status, state.CurrentFilter, updateStatusWithActions)
				} else if selectedEntry.Type == string(search.KindManagedInstanceGroup) {
					// For MIGs, use formatted details
					details := FormatMIGDetails(selectedEntry.Name, selectedEntry.Project, selectedEntry.Location, selectedEntry.Details)
					showInstanceDetailModal(app, table, flex, selectedEntry.Name, details, &state.ModalOpen, status, state.CurrentFilter, updateStatusWithActions)
				} else if selectedEntry.Type == string(search.KindConnectivityTest) {
					// For connectivity tests, fetch and show full details
					status.SetText(" [yellow]Loading connectivity test details...[-]")

					go func() {
						connClient, err := gcp.NewConnectivityClient(ctx, selectedEntry.Project)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error creating client: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						test, err := connClient.GetTest(ctx, selectedEntry.Name)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error loading test: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						app.QueueUpdateDraw(func() {
							ShowConnectivityTestDetails(app, test, func() {
								// On close callback
								state.ModalOpen = false
								app.SetRoot(flex, true)
								app.SetFocus(table)
								updateStatusWithActions()
							})
							state.ModalOpen = true
						})
					}()
				} else if selectedEntry.Type == string(search.KindVPNGateway) {
					// For VPN gateways, fetch and show full details
					go func() {
						// Progress callback to keep status updated
						progressCallback := func(msg string) {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [yellow]%s[-]", msg))
							})
						}

						progressCallback("Loading VPN gateway details...")

						gcpClient, err := gcp.NewClient(ctx, selectedEntry.Project)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error creating client: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						gateway, err := gcpClient.GetVPNGatewayOverview(ctx, selectedEntry.Location, selectedEntry.Name, progressCallback)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error loading gateway: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						app.QueueUpdateDraw(func() {
							ShowVPNGatewayDetails(app, gateway, func() {
								// On close callback
								state.ModalOpen = false
								app.SetRoot(flex, true)
								app.SetFocus(table)
								updateStatusWithActions()
							})
							state.ModalOpen = true
						})
					}()
				} else if selectedEntry.Type == string(search.KindVPNTunnel) {
					// For VPN tunnels, fetch and show full details
					go func() {
						// Progress callback to keep status updated
						progressCallback := func(msg string) {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [yellow]%s[-]", msg))
							})
						}

						progressCallback("Loading VPN tunnel details...")

						gcpClient, err := gcp.NewClient(ctx, selectedEntry.Project)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error creating client: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						tunnel, err := gcpClient.GetVPNTunnelOverview(ctx, selectedEntry.Location, selectedEntry.Name, progressCallback)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error loading tunnel: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
							return
						}

						app.QueueUpdateDraw(func() {
							ShowVPNTunnelDetails(app, tunnel, func() {
								// On close callback
								state.ModalOpen = false
								app.SetRoot(flex, true)
								app.SetFocus(table)
								updateStatusWithActions()
							})
							state.ModalOpen = true
						})
					}()
				} else {
					// For other types, show generic details
					showSearchResultDetail(app, table, flex, selectedEntry, &state.ModalOpen, status, state.CurrentFilter, updateStatusWithActions)
				}
				return nil

			case 'o':
				// Open in browser (for buckets) - legacy shortcut
				selectedEntry := getSelectedEntry()
				if selectedEntry == nil {
					return nil
				}

				if selectedEntry.Type == string(search.KindBucket) {
					executor := NewBucketActionExecutor(selectedEntry.Name, selectedEntry.Project)
					actionCtx := &ActionContext{
						App:         app,
						Ctx:         ctx,
						OutputRedir: outputRedir,
						OnStatusUpdate: func(msg string) {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [green]%s[-]", msg))
								time.AfterFunc(2*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
						},
						OnError: func(err error) {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(" [red]Error: %v[-]", err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										updateStatusWithActions()
									})
								})
							})
						},
					}
					executor.ExecuteOpen(actionCtx)
				}
				return nil

			case 'b':
				// Open in Google Cloud Console
				selectedEntry := getSelectedEntry()
				if selectedEntry == nil {
					return nil
				}

				url := GetCloudConsoleURL(selectedEntry.Type, selectedEntry.Name, selectedEntry.Project, selectedEntry.Location, selectedEntry.Details)
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
				showSearchHelp(app, table, flex, &state.ModalOpen, state.CurrentFilter, status, updateStatusWithActions)
				return nil
			}
		}
		return event
	})

	app.SetRoot(flex, true).EnableMouse(true).SetFocus(searchInput)
	return nil
}

// createSearchEngine creates a search engine with all providers
func createSearchEngine(parallelism int) *search.Engine {
	engine := search.NewEngine(
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
		&search.VPNGatewayProvider{
			NewClient: func(ctx context.Context, project string) (search.VPNGatewayClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.VPNTunnelProvider{
			NewClient: func(ctx context.Context, project string) (search.VPNTunnelClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.RouteProvider{
			NewClient: func(ctx context.Context, project string) (search.RouteClient, error) {
				return gcp.NewClient(ctx, project)
			},
		},
		&search.ConnectivityTestProvider{
			NewClient: func(ctx context.Context, project string) (search.ConnectivityTestClient, error) {
				return gcp.NewConnectivityClient(ctx, project)
			},
		},
	)
	engine.MaxConcurrentProjects = parallelism
	return engine
}

// getTypeColor returns the color for a resource type
// highlightMatch wraps the first occurrence of term in text with tview bold+yellow color tags.
// The match is case-insensitive but preserves the original case in the output.
