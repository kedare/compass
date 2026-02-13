package tui

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/rivo/tview"
)

// Status bar text constants for IP lookup view
const (
	ipStatusDefault      = " [yellow]Enter[-] lookup  [yellow]Esc[-] back  [yellow]/[-] filter results  [yellow]d[-] details  [yellow]?[-] help"
	ipStatusSearching    = " [yellow]Looking up IP...[-]  [yellow]Esc[-] cancel"
	ipStatusFilterPrompt = " [yellow]Filter: spaces=AND  |=OR  -=NOT  (e.g. \"web|api -dev\")  Enter to apply, Esc to cancel[-]"
	ipStatusHelpClose    = " [yellow]Esc[-] back  [yellow]?[-] close help"
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

// ipLookupViewState encapsulates all state for the IP lookup view
type ipLookupViewState struct {
	ctx         context.Context
	cache       *cache.Cache
	app         *tview.Application
	parallelism int
	onBack      func()

	// Data
	allProjects []string
	allResults  []ipLookupEntry
	resultsMu   sync.Mutex

	// UI components
	ipInput      *tview.InputField
	table        *tview.Table
	filterInput  *tview.InputField
	status       *tview.TextView
	progressText *tview.TextView
	flex         *tview.Flex
	statusFlex   *tview.Flex

	// State flags
	isSearching   bool
	searchCancel  context.CancelFunc
	modalOpen     bool
	currentFilter string
	filterMode    bool
}

// getPreferredProjectsForIP returns projects whose cached subnets contain the IP
func getPreferredProjectsForIP(c *cache.Cache, ip net.IP) []string {
	if ip == nil || c == nil {
		return nil
	}

	entries := c.FindSubnetsForIP(ip)
	if len(entries) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(entries))
	ordered := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}

		projectID := strings.TrimSpace(entry.Project)
		if projectID == "" {
			continue
		}

		key := strings.ToLower(projectID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		ordered = append(ordered, projectID)
	}

	return ordered
}

// RunIPLookupView shows the IP lookup interface with progressive results
func RunIPLookupView(ctx context.Context, c *cache.Cache, app *tview.Application, parallelism int, onBack func()) error {
	allProjects := c.GetProjectsByUsage()
	if len(allProjects) == 0 {
		return fmt.Errorf("no projects in cache")
	}

	s := &ipLookupViewState{
		ctx:         ctx,
		cache:       c,
		app:         app,
		parallelism: parallelism,
		onBack:      onBack,
		allProjects: allProjects,
	}

	s.setupUI()
	s.setupKeyboardHandlers()

	app.SetRoot(s.flex, true).EnableMouse(true).SetFocus(s.ipInput)
	return nil
}

// setupUI initializes all UI components
func (s *ipLookupViewState) setupUI() {
	s.ipInput = tview.NewInputField().
		SetLabel(" IP Address: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow).
		SetPlaceholder("Enter IP address and press Enter")

	s.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	s.table.SetBorder(true).SetTitle(" IP Lookup Results (0) ")

	headers := []string{"Kind", "Resource", "Project", "Location", "Details"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorBlack).
			SetBackgroundColor(tcell.ColorDarkCyan).
			SetSelectable(false).
			SetExpansion(1)
		s.table.SetCell(0, col, cell)
	}

	s.filterInput = tview.NewInputField().
		SetLabel(" Filter: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow)

	s.status = tview.NewTextView().
		SetDynamicColors(true).
		SetText(ipStatusDefault)

	s.progressText = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignRight)

	s.statusFlex = tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(s.status, 0, 3, false).
		AddItem(s.progressText, 0, 1, false)

	s.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(s.ipInput, 1, 0, true).
		AddItem(s.table, 0, 1, false).
		AddItem(s.statusFlex, 1, 0, false)

	s.table.SetSelectionChangedFunc(func(row, column int) {
		if !s.modalOpen && !s.filterMode {
			s.updateStatusWithActions()
		}
	})

	s.ipInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			ipAddress := strings.TrimSpace(s.ipInput.GetText())
			if ipAddress != "" {
				s.app.SetFocus(s.table)
				go s.performLookup(ipAddress)
			}
		case tcell.KeyEscape:
			if s.isSearching && s.searchCancel != nil {
				s.searchCancel()
			} else {
				s.onBack()
			}
		}
	})

	s.filterInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			s.currentFilter = s.filterInput.GetText()
			s.updateTable(s.currentFilter)
			s.exitFilterMode()
		case tcell.KeyEscape:
			s.filterInput.SetText(s.currentFilter)
			s.exitFilterMode()
		}
	})
}

