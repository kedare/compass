package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/rivo/tview"
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

// RunVPNView shows the VPN overview
func RunVPNView(ctx context.Context, c *cache.Cache, app *tview.Application, onBack func()) error {
	return RunVPNViewWithProgress(ctx, c, app, nil, onBack)
}

// RunVPNViewWithProgress shows the VPN overview with optional progress callback
// It first prompts the user to select a project, then loads VPN data for that project
func RunVPNViewWithProgress(ctx context.Context, c *cache.Cache, app *tview.Application, progress func(string), onBack func()) error {
	// Show project selector first
	ShowProjectSelector(app, c, " Select Project for VPN View ", func(result ProjectSelectorResult) {
		if result.Cancelled {
			onBack()
			return
		}

		selectedProject := result.Project

		// Show loading screen immediately after project selection
		loadingCtx, cancelLoading := context.WithCancel(ctx)

		updateMsg, spinnerDone := showLoadingScreen(app, fmt.Sprintf(" Loading VPN Data [%s] ", selectedProject), "Creating GCP client...", func() {
			cancelLoading()
			onBack()
		})

		// Load everything in background
		go func() {
			var vpnData *gcp.VPNOverview
			var allEntries []vpnEntry
			var gcpClient *gcp.Client

			// Create GCP client
			client, err := gcp.NewClient(loadingCtx, selectedProject)
			if err != nil {
				// Signal spinner to stop
				select {
				case spinnerDone <- true:
				default:
				}

				if loadingCtx.Err() == context.Canceled {
					return
				}

				app.QueueUpdateDraw(func() {
					modal := tview.NewModal().
						SetText(fmt.Sprintf("Failed to create GCP client for project '%s':\n\n%v", selectedProject, err)).
						AddButtons([]string{"OK"}).
						SetDoneFunc(func(buttonIndex int, buttonLabel string) {
							onBack()
						})
					app.SetRoot(modal, true).SetFocus(modal)
				})
				return
			}
			gcpClient = client

			// Progress callback
			progressCallback := func(msg string) {
				updateMsg(msg)
				if progress != nil {
					progress(msg)
				}
			}

			updateMsg("Loading VPN gateways and tunnels...")

			overview, err := gcpClient.ListVPNOverview(loadingCtx, progressCallback)
			if err != nil {
				allEntries = []vpnEntry{{
					Type:   "error",
					Name:   "Failed to load VPN data",
					Status: err.Error(),
				}}
			} else {
				vpnData = overview
				allEntries = buildVPNEntries(overview)
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
				// Now show the actual VPN view
				showVPNViewUI(ctx, gcpClient, selectedProject, app, vpnData, allEntries, onBack)
			})
		}()
	})

	return nil
}

