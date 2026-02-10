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
	statusDefault           = " [yellow]s[-] SSH  [yellow]d[-] details  [yellow]b[-] browser  [yellow]/[-] filter  [yellow]Shift+R[-] refresh  [yellow]v[-] VPN  [yellow]c[-] connectivity  [yellow]Shift+S[-] search  [yellow]i[-] IP lookup  [yellow]Esc[-] quit  [yellow]?[-] help"
	statusFilterActive      = " [green]Filter active: '%s'[-]  [yellow]Esc[-] clear  [yellow]s[-] SSH  [yellow]d[-] details  [yellow]b[-] browser  [yellow]/[-] edit"
	statusFilterMode        = " [yellow]Type to filter, Enter to apply, Esc to cancel[-]"
	statusNoSelection       = " [red]No instance selected[-]"
	statusDisconnected      = " [green]Disconnected from %s[-]"
	statusLoadingDetails    = " [yellow]Loading instance details...[-]"
	statusErrorDetails      = " [red]Error loading details: %v[-]"
	statusErrorClient       = " [red]Error creating client: %v[-]"
	statusErrorVPN          = " [red]Error loading VPN view: %v[-]"
	statusErrorConnectivity = " [red]Error loading connectivity tests: %v[-]"
)

// tuiState holds all mutable state for the TUI
type tuiState struct {
	app           *tview.Application
	cache         *cache.Cache
	ctx           context.Context
	outputRedir   *outputRedirector
	parallelism   int
	allInstances  []instanceData
	isRefreshing  bool
	currentFilter string

	// UI components
	table       *tview.Table
	flex        *tview.Flex
	status      *tview.TextView
	filterInput *tview.InputField

	// Keybinding manager
	kb *KeyBindings
}

// restoreMainView restores the main instance view after a modal/view
func (s *tuiState) restoreMainView() {
	s.kb.SetMode(ModeNormal)
	s.app.SetInputCapture(s.createInputCapture())
	s.app.SetRoot(s.flex, true).SetFocus(s.table)
	s.updateStatusBar()
}

// updateStatusBar updates the status bar based on current state
func (s *tuiState) updateStatusBar() {
	if s.currentFilter != "" {
		s.status.SetText(fmt.Sprintf(statusFilterActive, s.currentFilter))
	} else {
		s.status.SetText(statusDefault)
	}
}

// flashStatus shows a temporary status message
func (s *tuiState) flashStatus(msg string, duration time.Duration) {
	s.status.SetText(msg)
	time.AfterFunc(duration, func() {
		s.app.QueueUpdateDraw(func() {
			s.updateStatusBar()
		})
	})
}

// getSelectedInstance returns the currently selected instance or nil
func (s *tuiState) getSelectedInstance() *instanceData {
	row, _ := s.table.GetSelection()
	if row <= 0 || row >= s.table.GetRowCount() {
		return nil
	}

	nameCell := s.table.GetCell(row, 0)
	projectCell := s.table.GetCell(row, 1)
	zoneCell := s.table.GetCell(row, 2)
	if nameCell == nil || projectCell == nil || zoneCell == nil {
		return nil
	}

	instanceName := nameCell.Text
	instanceProject := projectCell.Text
	instanceZone := zoneCell.Text

	for i := range s.allInstances {
		if s.allInstances[i].Name == instanceName &&
			s.allInstances[i].Project == instanceProject &&
			s.allInstances[i].Zone == instanceZone {
			return &s.allInstances[i]
		}
	}
	return nil
}

