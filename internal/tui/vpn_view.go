package tui

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/rivo/tview"
)

// Status bar text constants
const (
	vpnStatusDefault      = " [yellow]Esc[-] back  [yellow]b[-] browser  [yellow]d[-] details  [yellow]Shift+R[-] refresh  [yellow]/[-] filter  [yellow]?[-] help"
	vpnStatusDefaultNoR   = " [yellow]Esc[-] back  [yellow]b[-] browser  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]/[-] filter  [yellow]?[-] help"
	vpnStatusFilterActive = " [green]Filter active: %s[-]"
	vpnStatusDetailScroll = " [yellow]Esc[-] back  [yellow]up/down[-] scroll"
	vpnStatusHelpClose    = " [yellow]Esc[-] back  [yellow]?[-] close help"
)

// vpnEntry represents a row in the VPN view
type vpnEntry struct {
	Type        string // "gateway", "tunnel", "bgp", "orphan-tunnel", "orphan-bgp"
	Name        string
	Status      string
	Region      string
	Network     string
	Parent      string // Parent gateway for tunnels/BGP
	DetailedMsg string
	Level       int // Indentation level (0=gateway, 1=tunnel, 2=bgp)

	// References to actual data structures for detailed views
	Gateway *gcp.VPNGatewayInfo
	Tunnel  *gcp.VPNTunnelInfo
	BGP     *gcp.BGPSessionInfo
}

// vpnViewState encapsulates all state for the VPN view
type vpnViewState struct {
	ctx             context.Context
	gcpClient       *gcp.Client
	selectedProject string
	app             *tview.Application
	vpnData         *gcp.VPNOverview
	allEntries      []vpnEntry
	onBack          func()

	// UI components
	table       *tview.Table
	status      *tview.TextView
	filterInput *tview.InputField
	flex        *tview.Flex

	// State flags
	isRefreshing  bool
	modalOpen     bool
	filterMode    bool
	currentFilter string
}

// RunVPNView shows the VPN overview
func RunVPNView(ctx context.Context, c *cache.Cache, app *tview.Application, onBack func()) error {
	return RunVPNViewWithProgress(ctx, c, app, nil, onBack)
}

// RunVPNViewWithProgress shows the VPN overview with optional progress callback
func RunVPNViewWithProgress(ctx context.Context, c *cache.Cache, app *tview.Application, progress func(string), onBack func()) error {
	ShowProjectSelector(app, c, " Select Project for VPN View ", func(result ProjectSelectorResult) {
		if result.Cancelled {
			onBack()
			return
		}

		loadVPNDataWithSpinner(ctx, app, result.Project, progress, onBack)
	})

	return nil
}

// loadVPNDataWithSpinner loads VPN data with a loading spinner
func loadVPNDataWithSpinner(ctx context.Context, app *tview.Application, project string, progress func(string), onBack func()) {
	loadingCtx, cancelLoading := context.WithCancel(ctx)
	updateMsg, spinnerDone := showLoadingScreen(app, fmt.Sprintf(" Loading VPN Data [%s] ", project), "Creating GCP client...", func() {
		cancelLoading()
		onBack()
	})

	go func() {
		defer func() {
			select {
			case spinnerDone <- true:
			default:
			}
		}()

		// Create GCP client
		client, err := gcp.NewClient(loadingCtx, project)
		if err != nil {
			if loadingCtx.Err() == context.Canceled {
				return
			}
			showErrorModal(app, fmt.Sprintf("Failed to create GCP client for project '%s':\n\n%v", project, err), onBack)
			return
		}

		// Load VPN data
		progressCallback := createProgressCallback(updateMsg, progress)
		updateMsg("Loading VPN gateways and tunnels...")

		overview, err := client.ListVPNOverview(loadingCtx, progressCallback)

		var entries []vpnEntry
		if err != nil {
			entries = []vpnEntry{{Type: "error", Name: "Failed to load VPN data", Status: err.Error()}}
		} else {
			entries = buildVPNEntries(overview)
		}

		if loadingCtx.Err() == context.Canceled {
			return
		}

		app.QueueUpdateDraw(func() {
			showVPNViewUI(ctx, client, project, app, overview, entries, onBack)
		})
	}()
}

