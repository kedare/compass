package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/rivo/tview"
)

// outputRedirector manages stdout/stderr redirection for TUI mode
type outputRedirector struct {
	origStdout *os.File
	origStderr *os.File
	devNull    *os.File
}

// newOutputRedirector creates a new output redirector and redirects stdout/stderr to /dev/null
func newOutputRedirector() *outputRedirector {
	r := &outputRedirector{
		origStdout: os.Stdout,
		origStderr: os.Stderr,
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return r
	}
	r.devNull = devNull

	os.Stdout = devNull
	os.Stderr = devNull

	return r
}

// Restore restores the original stdout/stderr
func (r *outputRedirector) Restore() {
	if r.origStdout != nil {
		os.Stdout = r.origStdout
	}
	if r.origStderr != nil {
		os.Stderr = r.origStderr
	}
	if r.devNull != nil {
		_ = r.devNull.Close()
	}
}

// OrigStdout returns the original stdout for use during Suspend
func (r *outputRedirector) OrigStdout() *os.File {
	return r.origStdout
}

// OrigStderr returns the original stderr for use during Suspend
func (r *outputRedirector) OrigStderr() *os.File {
	return r.origStderr
}

// Status bar message constants
const (
	statusDefault             = " [yellow]s[-] SSH  [yellow]d[-] details  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]v[-] VPN  [yellow]c[-] connectivity  [yellow]Shift+S[-] search  [yellow]Esc[-] quit  [yellow]?[-] help"
	statusFilterActive        = " [green]Filter active: '%s'[-]  [yellow]Esc[-] clear  [yellow]s[-] SSH  [yellow]d[-] details  [yellow]/[-] edit  [yellow]r[-] refresh  [yellow]v[-] VPN  [yellow]c[-] connectivity"
	statusFilterMode          = " [yellow]Type to filter, Enter to apply, Esc to cancel[-]"
	statusFilterCleared       = " [yellow]s[-] SSH  [yellow]d[-] details  [yellow]/[-] filter  [yellow]r[-] refresh  [yellow]Esc[-] quit  [yellow]?[-] help"
	statusNoSelection         = " [red]No instance selected[-]"
	statusDisconnected        = " [green]Disconnected from %s[-]"
	statusRefreshing          = " [yellow]Refreshing instance data from GCP...[-]"
	statusRefreshed           = " [green]Refreshed![-]"
	statusLoadingDetails      = " [yellow]Loading instance details...[-]"
	statusLoadingVPN          = " [yellow]Loading VPN view...[-]"
	statusLoadingConnectivity = " [yellow]Loading connectivity tests...[-]"
	statusErrorDetails        = " [red]Error loading details: %v[-]"
	statusErrorClient         = " [red]Error creating client: %v[-]"
	statusErrorVPN            = " [red]Error loading VPN view: %v[-]"
	statusErrorConnectivity   = " [red]Error loading connectivity tests: %v[-]"
)

// instanceData holds information about an instance for filtering
type instanceData struct {
	Name        string
	Project     string
	Zone        string
	Status      string
	ExternalIP  string
	InternalIP  string
	SearchText  string // Combined text for fuzzy search
	CanUseIAP   bool
	HasLiveData bool // Whether this instance has been loaded with live data from GCP
}

