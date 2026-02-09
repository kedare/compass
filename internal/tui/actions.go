package tui

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/gcp/search"
	"github.com/rivo/tview"
)

const (
	// cloudConsoleBaseURL is the base URL for Google Cloud Console
	cloudConsoleBaseURL = "https://console.cloud.google.com"
)

// ResourceAction represents an action that can be performed on a resource
type ResourceAction struct {
	Key         rune   // Key to trigger the action
	Name        string // Display name for the action
	Description string // Short description
}

// ActionContext provides context for executing actions
type ActionContext struct {
	App            *tview.Application
	Ctx            context.Context
	OutputRedir    *outputRedirector
	OnStatusUpdate func(msg string)
	OnError        func(err error)
	OnComplete     func()
}

// ResourceActionHandler handles actions for a specific resource
type ResourceActionHandler interface {
	// GetActions returns the available actions for this resource type
	GetActions() []ResourceAction
	// Execute executes the action for the given key
	Execute(key rune, ctx *ActionContext) bool
}

// GetActionsForResourceType returns the available actions for a resource type
func GetActionsForResourceType(resourceType string) []ResourceAction {
	switch resourceType {
	case string(search.KindComputeInstance):
		return []ResourceAction{
			{Key: 's', Name: "SSH", Description: "SSH to instance"},
			{Key: 'd', Name: "Details", Description: "Show details"},
			{Key: 'b', Name: "Browser", Description: "Open in Cloud Console"},
		}
	case string(search.KindManagedInstanceGroup):
		return []ResourceAction{
			{Key: 's', Name: "SSH", Description: "SSH to MIG instance"},
			{Key: 'd', Name: "Details", Description: "Show details"},
			{Key: 'b', Name: "Browser", Description: "Open in Cloud Console"},
		}
	case string(search.KindBucket):
		return []ResourceAction{
			{Key: 'd', Name: "Details", Description: "Show details"},
			{Key: 'b', Name: "Browser", Description: "Open in Cloud Console"},
			{Key: 'o', Name: "Open", Description: "Open bucket in browser"},
		}
	default:
		// Most resource types support details and browser
		return []ResourceAction{
			{Key: 'd', Name: "Details", Description: "Show details"},
			{Key: 'b', Name: "Browser", Description: "Open in Cloud Console"},
		}
	}
}

// FormatActionsStatusBar formats actions for display in the status bar
func FormatActionsStatusBar(actions []ResourceAction) string {
	if len(actions) == 0 {
		return ""
	}

	var parts []string
	for _, action := range actions {
		parts = append(parts, fmt.Sprintf("[yellow]%c[-] %s", action.Key, action.Name))
	}
	return strings.Join(parts, "  ")
}

// InstanceActionExecutor handles actions for compute instances
type InstanceActionExecutor struct {
	Name    string
	Project string
	Zone    string
	UseIAP  bool
}

// NewInstanceActionExecutor creates a new instance action executor
func NewInstanceActionExecutor(name, project, zone string, useIAP bool) *InstanceActionExecutor {
	return &InstanceActionExecutor{
		Name:    name,
		Project: project,
		Zone:    zone,
		UseIAP:  useIAP,
	}
}

// SSHOptions contains configuration options for an SSH session
type SSHOptions struct {
	UseIAP        *bool    // nil = auto, true = force IAP, false = no IAP
	SSHFlags      []string // Additional SSH flags
	RememberFlags bool     // Whether to persist SSH flags for future connections
}

// RunSSHSession suspends the TUI and runs an SSH session to the specified instance.
// This function blocks until the SSH session ends.
// The caller should update status directly after this function returns (not via QueueUpdateDraw).
func RunSSHSession(app *tview.Application, name, project, zone string, opts SSHOptions, outputRedir *outputRedirector) {
	if app == nil {
		return
	}

	// Mark the instance and project as used for future priority ordering
	if cacheStore, err := gcp.LoadCache(); err == nil && cacheStore != nil {
		_ = cacheStore.MarkInstanceUsed(name, project)
		_ = cacheStore.MarkProjectUsed(project)
	}

	// Save the IAP preference if explicitly set (not Auto)
	if opts.UseIAP != nil {
		SaveIAPPreference(name, project, zone, *opts.UseIAP)
	}

	// Save SSH flags if remember is enabled
	if opts.RememberFlags {
		SaveSSHFlags(name, project, zone, opts.SSHFlags)
	}

	app.Suspend(func() {
		args := []string{
			"compute",
			"ssh",
			name,
			"--project=" + project,
			"--zone=" + zone,
		}

		// Handle IAP option
		if opts.UseIAP != nil {
			if *opts.UseIAP {
				args = append(args, "--tunnel-through-iap")
			}
			// If UseIAP is explicitly false, don't add the flag (let gcloud decide)
		}

		// Add SSH flags
		for _, flag := range opts.SSHFlags {
			if flag != "" {
				args = append(args, "--ssh-flag="+flag)
			}
		}

		cmd := exec.Command("gcloud", args...)
		cmd.Stdin = os.Stdin

		if outputRedir != nil {
			cmd.Stdout = outputRedir.OrigStdout()
			cmd.Stderr = outputRedir.OrigStderr()
		} else {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}

		if err := cmd.Run(); err != nil {
			if outputRedir != nil {
				_, _ = fmt.Fprintln(outputRedir.OrigStdout(), "\nPress Enter to return to TUI...")
			} else {
				fmt.Println("\nPress Enter to return to TUI...")
			}
			_, _ = fmt.Fscanln(os.Stdin)
		}
	})
}