// loadInstances loads instances from cache or GCP
func (s *tuiState) loadInstances(useLiveData bool) {
	s.isRefreshing = true
	s.allInstances = []instanceData{}

	projects := s.cache.GetProjects()
	for _, project := range projects {
		locations, ok := s.cache.GetLocationsByProject(project)
		if !ok {
			continue
		}

		for _, location := range locations {
			// Skip instances that belong to a MIG (they can be accessed via the MIG entry)
			if location.Type == "instance" && location.MIGName != "" {
				logger.Log.Debugf("Skipping MIG member instance: %s (MIG: %s)", location.Name, location.MIGName)
				continue
			}

			if location.Type == "instance" || location.Type == "mig" {
				logger.Log.Debugf("Adding to list: %s (type: %s, MIGName: %q)", location.Name, location.Type, location.MIGName)
				inst := instanceData{
					Name:    location.Name,
					Project: project,
					Zone:    location.Location,
					Status:  "[gray]LOADING...[-]",
					IsMIG:   location.Type == "mig",
				}

				if useLiveData {
					projectClient, err := gcp.NewClient(s.ctx, project)
					if err == nil {
						gcpInst, err := projectClient.FindInstance(s.ctx, location.Name, location.Location)
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
					inst.Status = "[yellow]CACHED[-]"
					inst.HasLiveData = false
				}

				inst.SearchText = strings.ToLower(inst.Name + " " + inst.Project + " " + inst.Zone)
				s.allInstances = append(s.allInstances, inst)
			}
		}
	}
	s.isRefreshing = false
}

// updateTable updates the table with the current filter
func (s *tuiState) updateTable() {
	filter := s.currentFilter

	// Clear existing rows (keep header)
	for row := s.table.GetRowCount() - 1; row > 0; row-- {
		s.table.RemoveRow(row)
	}

	currentRow := 1
	matchCount := 0

	for _, inst := range s.allInstances {
		if filter != "" {
			filterLower := strings.ToLower(filter)
			nameLower := strings.ToLower(inst.Name)
			projectLower := strings.ToLower(inst.Project)
			zoneLower := strings.ToLower(inst.Zone)

			nameMatch := strings.Contains(nameLower, filterLower)
			projectMatch := strings.Contains(projectLower, filterLower)
			zoneMatch := strings.Contains(zoneLower, filterLower)

			if !nameMatch && !projectMatch && !zoneMatch {
				continue
			}
		}

		s.table.SetCell(currentRow, 0, tview.NewTableCell(inst.Name).SetExpansion(1))
		s.table.SetCell(currentRow, 1, tview.NewTableCell(inst.Project).SetExpansion(1))
		s.table.SetCell(currentRow, 2, tview.NewTableCell(inst.Zone).SetExpansion(1))
		typeStr := "Instance"
		if inst.IsMIG {
			typeStr = "MIG"
		}
		s.table.SetCell(currentRow, 3, tview.NewTableCell(typeStr).SetExpansion(1))
		s.table.SetCell(currentRow, 4, tview.NewTableCell(inst.Status).SetExpansion(1))
		currentRow++
		matchCount++
	}

	if filter != "" {
		s.table.SetTitle(fmt.Sprintf(" Instances (%d/%d matched) ", matchCount, len(s.allInstances)))
	} else {
		s.table.SetTitle(fmt.Sprintf(" Compass Instances (%d) ", len(s.allInstances)))
	}

	if matchCount > 0 {
		s.table.Select(1, 0)
	}
}

// Action handlers

func (s *tuiState) actionSSH() bool {
	inst := s.getSelectedInstance()
	if inst == nil {
		s.flashStatus(statusNoSelection, 2*time.Second)
		return true
	}

	instName := inst.Name
	instProject := inst.Project
	instZone := inst.Zone
	isMIG := inst.IsMIG

	if isMIG {
		s.status.SetText(fmt.Sprintf(" [yellow]Resolving MIG %s...[-]", instName))
		go s.resolveMIGAndSSH(instName, instProject, instZone)
	} else {
		defaultUseIAP := (inst.HasLiveData && inst.ExternalIP == "") || inst.CanUseIAP
		cachedIAP := LoadIAPPreference(instName)
		cachedSSHFlags := LoadSSHFlags(instName, instProject)
		s.showSSHModal(instName, instProject, instZone, defaultUseIAP, cachedIAP, cachedSSHFlags)
	}
	return true
}

func (s *tuiState) resolveMIGAndSSH(migName, project, zone string) {
	projectClient, err := gcp.NewClient(s.ctx, project)
	if err != nil {
		s.app.QueueUpdateDraw(func() {
			s.flashStatus(fmt.Sprintf(" [red]Error creating client: %v[-]", err), 3*time.Second)
		})
		return
	}

	// Get all instances in the MIG
	instances, _, err := projectClient.ListMIGInstances(s.ctx, migName, zone)
	if err != nil {
		s.app.QueueUpdateDraw(func() {
			s.flashStatus(fmt.Sprintf(" [red]Error resolving MIG: %v[-]", err), 3*time.Second)
		})
		return
	}

	if len(instances) == 0 {
		s.app.QueueUpdateDraw(func() {
			s.flashStatus(" [red]No instances found in MIG[-]", 3*time.Second)
		})
		return
	}

	// If only one instance, connect directly
	if len(instances) == 1 {
		s.connectToMIGInstance(projectClient, project, instances[0])
		return
	}

	// Multiple instances - show selection modal
	s.app.QueueUpdateDraw(func() {
		s.kb.SetMode(ModeModal)
		ShowMIGInstanceSelectionModal(s.app, migName, instances,
			func(selected MIGInstanceSelection) {
				// User selected an instance, now connect to it
				s.status.SetText(fmt.Sprintf(" [yellow]Connecting to %s...[-]", selected.Name))
				go s.connectToMIGInstance(projectClient, project, gcp.ManagedInstanceRef{
					Name:   selected.Name,
					Zone:   selected.Zone,
					Status: selected.Status,
				})
			},
			func() {
				// User cancelled
				s.restoreMainView()
			},
		)
	})
}

func (s *tuiState) connectToMIGInstance(client *gcp.Client, project string, instRef gcp.ManagedInstanceRef) {
	// Fetch full instance details to check for IAP capability
	instance, err := client.FindInstance(s.ctx, instRef.Name, instRef.Zone)
	if err != nil {
		s.app.QueueUpdateDraw(func() {
			s.flashStatus(fmt.Sprintf(" [red]Error fetching instance: %v[-]", err), 3*time.Second)
		})
		return
	}

	s.app.QueueUpdateDraw(func() {
		cachedIAP := LoadIAPPreference(instance.Name)
		cachedSSHFlags := LoadSSHFlags(instance.Name, project)
		s.showSSHModal(instance.Name, project, instance.Zone, instance.CanUseIAP, cachedIAP, cachedSSHFlags)
	})
}

func (s *tuiState) showSSHModal(instName, instProject, instZone string, defaultUseIAP bool, cachedIAP *bool, cachedSSHFlags []string) {
	s.kb.SetMode(ModeModal)
	ShowSSHOptionsModal(s.app, instName, defaultUseIAP, cachedIAP, cachedSSHFlags,
		func(opts SSHOptions) {
			s.app.SetRoot(s.flex, true)
			s.app.SetFocus(s.table)
			s.kb.SetMode(ModeNormal)

			RunSSHSession(s.app, instName, instProject, instZone, opts, s.outputRedir)

			s.flashStatus(fmt.Sprintf(statusDisconnected, instName), 3*time.Second)
		},
		func() {
			s.restoreMainView()
		},
	)
}

func (s *tuiState) actionShowDetails() bool {
	inst := s.getSelectedInstance()
	if inst == nil {
		s.flashStatus(statusNoSelection, 2*time.Second)
		return true
	}

	s.status.SetText(statusLoadingDetails)
	go s.loadAndShowDetails(inst)
	return true
}

func (s *tuiState) loadAndShowDetails(inst *instanceData) {
	projectClient, err := gcp.NewClient(s.ctx, inst.Project)
	if err != nil {
		s.app.QueueUpdateDraw(func() {
			s.flashStatus(fmt.Sprintf(statusErrorClient, err), 3*time.Second)
		})
		return
	}

	if inst.IsMIG {
		// Fetch MIG details
		mig, err := projectClient.GetManagedInstanceGroup(s.ctx, inst.Name, inst.Zone)
		if err != nil {
			s.app.QueueUpdateDraw(func() {
				s.flashStatus(fmt.Sprintf(statusErrorDetails, err), 3*time.Second)
			})
			return
		}

		// Also fetch the instances in the MIG
		instances, _, _ := projectClient.ListMIGInstances(s.ctx, inst.Name, inst.Zone)

		s.app.QueueUpdateDraw(func() {
			detailText := FormatMIGDetailsLive(mig, inst.Project, instances)
			s.showDetailView(detailText, fmt.Sprintf(" MIG: %s ", inst.Name))
		})
	} else {
		// Fetch instance details
		gcpInst, err := projectClient.FindInstance(s.ctx, inst.Name, inst.Zone)
		if err != nil {
			s.app.QueueUpdateDraw(func() {
				s.flashStatus(fmt.Sprintf(statusErrorDetails, err), 3*time.Second)
			})
			return
		}

		s.app.QueueUpdateDraw(func() {
			detailText := FormatInstanceDetails(gcpInst, inst.Project)
			s.showDetailView(detailText, fmt.Sprintf(" Instance: %s ", inst.Name))
		})
	}
}

func (s *tuiState) showDetailView(detailText, title string) {
	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(detailText).
		SetScrollable(true).
		SetWordWrap(true)
	detailView.SetBorder(true).SetTitle(title)

	detailStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Esc[-] back")

	detailFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(detailView, 0, 1, true).
		AddItem(detailStatus, 1, 0, false)

	detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			s.restoreMainView()
			return nil
		}
		return event
	})

	s.kb.SetMode(ModeModal)
	s.app.SetRoot(detailFlex, true).SetFocus(detailView)
}

