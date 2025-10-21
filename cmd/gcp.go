package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/ssh"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	resourceTypeInstance = "instance"
	resourceTypeMIG      = "mig"
)

var (
	project      string
	zone         string
	sshFlags     []string
	resourceType string
)

var gcpCmd = &cobra.Command{
	Use:   "gcp",
	Short: "Manage GCP resources",
	Long: `Access GCP tooling including SSH connections, connectivity tests, and VPN helpers.

Use subcommands like "compass gcp ssh" to open IAP-backed SSH sessions or
"compass gcp connectivity-test" to manage Network Connectivity Tests.`,
}

var gcpSshCmd = &cobra.Command{
	Use:   "ssh [instance-name]",
	Short: "Connect to a GCP instance",
	Long: `Connect to a GCP instance or MIG using SSH with IAP tunneling.

The tool caches project and location information after first use, so subsequent
connections don't need --project or --zone flags.

Examples:
  # First connection (requires project)
  compass gcp ssh my-instance --project my-project --type instance

  # Subsequent connections (uses cached project and zone)
  compass gcp ssh my-instance

  # Auto-detect resource type (tries MIG first, then instance)
  compass gcp ssh my-resource --project my-project

  # Explicitly specify it's an instance for faster search
  compass gcp ssh my-instance --project my-project --type instance

  # Explicitly specify it's a MIG for faster search
  compass gcp ssh my-mig --project my-project --type mig

  # Specify zone to bypass cache and auto-detection
  compass gcp ssh my-instance --zone us-central1-a --project my-project

  # Combine resource type with zone for fastest search
  compass gcp ssh my-instance --type instance --zone us-central1-a --project my-project`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: gcpSSHCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		instanceName := args[0]
		logger.Log.Infof("Starting connection process for: %s", instanceName)

		// Validate resource type if specified
		if resourceType != "" && resourceType != resourceTypeInstance && resourceType != resourceTypeMIG {
			logger.Log.Fatalf("Invalid resource type '%s'. Must be 'instance' or 'mig'", resourceType)
		}

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		// If project or resource type is not specified, try to get it from cache
		var cachedResourceType string
		if project == "" || resourceType == "" {
			logger.Log.Debug("Project or resource type not specified, checking cache")
			cache, err := gcp.LoadCache()
			if err == nil && cache != nil {
				if cachedInfo, found := cache.Get(instanceName); found {
					if project == "" {
						logger.Log.Debugf("Found cached project for resource: %s", cachedInfo.Project)
						project = cachedInfo.Project
					}
					if resourceType == "" {
						cachedResourceType = string(cachedInfo.Type)
						logger.Log.Debugf("Found cached resource type for resource: %s", cachedResourceType)
					}
				}
			}
		}

		gcpClient, err := gcp.NewClient(ctx, project)
		if err != nil {
			logger.Log.Fatalf("Failed to create GCP client: %v", err)
		}

		var instance *gcp.Instance

		// Use cached resource type if available and not explicitly specified
		effectiveResourceType := resourceType
		if effectiveResourceType == "" && cachedResourceType != "" {
			effectiveResourceType = cachedResourceType
			logger.Log.Debugf("Using cached resource type: %s", effectiveResourceType)
		}

		// Check if resource type is specified or cached
		switch effectiveResourceType {
		case resourceTypeMIG:
			logger.Log.Debug("Resource type specified as MIG, presenting instances for selection")
			instance, err = resolveInstanceFromMIG(ctx, cmd, gcpClient, instanceName, zone)
			if err != nil {
				if isContextCanceled(ctx, err) {
					logger.Log.Info("Instance lookup canceled")

					return
				}
				logger.Log.Fatalf("Failed to resolve MIG instances: %v", err)
			}
		case resourceTypeInstance:
			logger.Log.Debug("Resource type specified as instance, searching instance only")
			instance, err = findInstanceWithProgress(ctx, gcpClient, instanceName, zone)
			if err != nil {
				if isContextCanceled(ctx, err) {
					logger.Log.Info("Instance lookup canceled")

					return
				}
				logger.Log.Fatalf("Failed to find instance: %v", err)
			}
		default:
			// Auto-detect: Try MIG first, then instance
			logger.Log.Debug("Attempting to find instance in MIG first")
			instance, err = resolveInstanceFromMIG(ctx, cmd, gcpClient, instanceName, zone)
			if err != nil {
				if errors.Is(err, gcp.ErrMIGNotFound) ||
					errors.Is(err, gcp.ErrNoInstancesInMIG) ||
					errors.Is(err, gcp.ErrNoInstancesInRegionalMIG) {
					logger.Log.Debugf("MIG lookup failed (%v), trying standalone instance", err)
					instance, err = findInstanceWithProgress(ctx, gcpClient, instanceName, zone)
					if err != nil {
						if isContextCanceled(ctx, err) {
							logger.Log.Info("Instance lookup canceled")

							return
						}
						logger.Log.Fatalf("Failed to find instance: %v", err)
					}

					break
				}

				if isContextCanceled(ctx, err) {
					logger.Log.Info("Instance lookup canceled")

					return
				}
				logger.Log.Fatalf("Failed to list MIG instances: %v", err)

				return
			}

			if instance == nil {
				logger.Log.Debug("No MIG instance selected, attempting standalone instance")
				// If MIG not found, try to find standalone instance
				instance, err = findInstanceWithProgress(ctx, gcpClient, instanceName, zone)
				if err != nil {
					if isContextCanceled(ctx, err) {
						logger.Log.Info("Instance lookup canceled")

						return
					}
					logger.Log.Fatalf("Failed to find instance: %v", err)
				}
			}
		}

		logger.Log.Infof("Connecting to instance: %s of project %s in zone: %s\n", instance.Name, project, instance.Zone)

		// Connect via SSH with IAP tunnel
		sshClient := ssh.NewClient()
		if err := sshClient.ConnectWithIAP(ctx, instance, project, sshFlags); err != nil {
			logger.Log.Fatalf("Failed to connect via SSH: %v", err)
		}

		gcpClient.RememberProject()
	},
}