// LoadIAPPreference loads the cached IAP preference for an instance.
// Returns nil if no preference is stored.
func LoadIAPPreference(instanceName string) *bool {
	cacheStore, err := gcp.LoadCache()
	if err != nil || cacheStore == nil || instanceName == "" {
		return nil
	}

	info, found := cacheStore.Get(instanceName)
	if !found || info == nil || info.IAP == nil {
		return nil
	}

	// Return a copy of the value
	val := *info.IAP
	return &val
}

// SaveIAPPreference saves the IAP preference for an instance.
func SaveIAPPreference(instanceName, project, zone string, useIAP bool) {
	if instanceName == "" {
		return
	}

	cacheStore, err := gcp.LoadCache()
	if err != nil || cacheStore == nil {
		return
	}

	// Get existing info or create new
	info, found := cacheStore.Get(instanceName)
	if !found || info == nil {
		info = &cache.LocationInfo{
			Project: project,
			Zone:    zone,
			Type:    cache.ResourceTypeInstance,
		}
	} else {
		// Update project/zone if they were empty
		if info.Project == "" {
			info.Project = project
		}
		if info.Zone == "" {
			info.Zone = zone
		}
	}

	info.IAP = &useIAP
	_ = cacheStore.Set(instanceName, info)
}

// LoadSSHFlags loads the cached SSH flags for an instance.
// Returns nil if no flags are stored.
func LoadSSHFlags(instanceName, project string) []string {
	cacheStore, err := gcp.LoadCache()
	if err != nil || cacheStore == nil || instanceName == "" {
		return nil
	}

	var info *cache.LocationInfo
	var found bool

	if project != "" {
		info, found = cacheStore.GetWithProject(instanceName, project)
	} else {
		info, found = cacheStore.Get(instanceName)
	}

	if !found || info == nil {
		return nil
	}

	return info.SSHFlags
}

// SaveSSHFlags saves SSH flags for an instance.
func SaveSSHFlags(instanceName, project, zone string, flags []string) {
	if instanceName == "" {
		return
	}

	cacheStore, err := gcp.LoadCache()
	if err != nil || cacheStore == nil {
		return
	}

	// Get existing info or create new
	var info *cache.LocationInfo
	var found bool

	if project != "" {
		info, found = cacheStore.GetWithProject(instanceName, project)
	} else {
		info, found = cacheStore.Get(instanceName)
	}

	if !found || info == nil {
		info = &cache.LocationInfo{
			Project: project,
			Zone:    zone,
			Type:    cache.ResourceTypeInstance,
		}
	} else {
		if info.Project == "" {
			info.Project = project
		}
		if info.Zone == "" {
			info.Zone = zone
		}
	}

	info.SSHFlags = flags
	_ = cacheStore.Set(instanceName, info)
}