func (s *tuiState) actionEnterFilterMode() bool {
	s.kb.SetMode(ModeFilter)
	s.filterInput.SetText(s.currentFilter)
	s.flex.Clear()
	s.flex.AddItem(s.filterInput, 1, 0, true)
	s.flex.AddItem(s.table, 0, 1, false)
	s.flex.AddItem(s.status, 1, 0, false)
	s.app.SetFocus(s.filterInput)
	s.status.SetText(statusFilterMode)
	return true
}

func (s *tuiState) actionRefreshProjects() bool {
	s.kb.SetMode(ModeModal)
	showProjectRefreshPopupDirect(s.app, s.cache,
		func(selectedProjects []string) {
			s.kb.SetMode(ModeNormal)
			refreshCtx, cancelRefresh := context.WithCancel(s.ctx)

			_, spinnerDone := showLoadingScreen(s.app, " Refreshing Projects ", fmt.Sprintf("Refreshing %d project(s)...", len(selectedProjects)), func() {
				cancelRefresh()
				s.restoreMainView()
				s.flashStatus(" [yellow]Refresh cancelled[-]", 2*time.Second)
			})

			go func() {
				for _, project := range selectedProjects {
					if refreshCtx.Err() != nil {
						break
					}
					projectClient, err := gcp.NewClient(s.ctx, project)
					if err != nil {
						continue
					}
					_, _ = projectClient.ScanProjectResources(s.ctx, nil)
				}

				s.loadInstances(false)

				s.app.QueueUpdateDraw(func() {
					select {
					case spinnerDone <- true:
					default:
					}
					if refreshCtx.Err() == context.Canceled {
						return
					}
					s.app.SetRoot(s.flex, true).SetFocus(s.table)
					s.updateTable()
					s.flashStatus(fmt.Sprintf(" [green]Refreshed %d project(s)[-]", len(selectedProjects)), 2*time.Second)
				})
			}()
		},
		func() {
			s.restoreMainView()
		},
	)
	return true
}