// createProgressCallback creates a combined progress callback
func createProgressCallback(updateMsg func(string), progress func(string)) func(string) {
	return func(msg string) {
		updateMsg(msg)
		if progress != nil {
			progress(msg)
		}
	}
}

// showErrorModal displays an error modal
func showErrorModal(app *tview.Application, message string, onOK func()) {
	app.QueueUpdateDraw(func() {
		modal := tview.NewModal().
			SetText(message).
			AddButtons([]string{"OK"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				onOK()
			})
		app.SetRoot(modal, true).SetFocus(modal)
	})
}

// buildVPNEntries converts VPN overview data to display entries
func buildVPNEntries(overview *gcp.VPNOverview) []vpnEntry {
	var allEntries []vpnEntry

	// Add gateways and their tunnels/BGP sessions
	for _, gw := range overview.Gateways {
		allEntries = append(allEntries, createGatewayEntry(gw))
		allEntries = append(allEntries, createTunnelEntries(gw)...)
	}

	// Add orphan tunnels
	if len(overview.OrphanTunnels) > 0 {
		allEntries = append(allEntries, createSectionEntry("Orphan Tunnels (Classic VPN)", len(overview.OrphanTunnels), "tunnels"))
		for _, tunnel := range overview.OrphanTunnels {
			allEntries = append(allEntries, createOrphanTunnelEntry(tunnel))
		}
	}

	// Add orphan BGP sessions
	if len(overview.OrphanSessions) > 0 {
		allEntries = append(allEntries, createSectionEntry("Orphan BGP Sessions", len(overview.OrphanSessions), "sessions"))
		for _, bgp := range overview.OrphanSessions {
			allEntries = append(allEntries, createOrphanBGPEntry(bgp))
		}
	}

	return allEntries
}

// Helper functions to create entries
func createGatewayEntry(gw *gcp.VPNGatewayInfo) vpnEntry {
	tunnelCount := len(gw.Tunnels)
	bgpCount := 0
	for _, t := range gw.Tunnels {
		bgpCount += len(t.BgpSessions)
	}

	return vpnEntry{
		Type:    "gateway",
		Name:    gw.Name,
		Status:  fmt.Sprintf("%d tunnels, %d BGP", tunnelCount, bgpCount),
		Region:  gw.Region,
		Network: extractNetworkName(gw.Network),
		Level:   0,
		Gateway: gw,
	}
}

func createTunnelEntries(gw *gcp.VPNGatewayInfo) []vpnEntry {
	var entries []vpnEntry
	for _, tunnel := range gw.Tunnels {
		entries = append(entries, vpnEntry{
			Type:        "tunnel",
			Name:        tunnel.Name,
			Status:      formatTunnelStatus(tunnel.Status),
			Region:      tunnel.Region,
			Parent:      gw.Name,
			DetailedMsg: tunnel.DetailedStatus,
			Level:       1,
			Tunnel:      tunnel,
		})

		// Add BGP sessions for this tunnel
		for _, bgp := range tunnel.BgpSessions {
			entries = append(entries, vpnEntry{
				Type:   "bgp",
				Name:   bgp.Name,
				Status: formatBGPStatus(bgp.SessionState),
				Region: bgp.Region,
				Parent: tunnel.Name,
				Level:  2,
				BGP:    bgp,
			})
		}
	}
	return entries
}

func createSectionEntry(name string, count int, itemType string) vpnEntry {
	return vpnEntry{
		Type:   "section",
		Name:   name,
		Status: fmt.Sprintf("%d %s", count, itemType),
		Level:  0,
	}
}

func createOrphanTunnelEntry(tunnel *gcp.VPNTunnelInfo) vpnEntry {
	return vpnEntry{
		Type:        "orphan-tunnel",
		Name:        tunnel.Name,
		Status:      formatTunnelStatus(tunnel.Status),
		Region:      tunnel.Region,
		DetailedMsg: tunnel.DetailedStatus,
		Level:       1,
		Tunnel:      tunnel,
	}
}

func createOrphanBGPEntry(bgp *gcp.BGPSessionInfo) vpnEntry {
	return vpnEntry{
		Type:   "orphan-bgp",
		Name:   bgp.Name,
		Status: formatBGPStatus(bgp.SessionState),
		Region: bgp.Region,
		Level:  1,
		BGP:    bgp,
	}
}