// ShowSSHOptionsModal displays a modal to configure SSH options before connecting.
// It calls onConnect with the selected options when the user confirms, or onCancel when cancelled.
// cachedIAP is the previously stored IAP preference for this instance (nil = no preference).
// defaultUseIAP is the suggested default based on instance properties (e.g., no external IP).
// cachedSSHFlags is the previously stored SSH flags for this instance (nil = no saved flags).
func ShowSSHOptionsModal(app *tview.Application, instanceName string, defaultUseIAP bool, cachedIAP *bool, cachedSSHFlags []string, onConnect func(opts SSHOptions), onCancel func()) {
	// Create form
	form := tview.NewForm()

	// IAP dropdown options
	iapOptions := []string{"Auto", "Yes", "No"}
	iapIndex := 0 // Default to Auto
	// Use cached IAP preference first if available, otherwise fall back to default
	if cachedIAP != nil {
		if *cachedIAP {
			iapIndex = 1 // Yes
		} else {
			iapIndex = 2 // No
		}
	} else if defaultUseIAP {
		iapIndex = 1 // Default to Yes if instance suggests IAP
	}

	var selectedIAP string
	var sshFlagsInput string
	var rememberFlagsChecked bool

	// Pre-fill SSH flags from cache
	defaultSSHFlags := ""
	if len(cachedSSHFlags) > 0 {
		defaultSSHFlags = strings.Join(cachedSSHFlags, " ")
		rememberFlagsChecked = true // Default to checked when flags already saved
	}

	form.AddDropDown("IAP Tunnel", iapOptions, iapIndex, func(option string, index int) {
		selectedIAP = option
	})

	form.AddInputField("SSH Flags", defaultSSHFlags, 20, nil, func(text string) {
		sshFlagsInput = text
	})

	form.AddCheckbox("Remember Flags", rememberFlagsChecked, func(checked bool) {
		rememberFlagsChecked = checked
	})

	form.AddButton("Connect", func() {
		opts := SSHOptions{}

		// Parse IAP selection
		switch selectedIAP {
		case "Yes":
			useIAP := true
			opts.UseIAP = &useIAP
		case "No":
			useIAP := false
			opts.UseIAP = &useIAP
		default: // "Auto" or empty
			if defaultUseIAP {
				useIAP := true
				opts.UseIAP = &useIAP
			}
			// else leave nil for auto
		}

		// Parse SSH flags (space-separated)
		if sshFlagsInput != "" {
			opts.SSHFlags = strings.Fields(sshFlagsInput)
		}

		opts.RememberFlags = rememberFlagsChecked

		onConnect(opts)
	})

	form.AddButton("Cancel", func() {
		onCancel()
	})

	// Style the form
	form.SetBorder(true)
	form.SetTitle(fmt.Sprintf(" SSH to %s ", instanceName))
	form.SetTitleAlign(tview.AlignCenter)
	form.SetButtonsAlign(tview.AlignCenter)

	// Handle Escape key
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			onCancel()
			return nil
		}
		return event
	})

	// Create a centered modal layout
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 13, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)

	app.SetRoot(modal, true)
	// Focus on the Connect button (index 3: after dropdown, input field, and checkbox)
	form.SetFocus(3)
	app.SetFocus(form)
}

// MIGInstanceSelection represents a selected instance from a MIG.
type MIGInstanceSelection struct {
	Name   string
	Zone   string
	Status string
}

// ShowMIGInstanceSelectionModal displays a modal to select an instance from a MIG.
// It shows all instances with their status and lets the user choose one.
// onSelect is called with the selected instance when the user confirms.
// onCancel is called when the user cancels the selection.
func ShowMIGInstanceSelectionModal(app *tview.Application, migName string, instances []gcp.ManagedInstanceRef, onSelect func(instance MIGInstanceSelection), onCancel func()) {
	// Create table for instance selection
	table := tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0)

	// Truncate MIG name if too long for title
	titleMigName := migName
	if len(titleMigName) > 40 {
		titleMigName = titleMigName[:37] + "..."
	}
	table.SetBorder(true).SetTitle(fmt.Sprintf(" Select Instance (%s) ", titleMigName))

	// Add header row
	headers := []string{"Name", "Status", "Zone"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetSelectable(false).
			SetExpansion(1)
		table.SetCell(0, col, cell)
	}

	// Add instance rows
	for i, inst := range instances {
		row := i + 1

		// Name column
		table.SetCell(row, 0, tview.NewTableCell(inst.Name).SetExpansion(1))

		// Status column with color
		statusColor := tcell.ColorRed
		if inst.IsRunning() {
			statusColor = tcell.ColorGreen
		}
		table.SetCell(row, 1, tview.NewTableCell(inst.Status).SetTextColor(statusColor).SetExpansion(1))

		// Zone column
		table.SetCell(row, 2, tview.NewTableCell(inst.Zone).SetExpansion(1))
	}

	// Status bar
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [yellow]Enter[-] select  [yellow]↑/↓[-] navigate  [yellow]Esc[-] cancel")

	// Main layout
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true).
		AddItem(status, 1, 0, false)

	// Calculate modal dimensions
	// Height: header + instances + border (2) + status (1)
	modalHeight := len(instances) + 4
	if modalHeight > 15 {
		modalHeight = 15
	}
	if modalHeight < 6 {
		modalHeight = 6
	}

	// Width: accommodate long instance names
	modalWidth := 90

	// Create centered modal container
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(flex, modalHeight, 0, true).
			AddItem(nil, 0, 1, false), modalWidth, 0, true).
		AddItem(nil, 0, 1, false)

	// Handle keyboard input
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			onCancel()
			return nil
		case tcell.KeyEnter:
			row, _ := table.GetSelection()
			if row > 0 && row <= len(instances) {
				selected := instances[row-1]
				onSelect(MIGInstanceSelection{
					Name:   selected.Name,
					Zone:   selected.Zone,
					Status: selected.Status,
				})
			}
			return nil
		}
		return event
	})

	// Select first data row
	table.Select(1, 0)

	app.SetRoot(modal, true).SetFocus(table)
}