func (s *tuiState) actionShowHelp() bool {
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
  [white]b[-]             Open in Cloud Console (browser)
  [white]Shift+R[-]       Refresh project data from GCP

[yellow]Views[-]
  [white]v[-]             Switch to VPN view
  [white]c[-]             Switch to connectivity tests view
  [white]Shift+S[-]       Switch to global search view
  [white]i[-]             Switch to IP lookup view

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

	helpStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Esc[-] back  [yellow]?[-] close help")

	helpFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(helpView, 0, 1, true).
		AddItem(helpStatus, 1, 0, false)

	helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == '?') {
			s.restoreMainView()
			return nil
		}
		return event
	})

	s.kb.SetMode(ModeHelp)
	s.app.SetRoot(helpFlex, true).SetFocus(helpView)
	return true
}

func (s *tuiState) actionSwitchToVPN() bool {
	err := RunVPNView(s.ctx, s.cache, s.app, func() {
		s.restoreMainView()
	})
	if err != nil {
		s.flashStatus(fmt.Sprintf(statusErrorVPN, err), 3*time.Second)
	}
	return true
}

func (s *tuiState) actionSwitchToConnectivity() bool {
	err := RunConnectivityView(s.ctx, s.cache, s.app, func() {
		s.restoreMainView()
	})
	if err != nil {
		s.flashStatus(fmt.Sprintf(statusErrorConnectivity, err), 3*time.Second)
	}
	return true
}