// showVPNViewUI displays the VPN view UI after data is loaded
func showVPNViewUI(ctx context.Context, gcpClient *gcp.Client, selectedProject string, app *tview.Application, vpnData *gcp.VPNOverview, initialEntries []vpnEntry, onBack func()) {
	state := &vpnViewState{
		ctx:             ctx,
		gcpClient:       gcpClient,
		selectedProject: selectedProject,
		app:             app,
		vpnData:         vpnData,
		allEntries:      initialEntries,
		onBack:          onBack,
	}

	state.setupUI()
	state.setupKeyboardHandlers()
	state.updateTable("")

	app.SetRoot(state.flex, true).EnableMouse(true).SetFocus(state.table)
}

// setupUI initializes all UI components
func (s *vpnViewState) setupUI() {
	// Create table
	s.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	s.table.SetBorder(true).SetTitle(fmt.Sprintf(" VPN Overview [yellow][%s][-] ", s.selectedProject))

	// Add header
	headers := []string{"Name", "Status", "Region", "Network"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorBlack).
			SetBackgroundColor(tcell.ColorDarkCyan).
			SetSelectable(false).
			SetExpansion(1)
		s.table.SetCell(0, col, cell)
	}

	// Filter input
	s.filterInput = tview.NewInputField().
		SetLabel(" Filter: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorYellow)
	s.setupFilterHandlers()

	// Status bar
	s.status = tview.NewTextView().
		SetDynamicColors(true).
		SetText(vpnStatusDefault)

	// Layout
	s.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(s.table, 0, 1, true).
		AddItem(s.status, 1, 0, false)
}

// setupFilterHandlers sets up filter input handlers
func (s *vpnViewState) setupFilterHandlers() {
	s.filterInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			s.currentFilter = s.filterInput.GetText()
			s.exitFilterMode()
			s.updateTable(s.currentFilter)
		case tcell.KeyEscape:
			s.filterInput.SetText(s.currentFilter)
			s.exitFilterMode()
		}
	})
}

// setupKeyboardHandlers sets up all keyboard event handlers
func (s *vpnViewState) setupKeyboardHandlers() {
	s.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Let filter input handle events when in filter mode
		if s.filterMode || s.isRefreshing {
			return event
		}

		// Let modals handle their own escape
		if s.modalOpen && event.Key() == tcell.KeyEscape {
			return event
		}

		switch event.Key() {
		case tcell.KeyEscape:
			return s.handleEscape()
		case tcell.KeyRune:
			return s.handleRuneKey(event.Rune())
		}
		return event
	})
}

// handleEscape handles the Escape key
func (s *vpnViewState) handleEscape() *tcell.EventKey {
	if s.currentFilter != "" {
		s.currentFilter = ""
		s.filterInput.SetText("")
		s.updateTable("")
		s.showTemporaryStatus(" [yellow]Filter cleared[-]", vpnStatusDefaultNoR)
		return nil
	}
	s.onBack()
	return nil
}

// handleRuneKey handles character key presses
func (s *vpnViewState) handleRuneKey(r rune) *tcell.EventKey {
	switch r {
	case '/':
		s.enterFilterMode()
	case 'R':
		s.refreshVPNData()
	case 'b':
		s.openInBrowser()
	case 'd':
		s.showDetails()
	case '?':
		s.showHelp()
	default:
		return &tcell.EventKey{} // Return event for unhandled keys
	}
	return nil
}

// enterFilterMode enters filter/search mode
func (s *vpnViewState) enterFilterMode() {
	s.filterMode = true
	s.filterInput.SetText(s.currentFilter)
	s.flex.Clear()
	s.flex.AddItem(s.filterInput, 1, 0, true)
	s.flex.AddItem(s.table, 0, 1, false)
	s.flex.AddItem(s.status, 1, 0, false)
	s.app.SetFocus(s.filterInput)
}

// exitFilterMode exits filter mode and restores normal view
func (s *vpnViewState) exitFilterMode() {
	s.filterMode = false
	s.flex.RemoveItem(s.filterInput)
	s.flex.Clear()
	s.flex.AddItem(s.table, 0, 1, true)
	s.flex.AddItem(s.status, 1, 0, false)
	s.app.SetFocus(s.table)
	s.restoreDefaultStatus()
}