// ExecuteDetails fetches and displays instance details.
// Note: The caller should set any "loading" status message before calling this method,
// as this method spawns a goroutine and returns immediately.
func (e *InstanceActionExecutor) ExecuteDetails(ctx *ActionContext, showDetailFunc func(details string)) {
	go func() {
		client, err := gcp.NewClient(ctx.Ctx, e.Project)
		if err != nil {
			if ctx.OnError != nil {
				ctx.OnError(fmt.Errorf("failed to create client: %w", err))
			}
			return
		}

		instance, err := client.FindInstance(ctx.Ctx, e.Name, e.Zone)
		if err != nil {
			if ctx.OnError != nil {
				ctx.OnError(fmt.Errorf("failed to fetch instance: %w", err))
			}
			return
		}

		if instance == nil {
			if ctx.OnError != nil {
				ctx.OnError(fmt.Errorf("instance not found"))
			}
			return
		}

		// Mark the instance and project as used for future priority ordering
		if cacheStore, cacheErr := gcp.LoadCache(); cacheErr == nil && cacheStore != nil {
			_ = cacheStore.MarkInstanceUsed(e.Name, e.Project)
			_ = cacheStore.MarkProjectUsed(e.Project)
		}

		// Format instance details
		details := FormatInstanceDetails(instance, e.Project)

		ctx.App.QueueUpdateDraw(func() {
			showDetailFunc(details)
		})
	}()
}

// FormatInstanceDetails formats instance information for display
func FormatInstanceDetails(instance *gcp.Instance, project string) string {
	var details strings.Builder
	details.WriteString("[yellow::b]Compute Instance[-:-:-]\n\n")

	// Basic info
	details.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]         %s\n", instance.Name))
	details.WriteString(fmt.Sprintf("[white::b]Project:[-:-:-]      %s\n", project))
	details.WriteString(fmt.Sprintf("[white::b]Zone:[-:-:-]         %s\n", instance.Zone))
	details.WriteString(fmt.Sprintf("[white::b]Status:[-:-:-]       %s\n", instance.Status))
	if instance.Description != "" {
		details.WriteString(fmt.Sprintf("[white::b]Description:[-:-:-]  %s\n", instance.Description))
	}
	if instance.CreationTimestamp != "" {
		details.WriteString(fmt.Sprintf("[white::b]Created:[-:-:-]      %s\n", instance.CreationTimestamp))
	}

	details.WriteString("\n")

	// Machine configuration
	details.WriteString("[cyan::b]Machine Configuration[-:-:-]\n")
	details.WriteString(fmt.Sprintf("  [white::b]Machine Type:[-:-:-]     %s\n", instance.MachineType))
	if instance.CPUPlatform != "" {
		details.WriteString(fmt.Sprintf("  [white::b]CPU Platform:[-:-:-]     %s\n", instance.CPUPlatform))
	}

	details.WriteString("\n")

	// Network
	details.WriteString("[cyan::b]Network[-:-:-]\n")
	details.WriteString(fmt.Sprintf("  [white::b]Internal IP:[-:-:-]      %s\n", instance.InternalIP))
	if instance.ExternalIP != "" {
		details.WriteString(fmt.Sprintf("  [white::b]External IP:[-:-:-]      %s\n", instance.ExternalIP))
	}
	if instance.Network != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Network:[-:-:-]          %s\n", instance.Network))
	}
	if instance.Subnetwork != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Subnetwork:[-:-:-]       %s\n", instance.Subnetwork))
	}
	details.WriteString(fmt.Sprintf("  [white::b]Can Use IAP:[-:-:-]      %v\n", instance.CanUseIAP))
	if len(instance.NetworkTags) > 0 {
		details.WriteString(fmt.Sprintf("  [white::b]Network Tags:[-:-:-]     %s\n", strings.Join(instance.NetworkTags, ", ")))
	}

	// Disks
	if len(instance.Disks) > 0 {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Disks[-:-:-]\n")
		for i, disk := range instance.Disks {
			bootStr := ""
			if disk.Boot {
				bootStr = " [boot]"
			}
			sizeStr := ""
			if disk.DiskSizeGb > 0 {
				sizeStr = fmt.Sprintf(" (%dGB)", disk.DiskSizeGb)
			}
			details.WriteString(fmt.Sprintf("  [white::b]Disk %d:[-:-:-]           %s%s%s\n", i+1, disk.Name, sizeStr, bootStr))
		}
	}

	details.WriteString("\n")

	// Scheduling
	details.WriteString("[cyan::b]Scheduling[-:-:-]\n")
	details.WriteString(fmt.Sprintf("  [white::b]Preemptible:[-:-:-]      %v\n", instance.Preemptible))
	details.WriteString(fmt.Sprintf("  [white::b]Auto Restart:[-:-:-]     %v\n", instance.AutomaticRestart))
	if instance.OnHostMaintenance != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Host Maintenance:[-:-:-] %s\n", instance.OnHostMaintenance))
	}

	// Service accounts
	if len(instance.ServiceAccounts) > 0 {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Identity & Access[-:-:-]\n")
		for _, sa := range instance.ServiceAccounts {
			details.WriteString(fmt.Sprintf("  [white::b]Service Account:[-:-:-]  %s\n", sa))
		}
	}

	// Labels
	if len(instance.Labels) > 0 {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Labels[-:-:-]\n")
		for k, v := range instance.Labels {
			details.WriteString(fmt.Sprintf("  %s=%s\n", k, v))
		}
	}

	// Metadata keys
	if len(instance.MetadataKeys) > 0 {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Metadata Keys[-:-:-]\n")
		details.WriteString(fmt.Sprintf("  %s\n", strings.Join(instance.MetadataKeys, ", ")))
	}

	details.WriteString("\n[darkgray]Press Esc to close[-]")
	return details.String()
}