// buildVPNEntries converts VPN overview data to display entries
func buildVPNEntries(overview *gcp.VPNOverview) []vpnEntry {
	var allEntries []vpnEntry

	// Add gateways and their tunnels/BGP sessions
	for _, gw := range overview.Gateways {
		// Gateway entry
		tunnelCount := len(gw.Tunnels)
		bgpCount := 0
		for _, t := range gw.Tunnels {
			bgpCount += len(t.BgpSessions)
		}

		gwEntry := vpnEntry{
			Type:    "gateway",
			Name:    gw.Name,
			Status:  fmt.Sprintf("%d tunnels, %d BGP", tunnelCount, bgpCount),
			Region:  gw.Region,
			Network: extractNetworkName(gw.Network),
			Level:   0,
			Gateway: gw,
		}
		allEntries = append(allEntries, gwEntry)

		// Tunnel entries
		for _, tunnel := range gw.Tunnels {
			tunnelEntry := vpnEntry{
				Type:        "tunnel",
				Name:        tunnel.Name,
				Status:      formatTunnelStatus(tunnel.Status),
				Region:      tunnel.Region,
				Parent:      gw.Name,
				DetailedMsg: tunnel.DetailedStatus,
				Level:       1,
				Tunnel:      tunnel,
			}
			allEntries = append(allEntries, tunnelEntry)

			// BGP entries
			for _, bgp := range tunnel.BgpSessions {
				bgpEntry := vpnEntry{
					Type:   "bgp",
					Name:   bgp.Name,
					Status: formatBGPStatus(bgp.SessionState),
					Region: bgp.Region,
					Parent: tunnel.Name,
					Level:  2,
					BGP:    bgp,
				}
				allEntries = append(allEntries, bgpEntry)
			}
		}
	}

	// Add orphan tunnels
	if len(overview.OrphanTunnels) > 0 {
		allEntries = append(allEntries, vpnEntry{
			Type:   "section",
			Name:   "Orphan Tunnels (Classic VPN)",
			Status: fmt.Sprintf("%d tunnels", len(overview.OrphanTunnels)),
			Level:  0,
		})
		for _, tunnel := range overview.OrphanTunnels {
			tunnelEntry := vpnEntry{
				Type:        "orphan-tunnel",
				Name:        tunnel.Name,
				Status:      formatTunnelStatus(tunnel.Status),
				Region:      tunnel.Region,
				DetailedMsg: tunnel.DetailedStatus,
				Level:       1,
				Tunnel:      tunnel,
			}
			allEntries = append(allEntries, tunnelEntry)
		}
	}

	// Add orphan BGP sessions
	if len(overview.OrphanSessions) > 0 {
		allEntries = append(allEntries, vpnEntry{
			Type:   "section",
			Name:   "Orphan BGP Sessions",
			Status: fmt.Sprintf("%d sessions", len(overview.OrphanSessions)),
			Level:  0,
		})
		for _, bgp := range overview.OrphanSessions {
			bgpEntry := vpnEntry{
				Type:   "orphan-bgp",
				Name:   bgp.Name,
				Status: formatBGPStatus(bgp.SessionState),
				Region: bgp.Region,
				Level:  1,
				BGP:    bgp,
			}
			allEntries = append(allEntries, bgpEntry)
		}
	}

	return allEntries
}