// refreshVPNData refreshes VPN data from GCP
func (s *vpnViewState) refreshVPNData() {
	refreshCtx, cancelRefresh := context.WithCancel(s.ctx)
	updateMsg, spinnerDone := showLoadingScreen(s.app, " Refreshing VPN Data ", "Initializing...", func() {
		cancelRefresh()
		s.app.SetRoot(s.flex, true).SetFocus(s.table)
		s.showTemporaryStatus(" [yellow]Refresh cancelled[-]", vpnStatusDefault)
	})

	go func() {
		s.isRefreshing = true
		defer func() { s.isRefreshing = false }()

		overview, err := s.gcpClient.ListVPNOverview(refreshCtx, func(msg string) {
			updateMsg(msg)
		})

		s.app.QueueUpdateDraw(func() {
			select {
			case spinnerDone <- true:
			default:
			}

			if refreshCtx.Err() == context.Canceled {
				return
			}

			if err != nil {
				s.allEntries = []vpnEntry{{Type: "error", Name: "Failed to load VPN data", Status: err.Error()}}
			} else {
				s.vpnData = overview
				s.allEntries = buildVPNEntries(overview)
			}

			s.app.SetRoot(s.flex, true).SetFocus(s.table)
			s.updateTable(s.currentFilter)
			s.showTemporaryStatus(" [green]Refreshed![-]", vpnStatusDefault)
		})
	}()
}

// openInBrowser opens the selected VPN resource in the Cloud Console
func (s *vpnViewState) openInBrowser() {
	entry, err := s.getSelectedEntry()
	if err != nil {
		s.showTemporaryStatus(fmt.Sprintf(" [red]%s[-]", err.Error()), vpnStatusDefaultNoR)
		return
	}

	url := s.buildConsoleURL(entry)
	if url == "" {
		s.showTemporaryStatus(" [red]Browser view not available for this item type[-]", vpnStatusDefaultNoR)
		return
	}

	if err := OpenInBrowser(url); err != nil {
		s.status.SetText(fmt.Sprintf(" [yellow]URL: %s[-]", url))
	} else {
		s.showTemporaryStatus(" [green]Opened in browser[-]", vpnStatusDefaultNoR)
	}
}

// showDetails shows detailed information for the selected entry
func (s *vpnViewState) showDetails() {
	entry, err := s.getSelectedEntry()
	if err != nil {
		s.showTemporaryStatus(fmt.Sprintf(" [red]%s[-]", err.Error()), vpnStatusDefaultNoR)
		return
	}

	showVPNDetail(s.app, s.table, s.flex, entry, &s.modalOpen, s.status, s.currentFilter)
}

// showHelp displays the help screen
func (s *vpnViewState) showHelp() {
	showVPNHelp(s.app, s.table, s.flex, &s.modalOpen, s.currentFilter, s.status)
}

// getSelectedEntry returns the currently selected entry with validation
func (s *vpnViewState) getSelectedEntry() (vpnEntry, error) {
	row, _ := s.table.GetSelection()
	if row <= 0 || row > len(s.allEntries) {
		return vpnEntry{}, fmt.Errorf("no item selected")
	}
	return s.allEntries[row-1], nil
}

// buildConsoleURL builds the Google Cloud Console URL for a VPN entry
func (s *vpnViewState) buildConsoleURL(entry vpnEntry) string {
	switch {
	case entry.Gateway != nil:
		// HA VPN gateways need the isHA=true parameter
		baseURL := buildCloudConsoleURL(path.Join("hybrid/vpn/gateways/details", entry.Region, entry.Name), s.selectedProject)
		return baseURL + "&isHA=true"
	case entry.Tunnel != nil:
		// Differentiate between HA VPN and Classic VPN tunnels
		// Regular tunnels (type="tunnel") are HA VPN - need isHA=true
		// Orphan tunnels (type="orphan-tunnel") are Classic VPN - no isHA parameter
		baseURL := buildCloudConsoleURL(path.Join("hybrid/vpn/tunnels/details", entry.Region, entry.Name), s.selectedProject)
		if entry.Type == "tunnel" {
			// HA VPN tunnel
			return baseURL + "&isHA=true"
		}
		// Classic VPN tunnel (orphan)
		return baseURL
	case entry.BGP != nil:
		return buildCloudConsoleURL(path.Join("hybrid/routers/bgpSession", entry.BGP.Region, entry.BGP.RouterName, entry.BGP.Name), s.selectedProject)
	default:
		return ""
	}
}