func (s *tuiState) actionSwitchToSearch() bool {
	s.status.SetText(" [yellow]Loading search view...[-]")
	go func() {
		err := RunSearchView(s.ctx, s.cache, s.app, s.outputRedir, s.parallelism, func() {
			s.restoreMainView()
		})
		s.app.QueueUpdateDraw(func() {
			if err != nil {
				s.app.SetRoot(s.flex, true).SetFocus(s.table)
				s.flashStatus(fmt.Sprintf(" [red]Error loading search view: %v[-]", err), 3*time.Second)
			}
		})
	}()
	return true
}

func (s *tuiState) actionSwitchToIPLookup() bool {
	s.status.SetText(" [yellow]Loading IP lookup view...[-]")
	go func() {
		err := RunIPLookupView(s.ctx, s.cache, s.app, s.parallelism, func() {
			s.restoreMainView()
		})
		s.app.QueueUpdateDraw(func() {
			if err != nil {
				s.app.SetRoot(s.flex, true).SetFocus(s.table)
				s.flashStatus(fmt.Sprintf(" [red]Error loading IP lookup view: %v[-]", err), 3*time.Second)
			}
		})
	}()
	return true
}

func (s *tuiState) actionClearFilterOrQuit() bool {
	if s.currentFilter != "" {
		s.currentFilter = ""
		s.filterInput.SetText("")
		s.updateTable()
		s.updateStatusBar() // Show full keybindings legend
		return true
	}
	s.app.Stop()
	return true
}

func (s *tuiState) actionOpenInBrowser() bool {
	inst := s.getSelectedInstance()
	if inst == nil {
		s.flashStatus(statusNoSelection, 2*time.Second)
		return true
	}

	// Build Cloud Console URL for the instance
	var url string
	if inst.IsMIG {
		// MIG URL
		url = fmt.Sprintf("https://console.cloud.google.com/compute/instanceGroups/details/%s/%s?project=%s",
			inst.Zone, inst.Name, inst.Project)
	} else {
		// Instance URL
		url = fmt.Sprintf("https://console.cloud.google.com/compute/instancesDetail/zones/%s/instances/%s?project=%s",
			inst.Zone, inst.Name, inst.Project)
	}

	if err := OpenInBrowser(url); err != nil {
		s.flashStatus(fmt.Sprintf(" [red]Error opening browser: %v[-]", err), 3*time.Second)
		return true
	}

	s.flashStatus(" [green]Opened in browser[-]", 2*time.Second)
	return true
}