// setupKeyboardHandlers sets up all keyboard event handlers
func (s *ipLookupViewState) setupKeyboardHandlers() {
	s.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if s.modalOpen && event.Key() != tcell.KeyCtrlC {
			return event
		}
		if s.filterMode {
			return event
		}
		if s.app.GetFocus() == s.ipInput {
			return event
		}

		switch event.Key() {
		case tcell.KeyEscape:
			return s.handleEscape()
		case tcell.KeyEnter:
			s.app.SetFocus(s.ipInput)
			return nil
		case tcell.KeyRune:
			return s.handleRuneKey(event.Rune())
		}
		return event
	})
}

// handleEscape handles the Escape key
func (s *ipLookupViewState) handleEscape() *tcell.EventKey {
	if s.isSearching && s.searchCancel != nil {
		s.searchCancel()
		return nil
	}
	if s.currentFilter != "" {
		s.currentFilter = ""
		s.filterInput.SetText("")
		s.updateTable("")
		s.showTemporaryStatus(" [yellow]Filter cleared[-]")
		return nil
	}
	if s.searchCancel != nil {
		s.searchCancel()
	}
	s.onBack()
	return nil
}

// handleRuneKey handles character key presses
func (s *ipLookupViewState) handleRuneKey(r rune) *tcell.EventKey {
	switch r {
	case '/':
		s.enterFilterMode()
	case 'd':
		s.showDetails()
	case 'b':
		s.openInBrowser()
	case '?':
		s.showHelp()
	default:
		return &tcell.EventKey{}
	}
	return nil
}

// enterFilterMode enters filter/search mode
func (s *ipLookupViewState) enterFilterMode() {
	s.filterMode = true
	s.filterInput.SetText(s.currentFilter)
	s.rebuildLayout(true, false)
	s.app.SetFocus(s.filterInput)
	s.status.SetText(ipStatusFilterPrompt)
}

// exitFilterMode exits filter mode and restores normal view
func (s *ipLookupViewState) exitFilterMode() {
	s.filterMode = false
	s.rebuildLayout(false, true)
	s.app.SetFocus(s.table)
	s.updateStatusWithActions()
}

// rebuildLayout rebuilds the flex layout
func (s *ipLookupViewState) rebuildLayout(showFilter bool, focusTable bool) {
	s.flex.Clear()
	s.flex.AddItem(s.ipInput, 1, 0, !showFilter && !focusTable)
	if showFilter {
		s.flex.AddItem(s.filterInput, 1, 0, true)
	}
	s.flex.AddItem(s.table, 0, 1, focusTable)
	s.flex.AddItem(s.statusFlex, 1, 0, false)
}

// updateTable updates the table with current results and filter
func (s *ipLookupViewState) updateTable(filter string) {
	s.resultsMu.Lock()
	results := make([]ipLookupEntry, len(s.allResults))
	copy(results, s.allResults)
	s.resultsMu.Unlock()

	s.updateTableWithData(filter, results)
}

