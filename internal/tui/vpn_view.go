package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
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
}

// RunVPNView shows the VPN overview
func RunVPNView(ctx context.Context, gcpClient *gcp.Client, app *tview.Application, onBack func()) error {
	var vpnData *gcp.VPNOverview
	var entries []vpnEntry
	var isRefreshing bool
	var modalOpen bool

	// Function to load VPN data
	loadVPNData := func() {
		isRefreshing = true
		entries = []vpnEntry{}

		overview, err := gcpClient.ListVPNOverview(ctx, nil)
		if err != nil {
			// Show error
			entries = append(entries, vpnEntry{
				Type:   "error",
				Name:   "Failed to load VPN data",
				Status: err.Error(),
			})
			isRefreshing = false
			return
		}

		vpnData = overview

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
			}
			entries = append(entries, gwEntry)

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
				}
				entries = append(entries, tunnelEntry)

				// BGP entries
				for _, bgp := range tunnel.BgpSessions {
					bgpEntry := vpnEntry{
						Type:   "bgp",
						Name:   bgp.Name,
						Status: formatBGPStatus(bgp.SessionState),
						Region: bgp.Region,
						Parent: tunnel.Name,
						Level:  2,
					}
					entries = append(entries, bgpEntry)
				}
			}
		}

		// Add orphan tunnels
		if len(overview.OrphanTunnels) > 0 {
			entries = append(entries, vpnEntry{
				Type:   "section",
				Name:   "⚠️  Orphan Tunnels (Classic VPN)",
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
				}
				entries = append(entries, tunnelEntry)
			}
		}

		// Add orphan BGP sessions
		if len(overview.OrphanSessions) > 0 {
			entries = append(entries, vpnEntry{
				Type:   "section",
				Name:   "⚠️  Orphan BGP Sessions",
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
				}
				entries = append(entries, bgpEntry)
			}
		}

		isRefreshing = false
	}

	// Create table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	table.SetBorder(true).SetTitle(" VPN Overview ")

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

	// Update table with entries
	updateTable := func() {
		// Clear existing rows (keep header)
		for row := table.GetRowCount() - 1; row > 0; row-- {
			table.RemoveRow(row)
		}

		currentRow := 1
		for _, entry := range entries {
			// Add indentation based on level
			indent := strings.Repeat("  ", entry.Level)
			name := indent + entry.Name

			table.SetCell(currentRow, 0, tview.NewTableCell(name).SetExpansion(1))
			table.SetCell(currentRow, 1, tview.NewTableCell(entry.Status).SetExpansion(1))
			table.SetCell(currentRow, 2, tview.NewTableCell(entry.Region).SetExpansion(1))
			table.SetCell(currentRow, 3, tview.NewTableCell(entry.Network).SetExpansion(1))
			currentRow++
		}

		// Update title
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
		table.SetTitle(fmt.Sprintf(" VPN Overview (%d gateways, %d tunnels, %d BGP) ", gwCount, tunnelCount, bgpCount))

		// Select first data row if available
		if len(entries) > 0 {
			table.Select(1, 0)
		}
	}

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]?[-] help")

	// Layout
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true).
		AddItem(status, 1, 0, false)

	// Initial load
	loadVPNData()
	updateTable()

	// Setup keyboard
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
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
			// Go back to instance view
			onBack()
			return nil

		case tcell.KeyRune:
			switch event.Rune() {
			case 'r':
				// Refresh VPN data
				status.SetText(" [yellow]Refreshing VPN data...[-]")
				go func() {
					loadVPNData()
					app.QueueUpdateDraw(func() {
						updateTable()
						status.SetText(" [green]Refreshed![-]")
						time.AfterFunc(2*time.Second, func() {
							app.QueueUpdateDraw(func() {
								status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]?[-] help")
							})
						})
					})
				}()
				return nil

			case 'd':
				// Show details for selected item
				row, _ := table.GetSelection()
				if row <= 0 || row > len(entries) {
					status.SetText(" [red]No item selected[-]")
					time.AfterFunc(2*time.Second, func() {
						app.QueueUpdateDraw(func() {
							status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]?[-] help")
						})
					})
					return nil
				}

				entry := entries[row-1]
				showVPNDetail(app, table, flex, entry, &modalOpen, status)
				return nil

			case '?':
				// Show help
				showVPNHelp(app, table, flex, &modalOpen)
				return nil
			}
		}
		return event
	})

	app.SetRoot(flex, true).EnableMouse(true).SetFocus(table)
	return nil
}

