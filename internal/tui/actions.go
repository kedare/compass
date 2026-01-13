package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/gcp/search"
	"github.com/rivo/tview"
)

// ResourceAction represents an action that can be performed on a resource
type ResourceAction struct {
	Key         rune   // Key to trigger the action
	Name        string // Display name for the action
	Description string // Short description
}

// ActionContext provides context for executing actions
type ActionContext struct {
	App           *tview.Application
	Ctx           context.Context
	OutputRedir   *outputRedirector
	OnStatusUpdate func(msg string)
	OnError       func(err error)
	OnComplete    func()
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
		}
	case string(search.KindManagedInstanceGroup):
		return []ResourceAction{
			{Key: 'd', Name: "Details", Description: "Show details"},
		}
	case string(search.KindBucket):
		return []ResourceAction{
			{Key: 'd', Name: "Details", Description: "Show details"},
			{Key: 'o', Name: "Open", Description: "Open in browser"},
		}
	case string(search.KindGKECluster):
		return []ResourceAction{
			{Key: 'd', Name: "Details", Description: "Show details"},
		}
	case string(search.KindCloudSQLInstance):
		return []ResourceAction{
			{Key: 'd', Name: "Details", Description: "Show details"},
		}
	default:
		// Default actions for any resource
		return []ResourceAction{
			{Key: 'd', Name: "Details", Description: "Show details"},
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
	Name     string
	Project  string
	Zone     string
	UseIAP   bool
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
	UseIAP   *bool    // nil = auto, true = force IAP, false = no IAP
	SSHFlags []string // Additional SSH flags
}

// RunSSHSession suspends the TUI and runs an SSH session to the specified instance.
// This function blocks until the SSH session ends.
// The caller should update status directly after this function returns (not via QueueUpdateDraw).
func RunSSHSession(app *tview.Application, name, project, zone string, opts SSHOptions, outputRedir *outputRedirector) {
	if app == nil {
		return
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
				fmt.Fprintln(outputRedir.OrigStdout(), "\nPress Enter to return to TUI...")
			} else {
				fmt.Println("\nPress Enter to return to TUI...")
			}
			_, _ = fmt.Fscanln(os.Stdin)
		}
	})
}

// ShowSSHOptionsModal displays a modal to configure SSH options before connecting.
// It calls onConnect with the selected options when the user confirms, or onCancel when cancelled.
func ShowSSHOptionsModal(app *tview.Application, instanceName string, defaultUseIAP bool, onConnect func(opts SSHOptions), onCancel func()) {
	// Create form
	form := tview.NewForm()

	// IAP dropdown options
	iapOptions := []string{"Auto", "Yes", "No"}
	iapIndex := 0 // Default to Auto
	if defaultUseIAP {
		iapIndex = 1 // Default to Yes if instance suggests IAP
	}

	var selectedIAP string
	var sshFlagsInput string

	form.AddDropDown("IAP Tunnel", iapOptions, iapIndex, func(option string, index int) {
		selectedIAP = option
	})

	form.AddInputField("SSH Flags", "", 20, nil, func(text string) {
		sshFlagsInput = text
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
			AddItem(form, 11, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)

	app.SetRoot(modal, true)
	// Focus on the Connect button (index 2: after dropdown and input field)
	form.SetFocus(2)
	app.SetFocus(form)
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
	details.WriteString(fmt.Sprintf("[white::b]Name:[-:-:-]         %s\n", instance.Name))
	details.WriteString(fmt.Sprintf("[white::b]Project:[-:-:-]      %s\n", project))
	details.WriteString(fmt.Sprintf("[white::b]Zone:[-:-:-]         %s\n", instance.Zone))
	details.WriteString(fmt.Sprintf("[white::b]Status:[-:-:-]       %s\n", instance.Status))
	details.WriteString(fmt.Sprintf("[white::b]Machine Type:[-:-:-] %s\n", instance.MachineType))
	details.WriteString(fmt.Sprintf("[white::b]Internal IP:[-:-:-]  %s\n", instance.InternalIP))
	if instance.ExternalIP != "" {
		details.WriteString(fmt.Sprintf("[white::b]External IP:[-:-:-]  %s\n", instance.ExternalIP))
	}
	details.WriteString(fmt.Sprintf("[white::b]Can Use IAP:[-:-:-]  %v\n", instance.CanUseIAP))
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