func resolveInstanceFromMIG(ctx context.Context, cmd *cobra.Command, client *gcp.Client, migName, location string) (*gcp.Instance, error) {
	progress := newLookupProgressBar(fmt.Sprintf("Discovering instances in MIG %s", migName))
	progress.Start()
	defer progress.Stop()

	type result struct {
		refs       []gcp.ManagedInstanceRef
		isRegional bool
		err        error
	}

	resCh := make(chan result, 1)
	go func() {
		refs, isRegional, err := client.ListMIGInstances(ctx, migName, location, progress.Handle)
		resCh <- result{refs: refs, isRegional: isRegional, err: err}
	}()

	var migRes result
	select {
	case <-ctx.Done():
		progress.Fail("Canceled")
		return nil, ctx.Err()
	case migRes = <-resCh:
	}

	if migRes.err != nil {
		progressFail(progress, ctx, migRes.err, fmt.Sprintf("Failed to list instances in MIG %s", migName))

		return nil, migRes.err
	}

	refs := migRes.refs

	if len(refs) == 0 {
		progressFail(progress, ctx, nil, fmt.Sprintf("No instances in MIG %s", migName))

		return nil, fmt.Errorf("MIG '%s': %w", migName, gcp.ErrNoInstancesInMIG)
	}

	progressSuccess(progress, fmt.Sprintf("Found %d instance(s) in MIG %s", len(refs), migName))

	if len(refs) == 1 {
		selected := refs[0]
		logger.Log.Infof("MIG %s has a single instance: %s (zone %s)", migName, selected.Name, selected.Zone)

		return findInstanceWithProgress(ctx, client, selected.Name, selected.Zone)
	}

	selectedRef, err := promptMIGInstanceSelection(ctx, cmd, migName, refs)
	if err != nil {
		return nil, err
	}

	logger.Log.Infof("Selected MIG instance: %s (zone %s)", selectedRef.Name, selectedRef.Zone)

	return findInstanceWithProgress(ctx, client, selectedRef.Name, selectedRef.Zone)
}

func promptMIGInstanceSelection(ctx context.Context, cmd *cobra.Command, migName string, refs []gcp.ManagedInstanceRef) (gcp.ManagedInstanceRef, error) {
	stdin := cmd.InOrStdin()
	stdout := cmd.OutOrStdout()

	if isTerminalReader(stdin) && isTerminalWriter(stdout) {
		if selected, err := promptMIGInstanceSelectionInteractive(ctx, migName, refs); err == nil {
			return selected, nil
		} else if !isContextCanceled(ctx, err) {
			logger.Log.Debugf("Interactive selection failed, falling back to text prompt: %v", err)
		} else {
			return gcp.ManagedInstanceRef{}, err
		}
	}

	reader := bufio.NewReader(stdin)

	return promptMIGInstanceSelectionFromReader(reader, stdout, migName, refs)
}

func isTerminalReader(r io.Reader) bool {
	if f, ok := r.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}

	return false
}

func isTerminalWriter(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}

	return false
}