// updateTable updates the table with filtered entries
func (s *vpnViewState) updateTable(filter string) {
	// Clear existing rows (keep header)
	for row := s.table.GetRowCount() - 1; row > 0; row-- {
		s.table.RemoveRow(row)
	}

	// Filter entries
	expr := parseFilter(filter)
	var filteredEntries []vpnEntry
	for _, entry := range s.allEntries {
		if expr.matches(entry.Name, entry.Region, entry.Network, entry.Status) {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// Populate table rows
	for i, entry := range filteredEntries {
		indent := strings.Repeat("  ", entry.Level)
		name := indent + entry.Name

		s.table.SetCell(i+1, 0, tview.NewTableCell(name).SetExpansion(1))
		s.table.SetCell(i+1, 1, tview.NewTableCell(entry.Status).SetExpansion(1))
		s.table.SetCell(i+1, 2, tview.NewTableCell(entry.Region).SetExpansion(1))
		s.table.SetCell(i+1, 3, tview.NewTableCell(entry.Network).SetExpansion(1))
	}

	// Update title
	s.updateTitle(filter, len(filteredEntries))

	// Select first data row if available
	if len(filteredEntries) > 0 {
		s.table.Select(1, 0)
	}
}

// updateTitle updates the table title with counts
func (s *vpnViewState) updateTitle(filter string, filteredCount int) {
	gwCount, tunnelCount, bgpCount := s.countResources()

	titleSuffix := ""
	if filter != "" {
		titleSuffix = fmt.Sprintf(" [filtered: %d/%d]", filteredCount, len(s.allEntries))
	}

	s.table.SetTitle(fmt.Sprintf(" VPN Overview [yellow][%s][-] (%d gateways, %d tunnels, %d BGP)%s ",
		s.selectedProject, gwCount, tunnelCount, bgpCount, titleSuffix))
}

// countResources counts the total number of each resource type
func (s *vpnViewState) countResources() (gateways, tunnels, bgp int) {
	if s.vpnData == nil {
		return 0, 0, 0
	}

	gateways = len(s.vpnData.Gateways)
	for _, gw := range s.vpnData.Gateways {
		tunnels += len(gw.Tunnels)
		for _, t := range gw.Tunnels {
			bgp += len(t.BgpSessions)
		}
	}
	tunnels += len(s.vpnData.OrphanTunnels)
	bgp += len(s.vpnData.OrphanSessions)

	return
}

// restoreDefaultStatus restores the default status bar text
func (s *vpnViewState) restoreDefaultStatus() {
	if s.currentFilter != "" {
		s.status.SetText(fmt.Sprintf(vpnStatusFilterActive, s.currentFilter))
	} else {
		s.status.SetText(vpnStatusDefaultNoR)
	}
}

// showTemporaryStatus shows a temporary status message that auto-restores after 2 seconds
func (s *vpnViewState) showTemporaryStatus(message, defaultStatus string) {
	s.status.SetText(message)
	time.AfterFunc(2*time.Second, func() {
		s.app.QueueUpdateDraw(func() {
			if s.currentFilter != "" {
				s.status.SetText(fmt.Sprintf(vpnStatusFilterActive, s.currentFilter))
			} else {
				s.status.SetText(defaultStatus)
			}
		})
	})
}

// ShowVPNGatewayDetails displays a fullscreen modal with detailed VPN gateway information
func ShowVPNGatewayDetails(app *tview.Application, gateway *gcp.VPNGatewayInfo, onClose func()) {
	detailText := buildGatewayDetailText(gateway)
	showDetailModal(app, gateway.Name, detailText, onClose)
}

// ShowVPNTunnelDetails displays a fullscreen modal with detailed VPN tunnel information
func ShowVPNTunnelDetails(app *tview.Application, tunnel *gcp.VPNTunnelInfo, onClose func()) {
	detailText := buildTunnelDetailText(tunnel)
	showDetailModal(app, tunnel.Name, detailText, onClose)
}

// buildGatewayDetailText builds the detail text for a gateway
func buildGatewayDetailText(gateway *gcp.VPNGatewayInfo) string {
	var b strings.Builder

	b.WriteString("[yellow::b]VPN Gateway Details[-:-:-]\n\n")
	b.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]        %s\n", gateway.Name))
	b.WriteString(fmt.Sprintf("[white::b]Region:[-:-:-]      %s\n", gateway.Region))
	b.WriteString(fmt.Sprintf("[white::b]Network:[-:-:-]     %s\n", extractNetworkName(gateway.Network)))

	if gateway.Description != "" {
		b.WriteString(fmt.Sprintf("[white::b]Description:[-:-:-] %s\n", gateway.Description))
	}

	b.WriteString(fmt.Sprintf("[white::b]Tunnels:[-:-:-]     %d\n", len(gateway.Tunnels)))

	if len(gateway.Interfaces) > 0 {
		b.WriteString("\n[yellow::b]Interfaces:[-:-:-]\n")
		for _, iface := range gateway.Interfaces {
			b.WriteString(fmt.Sprintf("  Interface #%d: %s\n", iface.Id, iface.IpAddress))
		}
	}

	if len(gateway.Labels) > 0 {
		b.WriteString("\n[yellow::b]Labels:[-:-:-]\n")
		for k, v := range gateway.Labels {
			b.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}

	b.WriteString("\n[darkgray]Press Esc to close[-]")
	return b.String()
}

// buildTunnelDetailText builds the detail text for a tunnel
func buildTunnelDetailText(tunnel *gcp.VPNTunnelInfo) string {
	var b strings.Builder

	b.WriteString("[yellow::b]VPN Tunnel Details[-:-:-]\n\n")
	b.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]           %s\n", tunnel.Name))
	b.WriteString(fmt.Sprintf("[white::b]Region:[-:-:-]         %s\n", tunnel.Region))
	b.WriteString(fmt.Sprintf("[white::b]Status:[-:-:-]         %s\n", tunnel.Status))

	if tunnel.DetailedStatus != "" {
		b.WriteString(fmt.Sprintf("[white::b]Detailed Status:[-:-:-] %s\n", tunnel.DetailedStatus))
	}

	if tunnel.Description != "" {
		b.WriteString(fmt.Sprintf("[white::b]Description:[-:-:-]    %s\n", tunnel.Description))
	}

	b.WriteString(fmt.Sprintf("[white::b]Local Gateway IP:[-:-:-] %s\n", tunnel.LocalGatewayIP))
	b.WriteString(fmt.Sprintf("[white::b]Peer IP:[-:-:-]        %s\n", tunnel.PeerIP))

	if tunnel.PeerGateway != "" {
		b.WriteString(fmt.Sprintf("[white::b]Peer Gateway:[-:-:-]   %s\n", extractNetworkName(tunnel.PeerGateway)))
	}

	if tunnel.RouterName != "" {
		b.WriteString(fmt.Sprintf("[white::b]Router:[-:-:-]         %s\n", tunnel.RouterName))
	}

	if tunnel.IkeVersion > 0 {
		b.WriteString(fmt.Sprintf("[white::b]IKE Version:[-:-:-]    %d\n", tunnel.IkeVersion))
	}

	// BGP Sessions
	if len(tunnel.BgpSessions) > 0 {
		b.WriteString("\n[yellow::b]BGP Sessions:[-:-:-]\n")
		for _, bgp := range tunnel.BgpSessions {
			b.WriteString(fmt.Sprintf("\n  [white::b]%s[-:-:-]\n", bgp.Name))
			b.WriteString(fmt.Sprintf("    Status:      %s / %s\n", bgp.SessionStatus, bgp.SessionState))
			b.WriteString(fmt.Sprintf("    Local:       %s (AS%d)\n", bgp.LocalIP, bgp.LocalASN))
			b.WriteString(fmt.Sprintf("    Peer:        %s (AS%d)\n", bgp.PeerIP, bgp.PeerASN))
			b.WriteString(fmt.Sprintf("    Priority:    %d\n", bgp.RoutePriority))
			b.WriteString(fmt.Sprintf("    Enabled:     %v\n", bgp.Enabled))

			if len(bgp.AdvertisedPrefixes) > 0 {
				b.WriteString(fmt.Sprintf("    [green]Advertised:[-]  %d prefixes\n", len(bgp.AdvertisedPrefixes)))
				for _, prefix := range bgp.AdvertisedPrefixes {
					b.WriteString(fmt.Sprintf("      %s\n", prefix))
				}
			} else {
				b.WriteString("    [gray]Advertised:  0 prefixes[-]\n")
			}

			if len(bgp.LearnedPrefixes) > 0 {
				b.WriteString(fmt.Sprintf("    [green]Learned:[-]     %d prefixes\n", len(bgp.LearnedPrefixes)))
				for _, prefix := range bgp.LearnedPrefixes {
					b.WriteString(fmt.Sprintf("      %s\n", prefix))
				}
			} else {
				b.WriteString("    [gray]Learned:     0 prefixes[-]\n")
			}
		}
	}

	b.WriteString("\n[darkgray]Press Esc to close[-]")
	return b.String()
}

// buildBGPDetailText builds the detail text for a BGP session
func buildBGPDetailText(bgp *gcp.BGPSessionInfo) string {
	var b strings.Builder

	b.WriteString("[yellow::b]BGP Session Details[-:-:-]\n\n")
	b.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]           %s\n", bgp.Name))
	b.WriteString(fmt.Sprintf("[white::b]Region:[-:-:-]         %s\n", bgp.Region))
	b.WriteString(fmt.Sprintf("[white::b]Router:[-:-:-]         %s\n", bgp.RouterName))
	b.WriteString(fmt.Sprintf("[white::b]Interface:[-:-:-]      %s\n", bgp.Interface))
	b.WriteString(fmt.Sprintf("[white::b]Session Status:[-:-:-] %s\n", bgp.SessionStatus))
	b.WriteString(fmt.Sprintf("[white::b]Session State:[-:-:-]  %s\n", bgp.SessionState))
	b.WriteString(fmt.Sprintf("[white::b]Enabled:[-:-:-]        %v\n", bgp.Enabled))
	b.WriteString(fmt.Sprintf("\n[white::b]Local IP:[-:-:-]       %s\n", bgp.LocalIP))
	b.WriteString(fmt.Sprintf("[white::b]Local ASN:[-:-:-]      %d\n", bgp.LocalASN))
	b.WriteString(fmt.Sprintf("[white::b]Peer IP:[-:-:-]        %s\n", bgp.PeerIP))
	b.WriteString(fmt.Sprintf("[white::b]Peer ASN:[-:-:-]       %d\n", bgp.PeerASN))
	b.WriteString(fmt.Sprintf("[white::b]Route Priority:[-:-:-] %d\n", bgp.RoutePriority))

	if bgp.AdvertisedMode != "" {
		b.WriteString(fmt.Sprintf("[white::b]Advertise Mode:[-:-:-] %s\n", bgp.AdvertisedMode))
	}

	if len(bgp.AdvertisedGroups) > 0 {
		b.WriteString(fmt.Sprintf("\n[white::b]Advertised Groups:[-:-:-] %s\n", strings.Join(bgp.AdvertisedGroups, ", ")))
	}

	if len(bgp.AdvertisedPrefixes) > 0 {
		b.WriteString(fmt.Sprintf("\n[green::b]Advertised Prefixes (%d):[-:-:-]\n", len(bgp.AdvertisedPrefixes)))
		for _, prefix := range bgp.AdvertisedPrefixes {
			b.WriteString(fmt.Sprintf("  %s\n", prefix))
		}
	} else {
		b.WriteString("\n[gray]No advertised prefixes[-]\n")
	}

	if len(bgp.LearnedPrefixes) > 0 {
		b.WriteString(fmt.Sprintf("\n[green::b]Learned Prefixes (%d):[-:-:-]\n", len(bgp.LearnedPrefixes)))
		for _, prefix := range bgp.LearnedPrefixes {
			b.WriteString(fmt.Sprintf("  %s\n", prefix))
		}
	} else {
		b.WriteString("\n[gray]No learned prefixes[-]\n")
	}

	if len(bgp.BestRoutePrefixes) > 0 {
		b.WriteString(fmt.Sprintf("\n[yellow::b]Best Routes (%d):[-:-:-]\n", len(bgp.BestRoutePrefixes)))
		for _, prefix := range bgp.BestRoutePrefixes {
			b.WriteString(fmt.Sprintf("  %s\n", prefix))
		}
	}

	b.WriteString("\n[darkgray]Press Esc to close[-]")
	return b.String()
}