// setupKeybindings configures all keyboard shortcuts
func (s *tuiState) setupKeybindings() {
	s.kb = NewKeyBindings()

	// Instance actions (normal mode only)
	s.kb.RegisterKey('s', "SSH to instance", []ViewMode{ModeNormal}, s.actionSSH)
	s.kb.RegisterKey('d', "Show details", []ViewMode{ModeNormal}, s.actionShowDetails)
	s.kb.RegisterKey('b', "Open in browser", []ViewMode{ModeNormal}, s.actionOpenInBrowser)
	s.kb.RegisterKey('/', "Filter", []ViewMode{ModeNormal}, s.actionEnterFilterMode)
	s.kb.RegisterKey('R', "Refresh projects", []ViewMode{ModeNormal}, s.actionRefreshProjects)
	s.kb.RegisterKey('?', "Show help", []ViewMode{ModeNormal}, s.actionShowHelp)

	// View switching (normal mode only)
	s.kb.RegisterKey('v', "VPN view", []ViewMode{ModeNormal}, s.actionSwitchToVPN)
	s.kb.RegisterKey('c', "Connectivity view", []ViewMode{ModeNormal}, s.actionSwitchToConnectivity)
	s.kb.RegisterKey('S', "Search view", []ViewMode{ModeNormal}, s.actionSwitchToSearch)
	s.kb.RegisterKey('i', "IP lookup view", []ViewMode{ModeNormal}, s.actionSwitchToIPLookup)

	// Quit (normal mode)
	s.kb.RegisterKey('q', "Quit", []ViewMode{ModeNormal}, func() bool {
		s.app.Stop()
		return true
	})

	// Global keys
	s.kb.RegisterSpecial(tcell.KeyCtrlC, "Quit", nil, func() bool {
		s.app.Stop()
		return true
	})

	s.kb.RegisterSpecial(tcell.KeyEscape, "Back/Quit", []ViewMode{ModeNormal}, s.actionClearFilterOrQuit)
}

// createInputCapture creates the main input capture function
func (s *tuiState) createInputCapture() func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		// In modal or filter mode, only handle Ctrl+C globally
		mode := s.kb.GetMode()
		if mode == ModeModal || mode == ModeFilter {
			if event.Key() == tcell.KeyCtrlC {
				s.app.Stop()
				return nil
			}
			return event // Let the modal/filter handle other keys
		}

		// Don't process during refresh
		if s.isRefreshing {
			return event
		}

		// Try keybindings
		if s.kb.Handle(event) {
			return nil
		}

		return event
	}
}

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
	IsMIG       bool // Whether this is a MIG (Managed Instance Group) rather than a single instance
}

// RunDirect runs a minimal TUI without the full app structure
func RunDirect(c *cache.Cache, parallelism int) error {
	// Disable logging to prevent log output from corrupting the TUI
	logger.Log.Disable()
	defer logger.Log.Restore()

	// Redirect stdout/stderr to prevent any remaining output from corrupting the TUI
	outputRedir := newOutputRedirector()
	defer outputRedir.Restore()

	// Initialize state
	s := &tuiState{
		app:         tview.NewApplication(),
		cache:       c,
		ctx:         context.Background(),
		outputRedir: outputRedir,
		parallelism: parallelism,
	}

	// Create UI components
	s.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	// Add header
	headers := []string{"Name", "Project", "Zone", "Type", "Status"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorBlack).
			SetBackgroundColor(tcell.ColorDarkCyan).
			SetSelectable(false).
			SetExpansion(1)
		s.table.SetCell(0, col, cell)
	}

	// Filter input field
	s.filterInput = tview.NewInputField().
		SetLabel(" Filter: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow)

	// Status bar
	s.status = tview.NewTextView().
		SetDynamicColors(true).
		SetText(statusDefault)

	// Layout
	s.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(s.table, 0, 1, true).
		AddItem(s.status, 1, 0, false)

	// Setup filter input handlers
	s.filterInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			s.currentFilter = s.filterInput.GetText()
			s.updateTable()
			s.kb.SetMode(ModeNormal)
			s.flex.Clear()
			s.flex.AddItem(s.table, 0, 1, true)
			s.flex.AddItem(s.status, 1, 0, false)
			s.app.SetFocus(s.table)
			s.updateStatusBar()
		case tcell.KeyEscape:
			s.filterInput.SetText(s.currentFilter)
			s.kb.SetMode(ModeNormal)
			s.flex.Clear()
			s.flex.AddItem(s.table, 0, 1, true)
			s.flex.AddItem(s.status, 1, 0, false)
			s.app.SetFocus(s.table)
			s.updateStatusBar()
		}
	})

	// Update table as user types (live fuzzy search)
	s.filterInput.SetChangedFunc(func(text string) {
		s.currentFilter = text
		s.updateTable()
	})

	// Load initial data
	s.loadInstances(false)
	s.updateTable()
	s.table.SetBorder(true)

	// Setup keybindings
	s.setupKeybindings()

	// Set input capture
	s.app.SetInputCapture(s.createInputCapture())

	// Run
	if err := s.app.SetRoot(s.flex, true).EnableMouse(true).Run(); err != nil {
		return fmt.Errorf("tview run error: %w", err)
	}

	return nil
}