// showVPNViewUI displays the VPN view UI after data is loaded
func showVPNViewUI(ctx context.Context, gcpClient *gcp.Client, selectedProject string, app *tview.Application, vpnData *gcp.VPNOverview, initialEntries []vpnEntry, onBack func()) {
	var allEntries = initialEntries
	var isRefreshing bool
	var modalOpen bool
	var filterMode bool
	var currentFilter string

	// Function to load VPN data (for refresh)
	loadVPNData := func(progressCallback func(string)) {
		isRefreshing = true

		overview, err := gcpClient.ListVPNOverview(ctx, progressCallback)
		if err != nil {
			// Show error
			allEntries = []vpnEntry{{
				Type:   "error",
				Name:   "Failed to load VPN data",
				Status: err.Error(),
			}}
			isRefreshing = false
			return
		}

		vpnData = overview
		allEntries = buildVPNEntries(overview)
		isRefreshing = false
	}

	// Create table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	table.SetBorder(true).SetTitle(fmt.Sprintf(" VPN Overview [yellow][%s][-] ", selectedProject))

	// Add header
	headers := []string{"Name", "Status", "Region", "Network"}
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

	// Update table with entries
	updateTable := func(filter string) {
		// Clear existing rows (keep header)
		for row := table.GetRowCount() - 1; row > 0; row-- {
			table.RemoveRow(row)
		}

		// Filter entries
		expr := parseFilter(filter)
		var filteredEntries []vpnEntry
		for _, entry := range allEntries {
			if expr.matches(entry.Name, entry.Region, entry.Network, entry.Status) {
				filteredEntries = append(filteredEntries, entry)
			}
		}

		currentRow := 1
		for _, entry := range filteredEntries {
			// Add indentation based on level
			indent := strings.Repeat("  ", entry.Level)
			name := indent + entry.Name

			table.SetCell(currentRow, 0, tview.NewTableCell(name).SetExpansion(1))
			table.SetCell(currentRow, 1, tview.NewTableCell(entry.Status).SetExpansion(1))
			table.SetCell(currentRow, 2, tview.NewTableCell(entry.Region).SetExpansion(1))
			table.SetCell(currentRow, 3, tview.NewTableCell(entry.Network).SetExpansion(1))
			currentRow++
		}

		// Update title with project and counts
		gwCount := 0
		tunnelCount := 0
		bgpCount := 0
		if vpnData != nil {
			gwCount = len(vpnData.Gateways)
			for _, gw := range vpnData.Gateways {
				tunnelCount += len(gw.Tunnels)
				for _, t := range gw.Tunnels {
					bgpCount += len(t.BgpSessions)
				}
			}
			tunnelCount += len(vpnData.OrphanTunnels)
			bgpCount += len(vpnData.OrphanSessions)
		}

		titleSuffix := ""
		if filter != "" {
			titleSuffix = fmt.Sprintf(" [filtered: %d/%d]", len(filteredEntries), len(allEntries))
		}
		table.SetTitle(fmt.Sprintf(" VPN Overview [yellow][%s][-] (%d gateways, %d tunnels, %d BGP)%s ", selectedProject, gwCount, tunnelCount, bgpCount, titleSuffix))

		// Select first data row if available
		if len(filteredEntries) > 0 {
			table.Select(1, 0)
		}
	}

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]Shift+R[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")

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
				status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")
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
				status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")
			}
		}
	})

	// Initial table population (data is already loaded)
	updateTable("")

	// Setup keyboard
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If in filter mode, let the input field handle it
		if filterMode {
			return event
		}

		// Don't handle ESC if a modal is open
		if modalOpen && event.Key() == tcell.KeyEscape {
			return event
		}

		// Don't allow actions during refresh
		if isRefreshing {
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
						status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")
					})
				})
				return nil
			}
			// Go back to instance view
			onBack()
			return nil

		case tcell.KeyRune:
			switch event.Rune() {
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

			case 'R':
				// Refresh VPN data with loading screen
				refreshCtx, cancelRefresh := context.WithCancel(ctx)

				updateMsg, spinnerDone := showLoadingScreen(app, " Refreshing VPN Data ", "Initializing...", func() {
					cancelRefresh()
					app.SetRoot(flex, true).SetFocus(table)
					status.SetText(" [yellow]Refresh cancelled[-]")
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							if currentFilter != "" {
								status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
							} else {
								status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]Shift+R[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")
							}
						})
					})
				})

				// Progress callback for VPN loading
				progressCallback := func(msg string) {
					updateMsg(msg)
				}

				go func() {
					loadVPNData(progressCallback)
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
									status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]Shift+R[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")
								}
							})
						})
					})
				}()
				return nil

			case 'd':
				// Show details for selected item
				row, _ := table.GetSelection()
				if row <= 0 || row > len(allEntries) {
					status.SetText(" [red]No item selected[-]")
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							if currentFilter != "" {
								status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
							} else {
								status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")
							}
						})
					})
					return nil
				}

				entry := allEntries[row-1]
				showVPNDetail(app, table, flex, entry, &modalOpen, status, currentFilter)
				return nil

			case '?':
				// Show help
				showVPNHelp(app, table, flex, &modalOpen, currentFilter, status)
				return nil
			}
		}
		return event
	})

	app.SetRoot(flex, true).EnableMouse(true).SetFocus(table)
}