// showDetailModal shows a detail modal with the given content
func showDetailModal(app *tview.Application, title, text string, onClose func()) {
	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(text).
		SetScrollable(true).
		SetWordWrap(true)
	detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", title))

	detailStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(vpnStatusDetailScroll)

	detailFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(detailView, 0, 1, true).
		AddItem(detailStatus, 1, 0, false)

	detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			if onClose != nil {
				onClose()
			}
			return nil
		}
		return event
	})

	app.SetRoot(detailFlex, true).SetFocus(detailView)
}

// showVPNDetail shows details for a VPN entry
func showVPNDetail(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, entry vpnEntry, modalOpen *bool, status *tview.TextView, currentFilter string) {
	onClose := func() {
		*modalOpen = false
		app.SetRoot(mainFlex, true)
		app.SetFocus(table)
		if currentFilter != "" {
			status.SetText(fmt.Sprintf(vpnStatusFilterActive, currentFilter))
		} else {
			status.SetText(vpnStatusDefaultNoR)
		}
	}

	*modalOpen = true

	switch entry.Type {
	case "gateway":
		if entry.Gateway != nil {
			ShowVPNGatewayDetails(app, entry.Gateway, onClose)
		}
	case "tunnel", "orphan-tunnel":
		if entry.Tunnel != nil {
			ShowVPNTunnelDetails(app, entry.Tunnel, onClose)
		}
	case "bgp", "orphan-bgp":
		if entry.BGP != nil {
			detailText := buildBGPDetailText(entry.BGP)
			showDetailModal(app, entry.Name, detailText, onClose)
		}
	default:
		showDetailModal(app, entry.Name, "[red]No details available[-]\n\n[darkgray]Press Esc to close[-]", onClose)
	}
}