// RunDirect runs a minimal TUI without the full app structure
func RunDirect(c *cache.Cache, gcpClient *gcp.Client, parallelism int) error {
	// Disable logging to prevent log output from corrupting the TUI
	logger.Log.Disable()
	defer logger.Log.Restore()

	// Redirect stdout/stderr to prevent any remaining output from corrupting the TUI
	outputRedir := newOutputRedirector()
	defer outputRedir.Restore()

	app := tview.NewApplication()
	ctx := context.Background()
	// Store all instances for filtering
	var allInstances []instanceData
	var isRefreshing bool
	var modalOpen bool

	// Function to load instances from cache or GCP
	loadInstances := func(useLiveData bool) {
		isRefreshing = true
		allInstances = []instanceData{}

		projects := c.GetProjects()
		for _, project := range projects {
			locations, ok := c.GetLocationsByProject(project)
			if !ok {
				continue
			}

			for _, location := range locations {
				if location.Type == "instance" || location.Type == "mig" {
					inst := instanceData{
						Name:    location.Name,
						Project: project,
						Zone:    location.Location,
						Status:  "[gray]LOADING...[-]",
					}

					// If live data requested, fetch from GCP
					if useLiveData {
						// Create a client for the correct project
						projectClient, err := gcp.NewClient(ctx, project)
						if err == nil {
							gcpInst, err := projectClient.FindInstance(ctx, location.Name, location.Location)
							if err == nil {
								inst.Status = formatStatus(gcpInst.Status)
								inst.ExternalIP = gcpInst.ExternalIP
								inst.InternalIP = gcpInst.InternalIP
								inst.CanUseIAP = gcpInst.CanUseIAP
								inst.HasLiveData = true
							} else {
								inst.Status = "[red]ERROR[-]"
							}
						} else {
							inst.Status = "[red]ERROR[-]"
						}
					} else {
						// Use cached data
						inst.Status = "[yellow]CACHED[-]"
						inst.HasLiveData = false
					}

					// Create combined search text
					inst.SearchText = strings.ToLower(inst.Name + " " + inst.Project + " " + inst.Zone)
					allInstances = append(allInstances, inst)
				}
			}
		}
		isRefreshing = false
	}

	// Initial load from cache (fast)
	loadInstances(false)

	// Create a simple table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	table.SetBorder(true).SetTitle(fmt.Sprintf(" Compass Instances (%d) ", len(allInstances)))

	// Add header
	headers := []string{"Name", "Project", "Zone", "Status"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorBlack).
			SetBackgroundColor(tcell.ColorDarkCyan).
			SetSelectable(false).
			SetExpansion(1)
		table.SetCell(0, col, cell)
	}

	// Filter input field (hidden by default)
	filterInput := tview.NewInputField().
		SetLabel(" Filter: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow)

	var filterMode bool
	var currentFilter string

	// Function to update table with current filter
	updateTable := func(filter string) {
		// Clear existing rows (keep header)
		for row := table.GetRowCount() - 1; row > 0; row-- {
			table.RemoveRow(row)
		}

		currentRow := 1
		matchCount := 0

		for _, inst := range allInstances {
			// Apply filter if active
			if filter != "" {
				filterLower := strings.ToLower(filter)
				nameLower := strings.ToLower(inst.Name)
				projectLower := strings.ToLower(inst.Project)
				zoneLower := strings.ToLower(inst.Zone)

				// Use substring matching (contains) for exact matches
				nameMatch := strings.Contains(nameLower, filterLower)
				projectMatch := strings.Contains(projectLower, filterLower)
				zoneMatch := strings.Contains(zoneLower, filterLower)

				// Match if any field contains the filter
				if !nameMatch && !projectMatch && !zoneMatch {
					continue
				}
			}

			table.SetCell(currentRow, 0, tview.NewTableCell(inst.Name).SetExpansion(1))
			table.SetCell(currentRow, 1, tview.NewTableCell(inst.Project).SetExpansion(1))
			table.SetCell(currentRow, 2, tview.NewTableCell(inst.Zone).SetExpansion(1))
			table.SetCell(currentRow, 3, tview.NewTableCell(inst.Status).SetExpansion(1))
			currentRow++
			matchCount++
		}

		// Update title with match count
		if filter != "" {
			table.SetTitle(fmt.Sprintf(" Instances (%d/%d matched) ", matchCount, len(allInstances)))
		} else {
			table.SetTitle(fmt.Sprintf(" Compass Instances (%d) ", len(allInstances)))
		}

		// Select first data row if available
		if matchCount > 0 {
			table.Select(1, 0)
		}
	}

	// Initial table population
	updateTable("")

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(statusDefault)

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
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(statusFilterActive, currentFilter))
			} else {
				status.SetText(statusDefault)
			}
		case tcell.KeyEscape:
			// Cancel filter mode without applying
			filterInput.SetText(currentFilter)
			filterMode = false
			flex.RemoveItem(filterInput)
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(statusFilterActive, currentFilter))
			} else {
				status.SetText(statusDefault)
			}
		}
	})

	// Update table as user types (live fuzzy search)
	filterInput.SetChangedFunc(func(text string) {
		updateTable(text)
	})

	// Setup keyboard - store reference so we can restore it later
	var mainInputCapture func(event *tcell.EventKey) *tcell.EventKey
	mainInputCapture = func(event *tcell.EventKey) *tcell.EventKey {
		// If a modal is open, let it handle all keys (except Ctrl+C)
		if modalOpen && event.Key() != tcell.KeyCtrlC {
			return event
		}

		// If in filter mode, let the input field handle it
		if filterMode {
			return event
		}

		// Don't allow actions during refresh
		if isRefreshing {
			return event
		}

		switch event.Key() {
		case tcell.KeyCtrlC:
			app.Stop()
			return nil

		case tcell.KeyEscape:
			// Clear filter if active, otherwise quit
			if currentFilter != "" {
				currentFilter = ""
				filterInput.SetText("")
				updateTable("")
				status.SetText(statusFilterCleared)
				return nil
			}
			// No filter active, quit the TUI
			app.Stop()
			return nil
		case tcell.KeyRune:
			if event.Rune() == 's' {
				// SSH to selected instance
				row, _ := table.GetSelection()
				if row <= 0 || row >= table.GetRowCount() {
					status.SetText(statusNoSelection)
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							status.SetText(statusDefault)
						})
					})
					return nil
				}

				// Get instance details from table cells (name, project, zone)
				nameCell := table.GetCell(row, 0)
				projectCell := table.GetCell(row, 1)
				zoneCell := table.GetCell(row, 2)
				if nameCell == nil || projectCell == nil || zoneCell == nil {
					return nil
				}
				instanceName := nameCell.Text
				instanceProject := projectCell.Text
				instanceZone := zoneCell.Text

				// Find matching instance in allInstances using name, project, AND zone
				var selectedInst *instanceData
				for i := range allInstances {
					if allInstances[i].Name == instanceName &&
						allInstances[i].Project == instanceProject &&
						allInstances[i].Zone == instanceZone {
						selectedInst = &allInstances[i]
						break
					}
				}

				if selectedInst != nil {
					// Determine default IAP setting:
					// - HasLiveData is true AND ExternalIP is empty (no public IP), OR
					// - CanUseIAP is explicitly set to true
					defaultUseIAP := (selectedInst.HasLiveData && selectedInst.ExternalIP == "") || selectedInst.CanUseIAP

					// Capture values for callbacks
					instName := selectedInst.Name
					instProject := selectedInst.Project
					instZone := selectedInst.Zone

					// Show SSH options modal
					modalOpen = true
					ShowSSHOptionsModal(app, instName, defaultUseIAP,
						func(opts SSHOptions) {
							// Restore main view before SSH (needed for proper suspend/resume)
							app.SetRoot(flex, true)
							app.SetFocus(table)
							modalOpen = false

							// Execute SSH using shared function
							RunSSHSession(app, instName, instProject, instZone, opts, outputRedir)

							// After resume
							status.SetText(fmt.Sprintf(statusDisconnected, instName))
							time.AfterFunc(3*time.Second, func() {
								app.QueueUpdateDraw(func() {
									status.SetText(statusDefault)
								})
							})
						},
						func() {
							// Cancel - restore main view
							app.SetRoot(flex, true)
							app.SetFocus(table)
							modalOpen = false
							if currentFilter != "" {
								status.SetText(fmt.Sprintf(statusFilterActive, currentFilter))
							} else {
								status.SetText(statusDefault)
							}
						},
					)
				}
				return nil
			}
			if event.Rune() == 'd' {
				// Show instance details
				row, _ := table.GetSelection()
				if row <= 0 || row >= table.GetRowCount() {
					status.SetText(statusNoSelection)
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							status.SetText(statusDefault)
						})
					})
					return nil
				}

				// Get instance details from table cells (name, project, zone)
				nameCell := table.GetCell(row, 0)
				projectCell := table.GetCell(row, 1)
				zoneCell := table.GetCell(row, 2)
				if nameCell == nil || projectCell == nil || zoneCell == nil {
					return nil
				}
				instanceName := nameCell.Text
				instanceProject := projectCell.Text
				instanceZone := zoneCell.Text

				// Find matching instance in allInstances using name, project, AND zone
				var selectedInst *instanceData
				for i := range allInstances {
					if allInstances[i].Name == instanceName &&
						allInstances[i].Project == instanceProject &&
						allInstances[i].Zone == instanceZone {
						selectedInst = &allInstances[i]
						break
					}
				}

				if selectedInst != nil {
					// Fetch full instance details from GCP
					status.SetText(statusLoadingDetails)
					go func() {
						// Create a client for the correct project
						projectClient, err := gcp.NewClient(ctx, selectedInst.Project)
						if err != nil {
							app.QueueUpdateDraw(func() {
								status.SetText(fmt.Sprintf(statusErrorClient, err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										status.SetText(statusDefault)
									})
								})
							})
							return
						}

						gcpInst, err := projectClient.FindInstance(ctx, selectedInst.Name, selectedInst.Zone)
						app.QueueUpdateDraw(func() {
							if err != nil {
								status.SetText(fmt.Sprintf(statusErrorDetails, err))
								time.AfterFunc(3*time.Second, func() {
									app.QueueUpdateDraw(func() {
										status.SetText(statusDefault)
									})
								})
								return
							}

							// Create fullscreen detail view using shared formatter
							detailText := FormatInstanceDetails(gcpInst, selectedInst.Project)

							detailView := tview.NewTextView().
								SetDynamicColors(true).
								SetText(detailText).
								SetScrollable(true).
								SetWordWrap(true)
							detailView.SetBorder(true).SetTitle(fmt.Sprintf(" Instance: %s ", selectedInst.Name))

							// Create status bar for detail view
							detailStatus := tview.NewTextView().
								SetDynamicColors(true).
								SetText(" [yellow]Esc[-] back")

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
										status.SetText(fmt.Sprintf(statusFilterActive, currentFilter))
									} else {
										status.SetText(statusDefault)
									}
									return nil
								}
								return event
							})

							modalOpen = true
							app.SetRoot(detailFlex, true).SetFocus(detailView)
						})
					}()
				}
				return nil
			}
			if event.Rune() == '/' {
				// Enter filter mode
				filterMode = true
				filterInput.SetText(currentFilter)
				flex.Clear()
				flex.AddItem(filterInput, 1, 0, true)
				flex.AddItem(table, 0, 1, false)
				flex.AddItem(status, 1, 0, false)
				app.SetFocus(filterInput)
				status.SetText(statusFilterMode)
				return nil
			}
			if event.Rune() == 'R' {
				// Refresh instance data from GCP with loading screen
				refreshCtx, cancelRefresh := context.WithCancel(ctx)

				_, spinnerDone := showLoadingScreen(app, " Refreshing Instances ", "Refreshing instance data from GCP...", func() {
					cancelRefresh()
					app.SetRoot(flex, true).SetFocus(table)
					status.SetText(" [yellow]Refresh cancelled[-]")
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							status.SetText(statusDefault)
						})
					})
				})

				go func() {
					loadInstances(true)
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
						status.SetText(statusRefreshed)
						time.AfterFunc(2*time.Second, func() {
							app.QueueUpdateDraw(func() {
								status.SetText(statusDefault)
							})
						})
					})
				}()
				return nil
			}
			if event.Rune() == '?' {
				// Show help overlay
				helpText := `[yellow::b]Compass TUI - Keyboard Shortcuts[-:-:-]

[yellow]Navigation[-]
  [white]↑/k[-]           Move selection up
  [white]↓/j[-]           Move selection down
  [white]Home/g[-]        Jump to first item
  [white]End/G[-]         Jump to last item
  [white]PgUp[-]          Page up
  [white]PgDn[-]          Page down

[yellow]Instance Actions[-]
  [white]s[-]             SSH to selected instance
  [white]d[-]             Show instance details
  [white]Shift+R[-]       Refresh instance data from GCP

[yellow]Views[-]
  [white]v[-]             Switch to VPN view
  [white]c[-]             Switch to connectivity tests view
  [white]Shift+S[-]       Switch to global search view

[yellow]Filtering[-]
  [white]/[-]             Enter filter mode
  [white]Enter[-]         Apply filter (in filter mode)
  [white]Esc[-]           Cancel/clear filter, or quit TUI

[yellow]General[-]
  [white]?[-]             Show this help
  [white]q[-]             Quit
  [white]Ctrl+C[-]        Quit
  [white]Esc[-]           Close modals, clear filter, or quit

[darkgray]Press Esc or ? to close this help[-]`

				helpView := tview.NewTextView().
					SetDynamicColors(true).
					SetText(helpText).
					SetScrollable(true)
				helpView.SetBorder(true).
					SetTitle(" Help - Keyboard Shortcuts ").
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
							status.SetText(fmt.Sprintf(statusFilterActive, currentFilter))
						} else {
							status.SetText(statusDefault)
						}
						return nil
					}
					return event
				})

				modalOpen = true
				app.SetRoot(helpFlex, true).SetFocus(helpView)
				return nil
			}
			if event.Rune() == 'v' {
				// Switch to VPN view with cancellable loading
				loadingCtx, cancelLoading := context.WithCancel(ctx)

				// Create loading screen with progress
				loadingText := tview.NewTextView().
					SetDynamicColors(true).
					SetTextAlign(tview.AlignCenter)
				loadingText.SetBorder(true).SetTitle(" Loading VPN View ")

				spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
				spinnerIdx := 0
				currentMessage := "Initializing..."

				updateLoadingText := func() {
					loadingText.SetText(fmt.Sprintf("\n\n[yellow]%s[-] %s\n\n[gray]Press Ctrl+C or Esc to cancel[-]",
						spinnerFrames[spinnerIdx], currentMessage))
					spinnerIdx = (spinnerIdx + 1) % len(spinnerFrames)
				}
				updateLoadingText()

				loadingFlex := tview.NewFlex().
					AddItem(nil, 0, 1, false).
					AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
						AddItem(nil, 0, 1, false).
						AddItem(loadingText, 7, 0, true).
						AddItem(nil, 0, 1, false), 60, 0, true).
					AddItem(nil, 0, 1, false)

				// Set up cancel handler
				loadingText.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyCtrlC {
						cancelLoading()
						app.SetRoot(flex, true).SetFocus(table)
						status.SetText(" [yellow]Loading cancelled[-]")
						time.AfterFunc(2*time.Second, func() {
							app.QueueUpdateDraw(func() {
								status.SetText(statusDefault)
							})
						})
						return nil
					}
					return event
				})

				app.SetRoot(loadingFlex, true).SetFocus(loadingText)

				// Start spinner animation
				spinnerDone := make(chan bool, 2) // Buffered to prevent blocking
				go func() {
					ticker := time.NewTicker(100 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							app.QueueUpdateDraw(updateLoadingText)
						case <-spinnerDone:
							return
						case <-loadingCtx.Done():
							return
						}
					}
				}()

				// Progress callback for VPN loading
				progressCallback := func(msg string) {
					currentMessage = msg
					app.QueueUpdateDraw(updateLoadingText)
				}

				go func() {
					err := RunVPNViewWithProgress(loadingCtx, gcpClient, app, progressCallback, func() {
						// Callback to return to instance view
						select {
						case spinnerDone <- true:
						default:
						}
						app.SetInputCapture(mainInputCapture) // Restore main input handler
						app.SetRoot(flex, true).SetFocus(table)
						updateTable(currentFilter)
						status.SetText(statusDefault)
					})
					app.QueueUpdateDraw(func() {
						select {
						case spinnerDone <- true:
						default:
						}
						if err != nil {
							if loadingCtx.Err() == context.Canceled {
								// User cancelled, already handled
								return
							}
							app.SetRoot(flex, true).SetFocus(table)
							status.SetText(fmt.Sprintf(statusErrorVPN, err))
							time.AfterFunc(3*time.Second, func() {
								app.QueueUpdateDraw(func() {
									status.SetText(statusDefault)
								})
							})
						}
					})
				}()
				return nil
			}
			if event.Rune() == 'c' {
				// Switch to connectivity tests view with cancellable loading
				loadingCtx, cancelLoading := context.WithCancel(ctx)

				// Create loading screen with progress
				loadingText := tview.NewTextView().
					SetDynamicColors(true).
					SetTextAlign(tview.AlignCenter)
				loadingText.SetBorder(true).SetTitle(" Loading Connectivity Tests ")

				spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
				spinnerIdx := 0
				currentMessage := "Loading connectivity tests..."

				updateLoadingText := func() {
					loadingText.SetText(fmt.Sprintf("\n\n[yellow]%s[-] %s\n\n[gray]Press Ctrl+C or Esc to cancel[-]",
						spinnerFrames[spinnerIdx], currentMessage))
					spinnerIdx = (spinnerIdx + 1) % len(spinnerFrames)
				}
				updateLoadingText()

				loadingFlex := tview.NewFlex().
					AddItem(nil, 0, 1, false).
					AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
						AddItem(nil, 0, 1, false).
						AddItem(loadingText, 7, 0, true).
						AddItem(nil, 0, 1, false), 60, 0, true).
					AddItem(nil, 0, 1, false)

				// Set up cancel handler
				loadingText.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyCtrlC {
						cancelLoading()
						app.SetRoot(flex, true).SetFocus(table)
						status.SetText(" [yellow]Loading cancelled[-]")
						time.AfterFunc(2*time.Second, func() {
							app.QueueUpdateDraw(func() {
								status.SetText(statusDefault)
							})
						})
						return nil
					}
					return event
				})

				app.SetRoot(loadingFlex, true).SetFocus(loadingText)

				// Start spinner animation
				spinnerDone := make(chan bool, 2) // Buffered to prevent blocking
				go func() {
					ticker := time.NewTicker(100 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							app.QueueUpdateDraw(updateLoadingText)
						case <-spinnerDone:
							return
						case <-loadingCtx.Done():
							return
						}
					}
				}()

				go func() {
					err := RunConnectivityView(loadingCtx, gcpClient, app, func() {
						// Callback to return to instance view
						select {
						case spinnerDone <- true:
						default:
						}
						app.SetInputCapture(mainInputCapture) // Restore main input handler
						app.SetRoot(flex, true).SetFocus(table)
						updateTable(currentFilter)
						status.SetText(statusDefault)
					})
					app.QueueUpdateDraw(func() {
						select {
						case spinnerDone <- true:
						default:
						}
						if err != nil {
							if loadingCtx.Err() == context.Canceled {
								// User cancelled, already handled
								return
							}
							app.SetRoot(flex, true).SetFocus(table)
							status.SetText(fmt.Sprintf(statusErrorConnectivity, err))
							time.AfterFunc(3*time.Second, func() {
								app.QueueUpdateDraw(func() {
									status.SetText(statusDefault)
								})
							})
						}
					})
				}()
				return nil
			}
			if event.Rune() == 'S' {
				// Switch to search view
				status.SetText(" [yellow]Loading search view...[-]")

				go func() {
					err := RunSearchView(ctx, c, app, outputRedir, parallelism, func() {
						// Callback to return to instance view
						app.SetInputCapture(mainInputCapture) // Restore main input handler
						app.SetRoot(flex, true).SetFocus(table)
						updateTable(currentFilter)
						status.SetText(statusDefault)
					})
					app.QueueUpdateDraw(func() {
						if err != nil {
							app.SetRoot(flex, true).SetFocus(table)
							status.SetText(fmt.Sprintf(" [red]Error loading search view: %v[-]", err))
							time.AfterFunc(3*time.Second, func() {
								app.QueueUpdateDraw(func() {
									status.SetText(statusDefault)
								})
							})
						}
					})
				}()
				return nil
			}
			if event.Rune() == 'q' {
				app.Stop()
				return nil
			}
		}
		return event
	}

	// Set the main input capture
	app.SetInputCapture(mainInputCapture)

	// Run
	if err := app.SetRoot(flex, true).EnableMouse(true).Run(); err != nil {
		return fmt.Errorf("tview run error: %w", err)
	}

	return nil
}