// ShowVPNGatewayDetails displays a fullscreen modal with detailed VPN gateway information.
// This is a reusable function that can be called from both the VPN view and global search.
func ShowVPNGatewayDetails(app *tview.Application, gateway *gcp.VPNGatewayInfo, onClose func()) {
	var detailText strings.Builder

	detailText.WriteString("[yellow::b]VPN Gateway Details[-:-:-]\n\n")
	detailText.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]        %s\n", gateway.Name))
	detailText.WriteString(fmt.Sprintf("[white::b]Region:[-:-:-]      %s\n", gateway.Region))
	detailText.WriteString(fmt.Sprintf("[white::b]Network:[-:-:-]     %s\n", extractNetworkName(gateway.Network)))

	if gateway.Description != "" {
		detailText.WriteString(fmt.Sprintf("[white::b]Description:[-:-:-] %s\n", gateway.Description))
	}

	detailText.WriteString(fmt.Sprintf("[white::b]Tunnels:[-:-:-]     %d\n", len(gateway.Tunnels)))

	if len(gateway.Interfaces) > 0 {
		detailText.WriteString("\n[yellow::b]Interfaces:[-:-:-]\n")
		for _, iface := range gateway.Interfaces {
			detailText.WriteString(fmt.Sprintf("  Interface #%d: %s\n", iface.Id, iface.IpAddress))
		}
	}

	if len(gateway.Labels) > 0 {
		detailText.WriteString("\n[yellow::b]Labels:[-:-:-]\n")
		for k, v := range gateway.Labels {
			detailText.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}

	detailText.WriteString("\n[darkgray]Press Esc to close[-]")

	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(detailText.String()).
		SetScrollable(true).
		SetWordWrap(true)
	detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", gateway.Name))

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
			if onClose != nil {
				onClose()
			}
			return nil
		}
		return event
	})

	app.SetRoot(detailFlex, true).SetFocus(detailView)
}

// ShowVPNTunnelDetails displays a fullscreen modal with detailed VPN tunnel information.
// This is a reusable function that can be called from both the VPN view and global search.
func ShowVPNTunnelDetails(app *tview.Application, tunnel *gcp.VPNTunnelInfo, onClose func()) {
	var detailText strings.Builder

	detailText.WriteString("[yellow::b]VPN Tunnel Details[-:-:-]\n\n")
	detailText.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]           %s\n", tunnel.Name))
	detailText.WriteString(fmt.Sprintf("[white::b]Region:[-:-:-]         %s\n", tunnel.Region))
	detailText.WriteString(fmt.Sprintf("[white::b]Status:[-:-:-]         %s\n", tunnel.Status))

	if tunnel.DetailedStatus != "" {
		detailText.WriteString(fmt.Sprintf("[white::b]Detailed Status:[-:-:-] %s\n", tunnel.DetailedStatus))
	}

	if tunnel.Description != "" {
		detailText.WriteString(fmt.Sprintf("[white::b]Description:[-:-:-]    %s\n", tunnel.Description))
	}

	detailText.WriteString(fmt.Sprintf("[white::b]Local Gateway IP:[-:-:-] %s\n", tunnel.LocalGatewayIP))
	detailText.WriteString(fmt.Sprintf("[white::b]Peer IP:[-:-:-]        %s\n", tunnel.PeerIP))

	if tunnel.PeerGateway != "" {
		detailText.WriteString(fmt.Sprintf("[white::b]Peer Gateway:[-:-:-]   %s\n", extractNetworkName(tunnel.PeerGateway)))
	}

	if tunnel.RouterName != "" {
		detailText.WriteString(fmt.Sprintf("[white::b]Router:[-:-:-]         %s\n", tunnel.RouterName))
	}

	if tunnel.IkeVersion > 0 {
		detailText.WriteString(fmt.Sprintf("[white::b]IKE Version:[-:-:-]    %d\n", tunnel.IkeVersion))
	}

	// BGP Sessions
	if len(tunnel.BgpSessions) > 0 {
		detailText.WriteString("\n[yellow::b]BGP Sessions:[-:-:-]\n")
		for _, bgp := range tunnel.BgpSessions {
			detailText.WriteString(fmt.Sprintf("\n  [white::b]%s[-:-:-]\n", bgp.Name))
			detailText.WriteString(fmt.Sprintf("    Status:      %s / %s\n", bgp.SessionStatus, bgp.SessionState))
			detailText.WriteString(fmt.Sprintf("    Local:       %s (AS%d)\n", bgp.LocalIP, bgp.LocalASN))
			detailText.WriteString(fmt.Sprintf("    Peer:        %s (AS%d)\n", bgp.PeerIP, bgp.PeerASN))
			detailText.WriteString(fmt.Sprintf("    Priority:    %d\n", bgp.RoutePriority))
			detailText.WriteString(fmt.Sprintf("    Enabled:     %v\n", bgp.Enabled))

			if len(bgp.AdvertisedPrefixes) > 0 {
				detailText.WriteString(fmt.Sprintf("    [green]Advertised:[-]  %d prefixes\n", len(bgp.AdvertisedPrefixes)))
				for _, prefix := range bgp.AdvertisedPrefixes {
					detailText.WriteString(fmt.Sprintf("      %s\n", prefix))
				}
			} else {
				detailText.WriteString("    [gray]Advertised:  0 prefixes[-]\n")
			}

			if len(bgp.LearnedPrefixes) > 0 {
				detailText.WriteString(fmt.Sprintf("    [green]Learned:[-]     %d prefixes\n", len(bgp.LearnedPrefixes)))
				for _, prefix := range bgp.LearnedPrefixes {
					detailText.WriteString(fmt.Sprintf("      %s\n", prefix))
				}
			} else {
				detailText.WriteString("    [gray]Learned:     0 prefixes[-]\n")
			}
		}
	}

	detailText.WriteString("\n[darkgray]Press Esc to close[-]")

	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(detailText.String()).
		SetScrollable(true).
		SetWordWrap(true)
	detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", tunnel.Name))

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
			if onClose != nil {
				onClose()
			}
			return nil
		}
		return event
	})

	app.SetRoot(detailFlex, true).SetFocus(detailView)
}