// showVPNHelp displays the help screen
func showVPNHelp(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, modalOpen *bool, currentFilter string, status *tview.TextView) {
	helpText := `[yellow::b]VPN View - Keyboard Shortcuts[-:-:-]

[yellow]Navigation[-]
  [white]up/k[-]          Move selection up
  [white]down/j[-]        Move selection down
  [white]Home/g[-]        Jump to first item
  [white]End/G[-]         Jump to last item

[yellow]Actions[-]
  [white]b[-]             Open selected item in Cloud Console
  [white]d[-]             Show details for selected item
  [white]Shift+R[-]       Refresh VPN data from GCP
  [white]/[-]             Enter filter/search mode
  [white]Esc[-]           Clear filter (if active) or return to instance view

[yellow]General[-]
  [white]?[-]             Show this help

[darkgray]Press Esc or ? to close this help[-]`

	helpView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)
	helpView.SetBorder(true).
		SetTitle(" VPN Help ").
		SetTitleAlign(tview.AlignCenter)

	helpStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetText(vpnStatusHelpClose)

	helpFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(helpView, 0, 1, true).
		AddItem(helpStatus, 1, 0, false)

	helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == '?') {
			*modalOpen = false
			app.SetRoot(mainFlex, true)
			app.SetFocus(table)
			if currentFilter != "" {
				status.SetText(fmt.Sprintf(vpnStatusFilterActive, currentFilter))
			} else {
				status.SetText(vpnStatusDefaultNoR)
			}
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(helpFlex, true).SetFocus(helpView)
}

// Helper functions

func formatTunnelStatus(status string) string {
	switch status {
	case "ESTABLISHED":
		return "[green]ESTABLISHED[-]"
	case "NETWORK_ERROR", "AUTHORIZATION_ERROR", "NEGOTIATION_FAILURE":
		return "[red]" + status + "[-]"
	case "PROVISIONING", "WAITING_FOR_FULL_CONFIG":
		return "[yellow]" + status + "[-]"
	case "FIRST_HANDSHAKE", "NO_INCOMING_PACKETS":
		return "[orange]" + status + "[-]"
	default:
		return "[gray]" + status + "[-]"
	}
}

func formatBGPStatus(state string) string {
	switch state {
	case "UP", "Established":
		return "[green]UP[-]"
	case "DOWN":
		return "[red]DOWN[-]"
	default:
		return "[gray]" + state + "[-]"
	}
}

func extractNetworkName(networkURL string) string {
	if networkURL == "" {
		return ""
	}
	parts := strings.Split(networkURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return networkURL
}