// FormatMIGDetailsLive formats managed instance group information for display using live data
func FormatMIGDetailsLive(mig *gcp.ManagedInstanceGroup, project string, instances []gcp.ManagedInstanceRef) string {
	var details strings.Builder
	details.WriteString("[yellow::b]Managed Instance Group[-:-:-]\n\n")

	// Basic info
	details.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]              %s\n", mig.Name))
	details.WriteString(fmt.Sprintf("[white::b]Project:[-:-:-]           %s\n", project))
	locationType := "Zone"
	if mig.IsRegional {
		locationType = "Region"
	}
	details.WriteString(fmt.Sprintf("[white::b]%s:[-:-:-]%s%s\n", locationType, strings.Repeat(" ", 14-len(locationType)), mig.Location))
	if mig.Description != "" {
		details.WriteString(fmt.Sprintf("[white::b]Description:[-:-:-]       %s\n", mig.Description))
	}

	details.WriteString("\n")

	// Size and Status
	details.WriteString("[cyan::b]Size & Status[-:-:-]\n")
	details.WriteString(fmt.Sprintf("  [white::b]Target Size:[-:-:-]      %d\n", mig.TargetSize))
	details.WriteString(fmt.Sprintf("  [white::b]Current Size:[-:-:-]     %d\n", mig.CurrentSize))
	stableStr := "[red]No[-]"
	if mig.IsStable {
		stableStr = "[green]Yes[-]"
	}
	details.WriteString(fmt.Sprintf("  [white::b]Is Stable:[-:-:-]        %s\n", stableStr))

	details.WriteString("\n")

	// Template
	details.WriteString("[cyan::b]Configuration[-:-:-]\n")
	details.WriteString(fmt.Sprintf("  [white::b]Instance Template:[-:-:-] %s\n", mig.InstanceTemplate))
	if mig.BaseInstanceName != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Base Instance Name:[-:-:-] %s\n", mig.BaseInstanceName))
	}

	// Update Policy
	if mig.UpdateType != "" {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Update Policy[-:-:-]\n")
		details.WriteString(fmt.Sprintf("  [white::b]Update Type:[-:-:-]      %s\n", mig.UpdateType))
		if mig.MaxSurge != "" {
			details.WriteString(fmt.Sprintf("  [white::b]Max Surge:[-:-:-]        %s\n", mig.MaxSurge))
		}
		if mig.MaxUnavailable != "" {
			details.WriteString(fmt.Sprintf("  [white::b]Max Unavailable:[-:-:-]  %s\n", mig.MaxUnavailable))
		}
	}

	// Autoscaling
	if mig.AutoscalingEnabled {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Autoscaling[-:-:-]\n")
		details.WriteString(fmt.Sprintf("  [white::b]Min Replicas:[-:-:-]     %d\n", mig.MinReplicas))
		details.WriteString(fmt.Sprintf("  [white::b]Max Replicas:[-:-:-]     %d\n", mig.MaxReplicas))
	}

	// Named Ports
	if len(mig.NamedPorts) > 0 {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Named Ports[-:-:-]\n")
		for name, port := range mig.NamedPorts {
			details.WriteString(fmt.Sprintf("  %s: %d\n", name, port))
		}
	}

	// Target Zones (for regional MIGs)
	if len(mig.TargetZones) > 0 {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Target Zones[-:-:-]\n")
		for _, zone := range mig.TargetZones {
			details.WriteString(fmt.Sprintf("  %s\n", zone))
		}
	}

	// Instances
	if len(instances) > 0 {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Instances[-:-:-]\n")
		for _, inst := range instances {
			statusColor := "red"
			if inst.IsRunning() {
				statusColor = "green"
			}
			details.WriteString(fmt.Sprintf("  [%s]%s[-] %s (%s)\n", statusColor, inst.Status, inst.Name, inst.Zone))
		}
	}

	details.WriteString("\n[darkgray]Press Esc to close[-]")
	return details.String()
}