// updateTableWithData updates the table display with given results
func (s *ipLookupViewState) updateTableWithData(filter string, results []ipLookupEntry) {
	currentSelectedRow, _ := s.table.GetSelection()
	var selectedKey string
	if currentSelectedRow > 0 && currentSelectedRow < s.table.GetRowCount() {
		resourceCell := s.table.GetCell(currentSelectedRow, 1)
		projectCell := s.table.GetCell(currentSelectedRow, 2)
		locationCell := s.table.GetCell(currentSelectedRow, 3)
		if resourceCell != nil && projectCell != nil && locationCell != nil {
			selectedKey = resourceCell.Text + "|" + projectCell.Text + "|" + locationCell.Text
		}
	}

	for row := s.table.GetRowCount() - 1; row > 0; row-- {
		s.table.RemoveRow(row)
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
		displayDetails := entry.Details
		if len(displayDetails) > 50 {
			displayDetails = displayDetails[:47] + "..."
		}

		s.table.SetCell(currentRow, 0, tview.NewTableCell(fmt.Sprintf("[%s]%s[-]", kindColor, entry.Kind)).SetExpansion(1))
		s.table.SetCell(currentRow, 1, tview.NewTableCell(entry.Resource).SetExpansion(1))
		s.table.SetCell(currentRow, 2, tview.NewTableCell(entry.Project).SetExpansion(1))
		s.table.SetCell(currentRow, 3, tview.NewTableCell(entry.Location).SetExpansion(1))
		s.table.SetCell(currentRow, 4, tview.NewTableCell(displayDetails).SetExpansion(2))

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
		s.table.SetTitle(fmt.Sprintf(" IP Lookup Results (%d/%d matched) ", matchCount, len(results)))
	} else {
		s.table.SetTitle(fmt.Sprintf(" IP Lookup Results (%d) ", len(results)))
	}

	if matchCount > 0 && s.table.GetRowCount() > 1 {
		if newSelectedRow > 0 {
			s.table.Select(newSelectedRow, 0)
		} else if currentSelectedRow > 0 && currentSelectedRow < s.table.GetRowCount() {
			s.table.Select(currentSelectedRow, 0)
		} else if currentSelectedRow >= s.table.GetRowCount() && s.table.GetRowCount() > 1 {
			s.table.Select(s.table.GetRowCount()-1, 0)
		} else if currentSelectedRow == 0 {
			s.table.Select(1, 0)
		}
	}
}

// getSelectedEntry returns the currently selected entry
func (s *ipLookupViewState) getSelectedEntry() *ipLookupEntry {
	row, _ := s.table.GetSelection()
	if row <= 0 {
		return nil
	}

	s.resultsMu.Lock()
	defer s.resultsMu.Unlock()

	expr := parseFilter(s.currentFilter)
	visibleIdx := 0
	for i := range s.allResults {
		entry := &s.allResults[i]
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

// updateStatusWithActions updates the status bar with context-aware actions
func (s *ipLookupViewState) updateStatusWithActions() {
	s.resultsMu.Lock()
	count := len(s.allResults)
	s.resultsMu.Unlock()

	entry := s.getSelectedEntry()

	if s.isSearching {
		s.status.SetText(ipStatusSearching)
		return
	}

	if entry == nil {
		if count > 0 {
			s.status.SetText(fmt.Sprintf(" [green]%d results[-]  [yellow]Enter[-] lookup  [yellow]/[-] filter  [yellow]Esc[-] back  [yellow]?[-] help", count))
		} else {
			s.status.SetText(ipStatusDefault)
		}
		return
	}

	actionStr := "[yellow]d[-] details  [yellow]b[-] browser"
	if s.currentFilter != "" {
		s.status.SetText(fmt.Sprintf(" [green]Filter: %s[-]  %s  [yellow]/[-] edit  [yellow]Esc[-] clear  [yellow]?[-] help", s.currentFilter, actionStr))
	} else {
		s.status.SetText(fmt.Sprintf(" %s  [yellow]Enter[-] lookup  [yellow]/[-] filter  [yellow]Esc[-] back  [yellow]?[-] help", actionStr))
	}
}

// showTemporaryStatus shows a temporary status message that auto-restores after 2 seconds
func (s *ipLookupViewState) showTemporaryStatus(message string) {
	s.status.SetText(message)
	time.AfterFunc(2*time.Second, func() {
		s.app.QueueUpdateDraw(func() {
			s.updateStatusWithActions()
		})
	})
}

// performLookup performs IP lookup across projects with two-phase search
func (s *ipLookupViewState) performLookup(ipAddress string) {
	if ipAddress == "" {
		return
	}

	if s.searchCancel != nil {
		s.searchCancel()
	}

	s.resultsMu.Lock()
	s.allResults = []ipLookupEntry{}
	s.resultsMu.Unlock()

	lookupCtx, cancel := context.WithCancel(s.ctx)
	s.searchCancel = cancel
	s.isSearching = true

	// Phase 1: Try preferred projects from cache
	parsedIP := net.ParseIP(strings.TrimSpace(ipAddress))
	preferredProjects := getPreferredProjectsForIP(s.cache, parsedIP)

	projects := s.allProjects
	if len(preferredProjects) > 0 {
		projects = preferredProjects
	}

	s.app.QueueUpdateDraw(func() {
		s.updateStatusWithActions()
		s.progressText.SetText("[yellow]Starting IP lookup...[-]")
	})

	s.searchProjects(lookupCtx, projects, ipAddress, "Looking up IP...")

	// Phase 2: Fallback to all projects if preferred search found nothing
	s.resultsMu.Lock()
	foundResults := len(s.allResults) > 0
	s.resultsMu.Unlock()

	if !foundResults && len(preferredProjects) > 0 && len(preferredProjects) < len(s.allProjects) {
		s.app.QueueUpdateDraw(func() {
			s.progressText.SetText("[yellow]No results in cached projects, searching all projects...[-]")
		})
		s.searchProjects(lookupCtx, s.allProjects, ipAddress, "Full scan...")
	}

	s.isSearching = false

	// Sort final results
	s.resultsMu.Lock()
	sort.Slice(s.allResults, func(i, j int) bool {
		if s.allResults[i].Project != s.allResults[j].Project {
			return s.allResults[i].Project < s.allResults[j].Project
		}
		if s.allResults[i].Kind != s.allResults[j].Kind {
			return s.allResults[i].Kind < s.allResults[j].Kind
		}
		return s.allResults[i].Resource < s.allResults[j].Resource
	})
	s.resultsMu.Unlock()

	filter := s.currentFilter
	s.app.QueueUpdateDraw(func() {
		s.progressText.SetText("")
		s.rebuildLayout(false, true)
		s.updateTable(filter)
		s.app.SetFocus(s.table)
		s.updateStatusWithActions()
	})
}

// searchProjects searches the given projects in parallel and updates results progressively
func (s *ipLookupViewState) searchProjects(lookupCtx context.Context, projects []string, ipAddress string, statusPrefix string) {
	var (
		completedProjects int
		totalProjects     = len(projects)
		progressMu        sync.Mutex
	)

	spinnerDone := s.runSearchSpinner(lookupCtx, &progressMu, &completedProjects, totalProjects, statusPrefix)

	sem := make(chan struct{}, s.parallelism)
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

			associations, err := client.LookupIPAddressFast(lookupCtx, ipAddress)
			if err != nil {
				progressMu.Lock()
				completedProjects++
				progressMu.Unlock()
				return
			}

			if len(associations) > 0 {
				newEntries := make([]ipLookupEntry, 0, len(associations))
				for _, assoc := range associations {
					newEntries = append(newEntries, ipLookupEntry{
						Kind:         string(assoc.Kind),
						Resource:     assoc.Resource,
						Project:      assoc.Project,
						Location:     assoc.Location,
						IPAddress:    assoc.IPAddress,
						Details:      assoc.Details,
						ResourceLink: assoc.ResourceLink,
					})
				}

				s.resultsMu.Lock()
				s.allResults = append(s.allResults, newEntries...)
				s.resultsMu.Unlock()

				filter := s.currentFilter
				s.app.QueueUpdateDraw(func() {
					s.updateTable(filter)
				})
			}

			progressMu.Lock()
			completedProjects++
			progressMu.Unlock()
		}(project)
	}

	wg.Wait()

	select {
	case spinnerDone <- true:
	default:
	}
}