// showProjectRefreshPopupDirect displays a popup to select projects for refresh in direct mode
func showProjectRefreshPopupDirect(
	app *tview.Application,
	cache *cache.Cache,
	onComplete func(selectedProjects []string),
	onCancel func(),
) {
	// Get projects sorted by usage
	projects := cache.GetProjectsByUsage()

	if len(projects) == 0 {
		// No projects in cache, show error modal
		modal := tview.NewModal().
			SetText("No projects found in cache.\n\nRun 'compass gcp projects import' to import projects first.").
			AddButtons([]string{"OK"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				if onCancel != nil {
					onCancel()
				}
			})
		app.SetRoot(modal, true).SetFocus(modal)
		return
	}

	// Track selected projects
	selected := make(map[string]bool)
	allSelected := false

	// Create the list with checkboxes
	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true)

	list.SetBorder(true).SetTitle(" Select Projects to Refresh ")

	// Function to update the list display
	updateList := func() {
		list.Clear()

		// Add "All Projects" option first
		allCheckbox := "[ ]"
		if allSelected {
			allCheckbox = "[x]"
		}
		list.AddItem(fmt.Sprintf("%s All Projects (%d)", allCheckbox, len(projects)), "", 0, nil)

		// Add individual projects
		for _, project := range projects {
			checkbox := "[ ]"
			if selected[project] || allSelected {
				checkbox = "[x]"
			}
			list.AddItem(fmt.Sprintf("%s %s", checkbox, project), "", 0, nil)
		}
	}

	updateList()

	// Status bar
	statusText := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Space[-] toggle  [yellow]Enter[-] refresh  [yellow]Esc[-] cancel")

	// Main layout
	contentFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(statusText, 1, 0, false)

	// Create centered modal container
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(contentFlex, 20, 0, true).
			AddItem(nil, 0, 1, false), 60, 0, true).
		AddItem(nil, 0, 1, false)

	// Handle keyboard input
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			if onCancel != nil {
				onCancel()
			}
			return nil

		case tcell.KeyEnter:
			// Gather selected projects
			var toRefresh []string
			if allSelected {
				toRefresh = projects
			} else {
				for project := range selected {
					toRefresh = append(toRefresh, project)
				}
			}

			if len(toRefresh) == 0 {
				// Flash a message - no projects selected
				statusText.SetText(" [red]No projects selected! Use Space to select.[-]")
				return nil
			}

			// Call the completion callback with selected projects
			if onComplete != nil {
				onComplete(toRefresh)
			}
			return nil

		case tcell.KeyRune:
			if event.Rune() == ' ' {
				// Toggle selection
				idx := list.GetCurrentItem()
				if idx == 0 {
					// Toggle "All"
					allSelected = !allSelected
					if allSelected {
						// Clear individual selections when "All" is selected
						selected = make(map[string]bool)
					}
				} else if idx > 0 && idx <= len(projects) {
					project := projects[idx-1]
					if allSelected {
						// Deselect "All" when individual is toggled
						allSelected = false
						// Select all except the toggled one
						for _, p := range projects {
							if p != project {
								selected[p] = true
							}
						}
					} else {
						if selected[project] {
							delete(selected, project)
						} else {
							selected[project] = true
						}
					}
				}
				updateList()
				list.SetCurrentItem(idx)
				return nil
			}
		}
		return event
	})

	app.SetRoot(modal, true).SetFocus(list)
}