// FormatInstanceTemplateDetails formats instance template information for display
func FormatInstanceTemplateDetails(name, project, location string, detailsMap map[string]string) string {
	var details strings.Builder

	details.WriteString("[yellow::b]Instance Template[-:-:-]\n\n")

	// Basic info
	details.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]         %s\n", name))
	details.WriteString(fmt.Sprintf("[white::b]Project:[-:-:-]      %s\n", project))
	details.WriteString(fmt.Sprintf("[white::b]Location:[-:-:-]     %s\n", location))

	// Description (if present)
	if desc := detailsMap["description"]; desc != "" {
		details.WriteString(fmt.Sprintf("[white::b]Description:[-:-:-]  %s\n", desc))
	}

	details.WriteString("\n")

	// Machine configuration
	details.WriteString("[cyan::b]Machine Configuration[-:-:-]\n")
	if mt := detailsMap["machineType"]; mt != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Machine Type:[-:-:-]     %s\n", mt))
	}
	if cpu := detailsMap["minCpuPlatform"]; cpu != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Min CPU Platform:[-:-:-] %s\n", cpu))
	}
	if gpu := detailsMap["gpuAccelerators"]; gpu != "" {
		details.WriteString(fmt.Sprintf("  [white::b]GPU Accelerators:[-:-:-] %s\n", gpu))
	}

	details.WriteString("\n")

	// Disks
	details.WriteString("[cyan::b]Disks[-:-:-]\n")
	if disks := detailsMap["disks"]; disks != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Configuration:[-:-:-]    %s\n", disks))
	}
	if img := detailsMap["sourceImage"]; img != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Source Image:[-:-:-]     %s\n", img))
	}

	details.WriteString("\n")

	// Network
	details.WriteString("[cyan::b]Network[-:-:-]\n")
	if nets := detailsMap["networks"]; nets != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Interfaces:[-:-:-]       %s\n", nets))
	}
	if fwd := detailsMap["canIpForward"]; fwd != "" {
		details.WriteString(fmt.Sprintf("  [white::b]IP Forwarding:[-:-:-]    %s\n", fwd))
	}
	if tags := detailsMap["tags"]; tags != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Network Tags:[-:-:-]     %s\n", tags))
	}

	details.WriteString("\n")

	// Scheduling
	details.WriteString("[cyan::b]Scheduling[-:-:-]\n")
	if pre := detailsMap["preemptible"]; pre != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Preemptible:[-:-:-]      %s\n", pre))
	}
	if ar := detailsMap["automaticRestart"]; ar != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Auto Restart:[-:-:-]     %s\n", ar))
	}
	if ohm := detailsMap["onHostMaintenance"]; ohm != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Host Maintenance:[-:-:-] %s\n", ohm))
	}

	details.WriteString("\n")

	// Identity
	details.WriteString("[cyan::b]Identity & Access[-:-:-]\n")
	if sa := detailsMap["serviceAccounts"]; sa != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Service Accounts:[-:-:-] %s\n", sa))
	}

	// Labels (if present)
	if labels := detailsMap["labels"]; labels != "" {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Labels[-:-:-]\n")
		details.WriteString(fmt.Sprintf("  %s\n", labels))
	}

	// Metadata keys (if present)
	if meta := detailsMap["metadataKeys"]; meta != "" {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Metadata Keys[-:-:-]\n")
		details.WriteString(fmt.Sprintf("  %s\n", meta))
	}

	details.WriteString("\n[darkgray]Press Esc to close[-]")
	return details.String()
}