func promptMIGInstanceSelectionInteractive(ctx context.Context, migName string, refs []gcp.ManagedInstanceRef) (gcp.ManagedInstanceRef, error) {
	if len(refs) == 0 {
		return gcp.ManagedInstanceRef{}, fmt.Errorf("no instances available in MIG %s", migName)
	}

	defaultIdx := defaultMIGSelectionIndex(refs)
	options := make([]string, len(refs))
	for i, ref := range refs {
		options[i] = fmt.Sprintf("%s (zone: %s, status: %s)", ref.Name, ref.Zone, ref.Status)
	}

	var interrupted bool

	selectedOption, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText(fmt.Sprintf("Select instance from MIG %s", migName)).
		WithDefaultOption(options[defaultIdx]).
		WithOnInterruptFunc(func() {
			interrupted = true
		}).
		Show()
	if err != nil {
		return gcp.ManagedInstanceRef{}, err
	}

	if interrupted || isContextCanceled(ctx, nil) {
		return gcp.ManagedInstanceRef{}, context.Canceled
	}

	for i, option := range options {
		if option == selectedOption {
			return refs[i], nil
		}
	}

	return refs[defaultIdx], nil
}

func promptMIGInstanceSelectionFromReader(reader *bufio.Reader, out io.Writer, migName string, refs []gcp.ManagedInstanceRef) (gcp.ManagedInstanceRef, error) {
	defaultIdx := defaultMIGSelectionIndex(refs)

	if _, err := fmt.Fprintf(out, "Multiple instances available in MIG %s:\n", migName); err != nil {
		return gcp.ManagedInstanceRef{}, err
	}
	for i, ref := range refs {
		marker := " "
		if i == defaultIdx {
			marker = "*"
		}

		if _, err := fmt.Fprintf(out, "  [%d]%s %s (zone: %s, status: %s)\n", i+1, marker, ref.Name, ref.Zone, ref.Status); err != nil {
			return gcp.ManagedInstanceRef{}, err
		}
	}
	if _, err := fmt.Fprintf(out, "Select instance [default %d]: ", defaultIdx+1); err != nil {
		return gcp.ManagedInstanceRef{}, err
	}

	selected := defaultIdx

	for {
		input, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if _, writeErr := fmt.Fprintln(out); writeErr != nil {
					return gcp.ManagedInstanceRef{}, writeErr
				}

				break
			}

			return gcp.ManagedInstanceRef{}, err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			if _, writeErr := fmt.Fprintln(out); writeErr != nil {
				return gcp.ManagedInstanceRef{}, writeErr
			}

			break
		}

		value, err := strconv.Atoi(input)
		if err != nil || value < 1 || value > len(refs) {
			if _, writeErr := fmt.Fprintf(out, "Invalid selection. Enter a value between 1 and %d: ", len(refs)); writeErr != nil {
				return gcp.ManagedInstanceRef{}, writeErr
			}

			continue
		}

		if _, writeErr := fmt.Fprintln(out); writeErr != nil {
			return gcp.ManagedInstanceRef{}, writeErr
		}
		selected = value - 1

		break
	}

	return refs[selected], nil
}

func defaultMIGSelectionIndex(refs []gcp.ManagedInstanceRef) int {
	for idx, ref := range refs {
		if ref.IsRunning() {
			return idx
		}
	}

	return 0
}

func findInstanceWithProgress(ctx context.Context, client *gcp.Client, instanceName, zone string) (*gcp.Instance, error) {
	message := fmt.Sprintf("Searching for instance %s", instanceName)
	if strings.TrimSpace(zone) != "" {
		message = fmt.Sprintf("Searching for instance %s in %s", instanceName, zone)
	}

	progress := newLookupProgressBar(message)
	progress.Start()
	defer progress.Stop()

	type result struct {
		instance *gcp.Instance
		err      error
	}

	resCh := make(chan result, 1)
	go func() {
		inst, err := client.FindInstance(ctx, instanceName, zone, progress.Handle)
		resCh <- result{instance: inst, err: err}
	}()

	select {
	case <-ctx.Done():
		progress.Fail("Canceled")
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			progressFail(progress, ctx, res.err, fmt.Sprintf("Failed to locate instance %s", instanceName))

			return nil, res.err
		}

		if res.instance == nil {
			progressFail(progress, ctx, errors.New("instance not found"), fmt.Sprintf("Failed to locate instance %s", instanceName))

			return nil, fmt.Errorf("instance %s not found", instanceName)
		}

		displayZone := res.instance.Zone
		if strings.TrimSpace(displayZone) == "" {
			displayZone = "unknown zone"
		}

		progressSuccess(progress, fmt.Sprintf("Instance %s ready (%s)", res.instance.Name, displayZone))

		return res.instance, nil
	}
}

func progressFail(progress *lookupProgressBar, ctx context.Context, err error, message string) {
	if progress == nil {
		return
	}

	if isContextCanceled(ctx, err) {
		progress.Fail("Canceled")

		return
	}

	if strings.TrimSpace(message) == "" {
		message = "Operation failed"
	}

	if err != nil {
		progress.Fail(fmt.Sprintf("%s: %v", message, err))

		return
	}

	progress.Fail(message)
}