func showVPNDetail(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, entry vpnEntry, modalOpen *bool, status *tview.TextView) {
	var detailText string

	switch entry.Type {
	case "gateway":
		detailText = fmt.Sprintf(`[yellow]VPN Gateway Details[-]

[darkgray]Name:[-]     %s
[darkgray]Region:[-]   %s
[darkgray]Network:[-]  %s
[darkgray]Status:[-]   %s

[darkgray]Press Esc to close[-]`,
			entry.Name, entry.Region, entry.Network, entry.Status)

	case "tunnel", "orphan-tunnel":
		detailText = fmt.Sprintf(`[yellow]VPN Tunnel Details[-]

[darkgray]Name:[-]           %s
[darkgray]Region:[-]         %s
[darkgray]Status:[-]         %s
[darkgray]Detailed Status:[-] %s

[darkgray]Press Esc to close[-]`,
			entry.Name, entry.Region, entry.Status, entry.DetailedMsg)

	case "bgp", "orphan-bgp":
		detailText = fmt.Sprintf(`[yellow]BGP Session Details[-]

[darkgray]Name:[-]   %s
[darkgray]Region:[-] %s
[darkgray]Status:[-] %s

[darkgray]Press Esc to close[-]`,
			entry.Name, entry.Region, entry.Status)

	default:
		detailText = "[red]No details available[-]"
	}

	detailView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(detailText).
		SetScrollable(true)
	detailView.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", entry.Name))

	// Create modal with fixed size
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(detailView, 20, 0, true).
			AddItem(nil, 0, 1, false), 80, 0, true).
		AddItem(nil, 0, 1, false)

	// Create pages
	pages := tview.NewPages().
		AddPage("main", mainFlex, true, true).
		AddPage("modal", modal, true, true)

	// Set up modal input handler
	detailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("modal")
			app.SetRoot(mainFlex, true)
			app.SetFocus(table)
			*modalOpen = false
			status.SetText(" [yellow]Esc[-] back  [yellow]d[-] details  [yellow]r[-] refresh  [yellow]?[-] help")
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(pages, true).SetFocus(detailView)
}

func showVPNHelp(app *tview.Application, table *tview.Table, mainFlex *tview.Flex, modalOpen *bool) {
	helpText := `[yellow::b]VPN View - Keyboard Shortcuts[-:-:-]

[yellow]Navigation[-]
  [white]↑/k[-]           Move selection up
  [white]↓/j[-]           Move selection down
  [white]Home/g[-]        Jump to first item
  [white]End/G[-]         Jump to last item

[yellow]Actions[-]
  [white]d[-]             Show details for selected item
  [white]r[-]             Refresh VPN data from GCP
  [white]Esc[-]           Return to instance view

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

	// Create help modal
	helpModal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(helpView, 20, 0, true).
			AddItem(nil, 0, 1, false), 60, 0, true).
		AddItem(nil, 0, 1, false)

	// Create pages
	helpPages := tview.NewPages().
		AddPage("main", mainFlex, true, true).
		AddPage("help", helpModal, true, true)

	// Set up help modal input handler
	helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == '?') {
			helpPages.RemovePage("help")
			app.SetRoot(mainFlex, true)
			app.SetFocus(table)
			*modalOpen = false
			return nil
		}
		return event
	})

	*modalOpen = true
	app.SetRoot(helpPages, true).SetFocus(helpView)
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