// FormatMIGDetails formats managed instance group information for display
func FormatMIGDetails(name, project, location string, detailsMap map[string]string) string {
	var details strings.Builder

	details.WriteString("[yellow::b]Managed Instance Group[-:-:-]\n\n")

	// Basic info
	details.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]             %s\n", name))
	details.WriteString(fmt.Sprintf("[white::b]Project:[-:-:-]          %s\n", project))
	details.WriteString(fmt.Sprintf("[white::b]Location:[-:-:-]         %s\n", location))
	if scope := detailsMap["scope"]; scope != "" {
		details.WriteString(fmt.Sprintf("[white::b]Scope:[-:-:-]            %s\n", scope))
	}
	if desc := detailsMap["description"]; desc != "" {
		details.WriteString(fmt.Sprintf("[white::b]Description:[-:-:-]      %s\n", desc))
	}

	details.WriteString("\n")

	// Size & Status
	details.WriteString("[cyan::b]Size & Status[-:-:-]\n")
	if target := detailsMap["targetSize"]; target != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Target Size:[-:-:-]      %s\n", target))
	}
	if current := detailsMap["currentSize"]; current != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Current Size:[-:-:-]     %s\n", current))
	}
	if status := detailsMap["status"]; status != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Status:[-:-:-]           %s\n", status))
	}

	details.WriteString("\n")

	// Instance Template
	details.WriteString("[cyan::b]Instance Configuration[-:-:-]\n")
	if tmpl := detailsMap["instanceTemplate"]; tmpl != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Instance Template:[-:-:-] %s\n", tmpl))
	}
	if baseName := detailsMap["baseInstanceName"]; baseName != "" {
		details.WriteString(fmt.Sprintf("  [white::b]Base Instance Name:[-:-:-] %s\n", baseName))
	}

	// Update Policy
	if detailsMap["updateType"] != "" || detailsMap["maxSurge"] != "" || detailsMap["maxUnavailable"] != "" {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Update Policy[-:-:-]\n")
		if updateType := detailsMap["updateType"]; updateType != "" {
			details.WriteString(fmt.Sprintf("  [white::b]Update Type:[-:-:-]      %s\n", updateType))
		}
		if maxSurge := detailsMap["maxSurge"]; maxSurge != "" {
			details.WriteString(fmt.Sprintf("  [white::b]Max Surge:[-:-:-]        %s\n", maxSurge))
		}
		if maxUnavail := detailsMap["maxUnavailable"]; maxUnavail != "" {
			details.WriteString(fmt.Sprintf("  [white::b]Max Unavailable:[-:-:-]  %s\n", maxUnavail))
		}
	}

	// Named Ports
	if ports := detailsMap["namedPorts"]; ports != "" {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Named Ports[-:-:-]\n")
		details.WriteString(fmt.Sprintf("  %s\n", ports))
	}

	// Target Zones (for regional MIGs)
	if zones := detailsMap["targetZones"]; zones != "" {
		details.WriteString("\n")
		details.WriteString("[cyan::b]Distribution Policy[-:-:-]\n")
		details.WriteString(fmt.Sprintf("  [white::b]Target Zones:[-:-:-]     %s\n", zones))
	}

	details.WriteString("\n[darkgray]Press Esc to close[-]")
	return details.String()
}

// BucketActionExecutor handles actions for storage buckets
type BucketActionExecutor struct {
	Name    string
	Project string
}

// NewBucketActionExecutor creates a new bucket action executor
func NewBucketActionExecutor(name, project string) *BucketActionExecutor {
	return &BucketActionExecutor{
		Name:    name,
		Project: project,
	}
}