func progressSuccess(progress *lookupProgressBar, message string) {
	if progress == nil {
		return
	}

	if strings.TrimSpace(message) == "" {
		message = "Completed"
	}

	progress.Success(message)
}

func isContextCanceled(ctx context.Context, err error) bool {
	if ctx != nil {
		if ctxErr := ctx.Err(); errors.Is(ctxErr, context.Canceled) || errors.Is(ctxErr, context.DeadlineExceeded) {
			return true
		}
	}

	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

type lookupProgressBar struct {
	mu        sync.Mutex
	title     string
	writer    io.Writer
	enabled   bool
	started   bool
	closed    bool
	bar       *pterm.ProgressbarPrinter
	seen      map[string]struct{}
	completed map[string]struct{}
}

func newLookupProgressBar(title string) *lookupProgressBar {
	writer := os.Stderr

	return &lookupProgressBar{
		title:     title,
		writer:    writer,
		enabled:   term.IsTerminal(int(writer.Fd())),
		seen:      make(map[string]struct{}),
		completed: make(map[string]struct{}),
	}
}

func (p *lookupProgressBar) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p == nil || p.started {
		return
	}

	p.started = true

	if p.enabled {
		bar, err := pterm.DefaultProgressbar.
			WithTitle(p.title).
			WithWriter(p.writer).
			WithRemoveWhenDone(true).
			WithTotal(0).
			Start()
		if err == nil {
			p.bar = bar

			return
		}

		logger.Log.Debugf("Failed to start progress bar: %v", err)
		p.enabled = false
	}

	if _, err := fmt.Fprintf(p.writer, "%s...\n", p.title); err != nil {
		logger.Log.Debugf("Failed to write progress start: %v", err)
	}
}

func (p *lookupProgressBar) Handle(event gcp.ProgressEvent) {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return
	}

	key := gcp.FormatProgressKey(event.Key)

	if !p.enabled || p.bar == nil {
		if event.Message != "" {
			if _, err := fmt.Fprintln(p.writer, event.Message); err != nil {
				logger.Log.Debugf("Failed to write progress update: %v", err)
			}
		}

		return
	}

	if key != "" {
		if _, seen := p.seen[key]; !seen {
			p.bar.Add(1)
			p.seen[key] = struct{}{}
		}

		if event.Done {
			if _, done := p.completed[key]; !done {
				p.bar.Increment()
				p.completed[key] = struct{}{}
			}
		}
	}

	if event.Message != "" {
		p.bar.UpdateTitle(event.Message)
	}
}

func (p *lookupProgressBar) Success(message string) {
	p.stopWithPrinter(pterm.Success, message)
}

func (p *lookupProgressBar) Fail(message string) {
	p.stopWithPrinter(pterm.Error, message)
}

func (p *lookupProgressBar) Stop() {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopLocked()
}

func (p *lookupProgressBar) stopWithPrinter(printer pterm.PrefixPrinter, message string) {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopLocked()

	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	if p.enabled {
		printer.WithWriter(p.writer).Printfln("%s", message)

		return
	}

	if _, err := fmt.Fprintln(p.writer, message); err != nil {
		logger.Log.Debugf("Failed to write progress completion: %v", err)
	}
}

func (p *lookupProgressBar) stopLocked() {
	if p.closed {
		return
	}

	if p.enabled && p.bar != nil {
		if _, err := p.bar.Stop(); err != nil {
			logger.Log.Debugf("Failed to stop progress bar: %v", err)
		}
		p.bar = nil
	}

	p.closed = true
}

func init() {
	// Use PersistentFlags for project so it's inherited by subcommands like connectivity-test
	gcpCmd.PersistentFlags().StringVarP(&project, "project", "p", "", "GCP project ID")
	if err := gcpCmd.RegisterFlagCompletionFunc("project", gcpProjectCompletion); err != nil {
		logger.Log.Fatalf("Failed to register project completion: %v", err)
	}
	gcpSshCmd.Flags().StringVarP(&zone, "zone", "z", "", "GCP zone (auto-discovered if not specified)")
	gcpSshCmd.Flags().StringVarP(&resourceType, "type", "t", "", "Resource type: 'instance' or 'mig' (auto-detected if not specified)")
	gcpSshCmd.Flags().StringSliceVar(&sshFlags, "ssh-flag", []string{}, "Additional SSH flags to pass to the SSH command (can be used multiple times)")
	if err := gcpSshCmd.RegisterFlagCompletionFunc("zone", gcpSSHZoneCompletion); err != nil {
		logger.Log.Fatalf("Failed to register zone completion: %v", err)
	}

	gcpCmd.AddCommand(gcpSshCmd)
}