// runSearchSpinner starts a spinner goroutine and returns a channel to signal completion
func (s *ipLookupViewState) runSearchSpinner(lookupCtx context.Context, progressMu *sync.Mutex, completedProjects *int, totalProjects int, statusPrefix string) chan bool {
	spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinnerIdx := 0
	done := make(chan bool, 2)

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.resultsMu.Lock()
				count := len(s.allResults)
				s.resultsMu.Unlock()
				progressMu.Lock()
				completed := *completedProjects
				progressMu.Unlock()
				frame := spinnerFrames[spinnerIdx]
				spinnerIdx = (spinnerIdx + 1) % len(spinnerFrames)
				s.app.QueueUpdateDraw(func() {
					if s.isSearching {
						s.progressText.SetText(fmt.Sprintf("[yellow]%s %d/%d projects | %d results[-]", frame, completed, totalProjects, count))
						s.status.SetText(fmt.Sprintf(" [yellow]%s (%d/%d projects)[-]  [yellow]Esc[-] cancel", statusPrefix, completed, totalProjects))
					}
				})
			case <-done:
				return
			case <-lookupCtx.Done():
				return
			}
		}
	}()

	return done
}

// openInBrowser opens the selected resource in the Cloud Console
func (s *ipLookupViewState) openInBrowser() {
	entry := s.getSelectedEntry()
	if entry == nil {
		return
	}

	url := getIPLookupConsoleURL(entry)
	if err := OpenInBrowser(url); err != nil {
		s.status.SetText(fmt.Sprintf(" [yellow]URL: %s[-]", url))
	} else {
		s.showTemporaryStatus(" [green]Opened in browser[-]")
	}
}