// ExecuteOpen opens the bucket in the browser
func (e *BucketActionExecutor) ExecuteOpen(ctx *ActionContext) {
	url := fmt.Sprintf("https://console.cloud.google.com/storage/browser/%s?project=%s", e.Name, e.Project)

	var cmd *exec.Cmd
	switch {
	case isCommandAvailable("xdg-open"):
		cmd = exec.Command("xdg-open", url)
	case isCommandAvailable("open"):
		cmd = exec.Command("open", url)
	default:
		if ctx.OnStatusUpdate != nil {
			ctx.OnStatusUpdate(fmt.Sprintf("Open URL: %s", url))
		}
		return
	}

	if err := cmd.Start(); err != nil {
		if ctx.OnError != nil {
			ctx.OnError(err)
		}
		return
	}

	if ctx.OnStatusUpdate != nil {
		ctx.OnStatusUpdate("Opened in browser")
	}
}

// isCommandAvailable checks if a command is available in PATH
func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// buildCloudConsoleURL constructs a Google Cloud Console URL with the given path and project
func buildCloudConsoleURL(urlPath, project string) string {
	u, _ := url.Parse(cloudConsoleBaseURL)
	u.Path = urlPath
	q := u.Query()
	q.Set("project", project)
	u.RawQuery = q.Encode()
	return u.String()
}

// GetCloudConsoleURL returns the Google Cloud Console URL for a resource
func GetCloudConsoleURL(resourceType, name, project, location string, details map[string]string) string {
	switch resourceType {
	case string(search.KindComputeInstance):
		return buildCloudConsoleURL(path.Join("compute/instancesDetail/zones", location, "instances", name), project)

	case string(search.KindInstanceTemplate):
		return buildCloudConsoleURL(path.Join("compute/instanceTemplates/details", name), project)

	case string(search.KindManagedInstanceGroup):
		return buildCloudConsoleURL(path.Join("compute/instanceGroups/details", location, name), project)

	case string(search.KindBucket):
		return buildCloudConsoleURL(path.Join("storage/browser", name), project)

	case string(search.KindGKECluster):
		return buildCloudConsoleURL(path.Join("kubernetes/clusters/details", location, name, "details"), project)

	case string(search.KindGKENodePool):
		cluster := details["cluster"]
		if cluster == "" {
			cluster = "unknown"
		}
		return buildCloudConsoleURL(path.Join("kubernetes/nodepool", location, cluster, name), project)

	case string(search.KindCloudSQLInstance):
		return buildCloudConsoleURL(path.Join("sql/instances", name, "overview"), project)

	case string(search.KindCloudRunService):
		return buildCloudConsoleURL(path.Join("run/detail", location, name, "metrics"), project)

	case string(search.KindSecret):
		return buildCloudConsoleURL(path.Join("security/secret-manager/secret", name), project)

	case string(search.KindAddress):
		return buildCloudConsoleURL("networking/addresses/list", project)

	case string(search.KindDisk):
		return buildCloudConsoleURL(path.Join("compute/disksDetail/zones", location, "disks", name), project)

	case string(search.KindSnapshot):
		return buildCloudConsoleURL(path.Join("compute/snapshotsDetail/projects", project, "global/snapshots", name), project)

	case string(search.KindForwardingRule):
		return buildCloudConsoleURL(path.Join("net-services/loadbalancing/advanced/forwardingRules/details", location, name), project)

	case string(search.KindBackendService):
		return buildCloudConsoleURL(path.Join("net-services/loadbalancing/advanced/backendServices/details", name), project)

	case string(search.KindHealthCheck):
		return buildCloudConsoleURL("compute/healthChecks", project)

	case string(search.KindURLMap):
		return buildCloudConsoleURL(path.Join("net-services/loadbalancing/advanced/urlMaps/details", name), project)

	case string(search.KindVPCNetwork):
		return buildCloudConsoleURL(path.Join("networking/networks/details", name), project)

	case string(search.KindSubnet):
		return buildCloudConsoleURL(path.Join("networking/subnetworks/details", location, name), project)

	case string(search.KindFirewallRule):
		return buildCloudConsoleURL(path.Join("networking/firewalls/details", name), project)

	case string(search.KindVPNGateway):
		return buildCloudConsoleURL(path.Join("hybrid/vpn/gateways/details", location, name), project)

	case string(search.KindVPNTunnel):
		return buildCloudConsoleURL(path.Join("hybrid/vpn/tunnels/details", location, name), project)

	default:
		return buildCloudConsoleURL("home/dashboard", project)
	}
}

// OpenInBrowser opens a URL in the default browser
func OpenInBrowser(url string) error {
	var cmd *exec.Cmd
	switch {
	case isCommandAvailable("xdg-open"):
		cmd = exec.Command("xdg-open", url)
	case isCommandAvailable("open"):
		cmd = exec.Command("open", url)
	default:
		return fmt.Errorf("no browser command available")
	}
	return cmd.Start()
}