func showVPNDetail(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, entry vpnEntry, modalOpen *bool, status *tview.TextView, currentFilter string) {
	// Prepare onClose callback to restore the VPN view
	onClose := func() {
		*modalOpen = false
		app.SetRoot(mainFlex, true)
		app.SetFocus(table)
		if currentFilter != "" {
			status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
		} else {
			status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")
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
			// For BGP sessions, keep inline formatting as they aren't searchable resources
			var detailText strings.Builder
			bgp := entry.BGP
			detailText.WriteString("[yellow::b]BGP Session Details[-:-:-]\n\n")
			detailText.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]           %s\n", bgp.Name))
			detailText.WriteString(fmt.Sprintf("[white::b]Region:[-:-:-]         %s\n", bgp.Region))
			detailText.WriteString(fmt.Sprintf("[white::b]Router:[-:-:-]         %s\n", bgp.RouterName))
			detailText.WriteString(fmt.Sprintf("[white::b]Interface:[-:-:-]      %s\n", bgp.Interface))
			detailText.WriteString(fmt.Sprintf("[white::b]Session Status:[-:-:-] %s\n", bgp.SessionStatus))
			detailText.WriteString(fmt.Sprintf("[white::b]Session State:[-:-:-]  %s\n", bgp.SessionState))
			detailText.WriteString(fmt.Sprintf("[white::b]Enabled:[-:-:-]        %v\n", bgp.Enabled))
			detailText.WriteString(fmt.Sprintf("\n[white::b]Local IP:[-:-:-]       %s\n", bgp.LocalIP))
			detailText.WriteString(fmt.Sprintf("[white::b]Local ASN:[-:-:-]      %d\n", bgp.LocalASN))
			detailText.WriteString(fmt.Sprintf("[white::b]Peer IP:[-:-:-]        %s\n", bgp.PeerIP))
			detailText.WriteString(fmt.Sprintf("[white::b]Peer ASN:[-:-:-]       %d\n", bgp.PeerASN))
			detailText.WriteString(fmt.Sprintf("[white::b]Route Priority:[-:-:-] %d\n", bgp.RoutePriority))

			if bgp.AdvertisedMode != "" {
				detailText.WriteString(fmt.Sprintf("[white::b]Advertise Mode:[-:-:-] %s\n", bgp.AdvertisedMode))
			}

			if len(bgp.AdvertisedGroups) > 0 {
				detailText.WriteString(fmt.Sprintf("\n[white::b]Advertised Groups:[-:-:-] %s\n", strings.Join(bgp.AdvertisedGroups, ", ")))
			}

			if len(bgp.AdvertisedPrefixes) > 0 {
				detailText.WriteString(fmt.Sprintf("\n[green::b]Advertised Prefixes (%d):[-:-:-]\n", len(bgp.AdvertisedPrefixes)))
				for _, prefix := range bgp.AdvertisedPrefixes {
					detailText.WriteString(fmt.Sprintf("  %s\n", prefix))
				}
			} else {
				detailText.WriteString("\n[gray]No advertised prefixes[-]\n")
			}

			if len(bgp.LearnedPrefixes) > 0 {
				detailText.WriteString(fmt.Sprintf("\n[green::b]Learned Prefixes (%d):[-:-:-]\n", len(bgp.LearnedPrefixes)))
				for _, prefix := range bgp.LearnedPrefixes {
					detailText.WriteString(fmt.Sprintf("  %s\n", prefix))
				}
			} else {
				detailText.WriteString("\n[gray]No learned prefixes[-]\n")
			}

			if len(bgp.BestRoutePrefixes) > 0 {
				detailText.WriteString(fmt.Sprintf("\n[yellow::b]Best Routes (%d):[-:-:-]\n", len(bgp.BestRoutePrefixes)))
				for _, prefix := range bgp.BestRoutePrefixes {
					detailText.WriteString(fmt.Sprintf("  %s\n", prefix))
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
				SetText(" [yellow]Esc[-] back  [yellow]up/down[-] scroll")

			// Create fullscreen detail layout
			detailFlex := tview.NewFlex().
				SetDirection(tview.FlexRow).
				AddItem(detailView, 0, 1, true).
				AddItem(detailStatus, 1, 0, false)

			// Set up input handler
			detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				if event.Key() == tcell.KeyEscape {
					onClose()
					return nil
				}
				return event
			})

			app.SetRoot(detailFlex, true).SetFocus(detailView)
		}

	default:
		// Handle unknown type
		var detailText strings.Builder
		detailText.WriteString("[red]No details available[-]")
		detailText.WriteString("\n[darkgray]Press Esc to close[-]")

		detailView := tview.NewTextView().
			SetDynamicColors(true).
			SetText(detailText.String()).
			SetScrollable(true).
			SetWordWrap(true)
		detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", entry.Name))

		detailStatus := tview.NewTextView().
			SetDynamicColors(true).
			SetText(" [yellow]Esc[-] back")

		detailFlex := tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(detailView, 0, 1, true).
			AddItem(detailStatus, 1, 0, false)

		detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyEscape {
				onClose()
				return nil
			}
			return event
		})

		app.SetRoot(detailFlex, true).SetFocus(detailView)
	}
}

func showVPNHelp(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, modalOpen *bool, currentFilter string, status *tview.TextView) {
	helpText := `[yellow::b]VPN View - Keyboard Shortcuts[-:-:-]

[yellow]Navigation[-]
  [white]up/k[-]          Move selection up
  [white]down/j[-]        Move selection down
  [white]Home/g[-]        Jump to first item
  [white]End/G[-]         Jump to last item

[yellow]Actions[-]
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
				status.SetText(fmt.Sprintf(" [green]Filter active: %s[-]", currentFilter))
			} else {
				status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]/[-] filter  [yellow]?[-] help")
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