// showDetails shows detailed information for the selected entry
func (s *ipLookupViewState) showDetails() {
	entry := s.getSelectedEntry()
	if entry == nil {
		s.showTemporaryStatus(" [red]No result selected[-]")
		return
	}

	s.modalOpen = true
	showDetailModal(s.app, entry.Resource, s.buildDetailText(entry), func() {
		s.modalOpen = false
		s.app.SetRoot(s.flex, true)
		s.app.SetFocus(s.table)
		s.updateStatusWithActions()
	})
}

// buildDetailText builds the detail text for an IP lookup result
func (s *ipLookupViewState) buildDetailText(entry *ipLookupEntry) string {
	var b strings.Builder

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

	b.WriteString(fmt.Sprintf("[yellow::b]%s[-:-:-]\n\n", kindDisplay))
	b.WriteString(fmt.Sprintf("[white::b]Resource:[-:-:-]   %s\n", entry.Resource))
	b.WriteString(fmt.Sprintf("[white::b]Project:[-:-:-]    %s\n", entry.Project))
	b.WriteString(fmt.Sprintf("[white::b]Location:[-:-:-]   %s\n", entry.Location))
	b.WriteString(fmt.Sprintf("[white::b]IP Address:[-:-:-] %s\n", entry.IPAddress))

	if entry.Details != "" {
		b.WriteString("\n[yellow::b]Details:[-:-:-]\n")
		pairs := strings.Split(entry.Details, ", ")
		for _, pair := range pairs {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				b.WriteString(fmt.Sprintf("  [white::b]%s:[-:-:-] %s\n", parts[0], parts[1]))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n", pair))
			}
		}
	}

	if entry.ResourceLink != "" {
		b.WriteString(fmt.Sprintf("\n[white::b]Resource Link:[-:-:-]\n  %s\n", entry.ResourceLink))
	}

	b.WriteString("\n[darkgray]Press Esc to close[-]")
	return b.String()
}

// showHelp displays the help screen
func (s *ipLookupViewState) showHelp() {
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

	s.modalOpen = true
	helpView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)
	helpView.SetBorder(true).
		SetTitle(" IP Lookup Help ").
		SetTitleAlign(tview.AlignCenter)

	helpStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(ipStatusHelpClose)

	helpFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(helpView, 0, 1, true).
		AddItem(helpStatus, 1, 0, false)

	helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == '?') {
			s.modalOpen = false
			s.app.SetRoot(s.flex, true)
			s.app.SetFocus(s.table)
			s.updateStatusWithActions()
			return nil
		}
		return event
	})

	s.app.SetRoot(helpFlex, true).SetFocus(helpView)
}

// getKindColor returns the display color for a resource kind
func getKindColor(kind string) string {
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

// getIPLookupConsoleURL returns the Cloud Console URL for an IP lookup result
func getIPLookupConsoleURL(entry *ipLookupEntry) string {
	baseURL := "https://console.cloud.google.com"

	switch entry.Kind {
	case string(gcp.IPAssociationInstanceInternal), string(gcp.IPAssociationInstanceExternal):
		return fmt.Sprintf("%s/compute/instancesDetail/zones/%s/instances/%s?project=%s",
			baseURL, entry.Location, entry.Resource, entry.Project)
	case string(gcp.IPAssociationForwardingRule):
		if entry.Location == "global" {
			return fmt.Sprintf("%s/net-services/loadbalancing/advanced/globalForwardingRules/details/%s?project=%s",
				baseURL, entry.Resource, entry.Project)
		}
		return fmt.Sprintf("%s/net-services/loadbalancing/advanced/forwardingRules/details/regions/%s/forwardingRules/%s?project=%s",
			baseURL, entry.Location, entry.Resource, entry.Project)
	case string(gcp.IPAssociationAddress):
		return fmt.Sprintf("%s/networking/addresses/list?project=%s", baseURL, entry.Project)
	case string(gcp.IPAssociationSubnet):
		return fmt.Sprintf("%s/networking/subnetworks/details/%s/%s?project=%s",
			baseURL, entry.Location, entry.Resource, entry.Project)
	default:
		return fmt.Sprintf("%s/home/dashboard?project=%s", baseURL, entry.Project)
	}
}
