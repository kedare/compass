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
	"github.com/kedare/compass/internal/output"
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
		var cachedProjects []string
		var cachedProject string
		var cachedZone string

		if resourceType == "" || project == "" || zone == "" {
			logger.Log.Debug("Checking cache for resource information")
			cache, err := gcp.LoadCache()
			if err == nil && cache != nil {
				if cachedInfo, found := cache.Get(instanceName); found {
					if resourceType == "" {
						cachedResourceType = string(cachedInfo.Type)
						logger.Log.Debugf("Found cached resource type: %s", cachedResourceType)
					}
					if project == "" && cachedInfo.Project != "" {
						cachedProject = cachedInfo.Project
						logger.Log.Debugf("Found cached project: %s", cachedProject)
					}
					if zone == "" && cachedInfo.Zone != "" {
						cachedZone = cachedInfo.Zone
						logger.Log.Debugf("Found cached zone: %s", cachedZone)
					}
				}
			}
		}

		// Determine which project(s) to use
		if project == "" {
			// If we found a cached project for this specific resource, use it directly
			if cachedProject != "" {
				logger.Log.Debugf("Using cached project %s for resource %s", cachedProject, instanceName)
				project = cachedProject
				// Also use cached zone if available
				if zone == "" && cachedZone != "" {
					zone = cachedZone
					logger.Log.Debugf("Using cached zone %s for resource %s", cachedZone, instanceName)
				}
			} else {
				// No cached resource - need to search across all cached projects
				logger.Log.Debug("No cached project for this resource, checking for available projects to search")

				// Try to get projects from cache (only if cache is enabled)
				cache, err := gcp.LoadCache()
				if err == nil && cache != nil {
					cachedProjects = cache.GetProjects()
					if len(cachedProjects) > 0 {
						logger.Log.Debugf("Will search across %d cached projects", len(cachedProjects))
					} else {
						// No cached projects - fail hard
						logger.Log.Fatalf("No projects found in cache. Please specify a project with --project, or import projects with 'compass gcp projects import'")
					}
				} else {
					// Cache disabled or failed to load - fail hard
					logger.Log.Fatalf("No project specified and cache is disabled or unavailable. Please specify a project with --project")
				}
			}
		}

		// If we have a specific project or no cached projects, create a single client
		// Otherwise, we'll search across multiple projects
		var gcpClient *gcp.Client
		if project != "" || len(cachedProjects) == 0 {
			var err error
			gcpClient, err = gcp.NewClient(ctx, project)
			if err != nil {
				logger.Log.Fatalf("Failed to create GCP client: %v", err)
			}
		}

		var instance *gcp.Instance
		var err error

		// Use cached resource type if available and not explicitly specified
		effectiveResourceType := resourceType
		if effectiveResourceType == "" && cachedResourceType != "" {
			effectiveResourceType = cachedResourceType
			logger.Log.Debugf("Using cached resource type: %s", effectiveResourceType)
		}

		// If we need to search across multiple projects
		if len(cachedProjects) > 0 && gcpClient == nil {
			logger.Log.Debugf("Searching for %s across %d projects", instanceName, len(cachedProjects))
			instance, err = findInstanceAcrossProjects(ctx, cmd, instanceName, zone, effectiveResourceType, cachedProjects)
			if err != nil {
				if isContextCanceled(ctx, err) {
					logger.Log.Info("Instance lookup canceled")
					return
				}
				logger.Log.Fatalf("Failed to find instance across projects: %v", err)
			}
		} else {
			// Single project search
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
				// If we have zone from cache, skip progress indicator as we know exactly where to look
				if zone != "" && cachedZone != "" {
					logger.Log.Debug("Using cached location, skipping search spinner")
					instance, err = findInstanceWithoutProgress(ctx, gcpClient, instanceName, zone)
				} else {
					instance, err = findInstanceWithProgress(ctx, gcpClient, instanceName, zone)
				}
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
						// If we have zone from cache, skip progress indicator
						if zone != "" && cachedZone != "" {
							logger.Log.Debug("Using cached location, skipping search spinner")
							instance, err = findInstanceWithoutProgress(ctx, gcpClient, instanceName, zone)
						} else {
							instance, err = findInstanceWithProgress(ctx, gcpClient, instanceName, zone)
						}
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
					// If we have zone from cache, skip progress indicator
					if zone != "" && cachedZone != "" {
						logger.Log.Debug("Using cached location, skipping search spinner")
						instance, err = findInstanceWithoutProgress(ctx, gcpClient, instanceName, zone)
					} else {
						instance, err = findInstanceWithProgress(ctx, gcpClient, instanceName, zone)
					}
					if err != nil {
						if isContextCanceled(ctx, err) {
							logger.Log.Info("Instance lookup canceled")

							return
						}
						logger.Log.Fatalf("Failed to find instance: %v", err)
					}
				}
			}
		}

		if instance == nil {
			logger.Log.Fatalf("Failed to locate instance %s", instanceName)
		}

		// Update project from the found instance's project
		project = instance.Project

		logger.Log.Infof("Connecting to instance: %s of project %s in zone: %s", instance.Name, project, instance.Zone)

		// Connect via SSH with IAP tunnel
		sshClient := ssh.NewClient()
		if err := sshClient.ConnectWithIAP(ctx, instance, project, sshFlags); err != nil {
			logger.Log.Fatalf("Failed to connect via SSH: %v", err)
		}

		// Remember project if we have a client (won't be set for multi-project searches)
		if gcpClient != nil {
			gcpClient.RememberProject()
		}
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

// findInstanceWithoutProgress searches for an instance without creating a spinner (used in multi-project search)
func findInstanceWithoutProgress(ctx context.Context, client *gcp.Client, instanceName, zone string) (*gcp.Instance, error) {
	return client.FindInstance(ctx, instanceName, zone)
}

// resolveInstanceFromMIGWithoutProgress resolves MIG instance without creating a spinner (used in multi-project search)
func resolveInstanceFromMIGWithoutProgress(ctx context.Context, cmd *cobra.Command, client *gcp.Client, migName, zone string) (*gcp.Instance, error) {
	// Get MIG instances
	refs, found, err := client.ListMIGInstances(ctx, migName, zone)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, gcp.ErrMIGNotFound
	}
	if len(refs) == 0 {
		return nil, gcp.ErrNoInstancesInMIG
	}

	// Select instance (prefer running, otherwise first)
	var selectedRef gcp.ManagedInstanceRef
	for _, ref := range refs {
		if ref.IsRunning() {
			selectedRef = ref
			break
		}
	}
	if selectedRef.Name == "" {
		selectedRef = refs[0]
	}

	// Get full instance details
	return client.FindInstance(ctx, selectedRef.Name, selectedRef.Zone)
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

// findInstanceAcrossProjects searches for an instance across multiple projects in parallel.
// The function uses the global concurrency setting to limit the number of concurrent project searches
// and the number of concurrent progress spinners displayed.
func findInstanceAcrossProjects(ctx context.Context, cmd *cobra.Command, instanceName, zone, resourceType string, projects []string) (*gcp.Instance, error) {
	if len(projects) == 0 {
		return nil, fmt.Errorf("no projects to search")
	}

	type searchResult struct {
		instance  *gcp.Instance
		projectID string
		err       error
	}

	mainSpin := output.NewSpinner(fmt.Sprintf("Searching for %s across %d projects", instanceName, len(projects)))
	mainSpin.Start()
	defer mainSpin.Stop()

	resultCh := make(chan searchResult, len(projects))
	progressCh := make(chan string, len(projects))
	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	projectSpinners := make(map[string]*output.Spinner)
	var spinnerMu sync.Mutex

	stopAllProjectSpinners := func(finalize func(*output.Spinner)) {
		spinnerMu.Lock()
		spinners := make([]*output.Spinner, 0, len(projectSpinners))
		for key, sp := range projectSpinners {
			if sp != nil {
				spinners = append(spinners, sp)
			}
			delete(projectSpinners, key)
		}
		spinnerMu.Unlock()

		for _, sp := range spinners {
			finalize(sp)
		}
	}

	var wg sync.WaitGroup
	// Use global concurrency setting for worker pool size
	workerCount := concurrency
	if workerCount > len(projects) {
		workerCount = len(projects)
	}
	semaphore := make(chan struct{}, workerCount)

	for _, projectID := range projects {
		projectID := projectID
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Acquire semaphore
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-searchCtx.Done():
				return
			}

			// Notify that we're starting this project
			select {
			case progressCh <- projectID:
			case <-searchCtx.Done():
				return
			}

			// Create client for this project
			client, err := gcp.NewClient(searchCtx, projectID)
			if err != nil {
				logger.Log.Debugf("Failed to create client for project %s: %v", projectID, err)
				resultCh <- searchResult{projectID: projectID, err: err}
				return
			}

			var instance *gcp.Instance

			// Search based on resource type (without creating additional spinners)
			switch resourceType {
			case resourceTypeMIG:
				instance, err = resolveInstanceFromMIGWithoutProgress(searchCtx, cmd, client, instanceName, zone)
			case resourceTypeInstance:
				instance, err = findInstanceWithoutProgress(searchCtx, client, instanceName, zone)
			default:
				// Try MIG first, then instance
				instance, err = resolveInstanceFromMIGWithoutProgress(searchCtx, cmd, client, instanceName, zone)
				if err != nil && (errors.Is(err, gcp.ErrMIGNotFound) ||
					errors.Is(err, gcp.ErrNoInstancesInMIG) ||
					errors.Is(err, gcp.ErrNoInstancesInRegionalMIG)) {
					instance, err = findInstanceWithoutProgress(searchCtx, client, instanceName, zone)
				}
			}

			resultCh <- searchResult{instance: instance, projectID: projectID, err: err}

			if instance != nil {
				cancel() // Found it, stop other searches
			}
		}()
	}

	// Close channels when all goroutines complete
	go func() {
		wg.Wait()
		close(progressCh)
		close(resultCh)
	}()

	// Handle progress and results
	started := 0
	processed := 0
	total := len(projects)
	ctxDone := ctx.Done()

	for progressCh != nil || resultCh != nil {
		select {
		case <-ctxDone:
			cancel()
			stopAllProjectSpinners(func(sp *output.Spinner) {
				sp.Fail("Search canceled")
			})
			mainSpin.Fail("Search canceled")
			return nil, ctx.Err()

		case projectID, ok := <-progressCh:
			if !ok {
				progressCh = nil
				continue
			}
			started++
			spinnerMu.Lock()
			projSpinner := output.NewSpinner(fmt.Sprintf("Searching project %s", projectID))
			projSpinner.Start()
			projectSpinners[projectID] = projSpinner
			spinnerMu.Unlock()

			mainSpin.Update(fmt.Sprintf("Searching %s across projects (%d/%d started)", instanceName, started, total))

		case res, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}

			processed++

			spinnerMu.Lock()
			projSpinner := projectSpinners[res.projectID]
			delete(projectSpinners, res.projectID)
			spinnerMu.Unlock()

			if res.err != nil {
				if !isContextCanceled(searchCtx, res.err) {
					logger.Log.Debugf("Search in project %s failed: %v", res.projectID, res.err)
					if projSpinner != nil {
						projSpinner.Fail(fmt.Sprintf("Failed: %v", res.err))
					}
				} else if projSpinner != nil {
					// Context was canceled (likely because we found the instance) - just stop silently
					projSpinner.Stop()
				}
			} else if res.instance != nil {
				if projSpinner != nil {
					projSpinner.Success(fmt.Sprintf("Found in project %s", res.projectID))
				}
				mainSpin.Success(fmt.Sprintf("Found %s in project %s", instanceName, res.projectID))

				// Stop remaining spinners
				stopAllProjectSpinners(func(sp *output.Spinner) {
					sp.Stop()
				})

				return res.instance, nil
			} else {
				if projSpinner != nil {
					projSpinner.Stop()
				}
			}

			mainSpin.Update(fmt.Sprintf("Searching %s across projects (%d/%d done)", instanceName, processed, total))
		}
	}

	mainSpin.Fail(fmt.Sprintf("Instance %s not found in any project", instanceName))
	return nil, fmt.Errorf("instance %s not found in any of %d projects", instanceName, len(projects))
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
	mu          sync.Mutex
	title       string
	writer      io.Writer
	enabled     bool
	started     bool
	closed      bool
	bars        map[string]*pterm.ProgressbarPrinter
	seen        map[string]struct{}
	completed   map[string]struct{}
	mainSpinner *output.Spinner // Fallback spinner when progress bars are disabled
}

func newLookupProgressBar(title string) *lookupProgressBar {
	writer := os.Stderr

	// Always use spinner, never progress bars
	spinner := output.NewSpinner(title)

	return &lookupProgressBar{
		title:       title,
		writer:      writer,
		enabled:     false, // Disable progress bars
		bars:        make(map[string]*pterm.ProgressbarPrinter),
		seen:        make(map[string]struct{}),
		completed:   make(map[string]struct{}),
		mainSpinner: spinner,
	}
}

func (p *lookupProgressBar) Start() {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return
	}

	p.started = true

	// Always start spinner
	if p.mainSpinner != nil {
		p.mainSpinner.Start()
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

	// Always update spinner with the message
	if event.Message != "" && p.mainSpinner != nil {
		p.mainSpinner.Update(event.Message)
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

	message = strings.TrimSpace(message)

	// Always use spinner Success/Fail methods
	if p.mainSpinner != nil && message != "" {
		switch printer {
		case pterm.Success:
			p.mainSpinner.Success(message)
		case pterm.Error:
			p.mainSpinner.Fail(message)
		default:
			p.mainSpinner.Info(message)
		}
		p.mainSpinner = nil // Mark as stopped
	} else if p.mainSpinner != nil {
		// Just stop the spinner without message
		p.mainSpinner.Stop()
		p.mainSpinner = nil
	}

	p.closed = true
}

func (p *lookupProgressBar) stopLocked() {
	if p.closed {
		return
	}

	// Stop spinner if it's running
	if p.mainSpinner != nil {
		p.mainSpinner.Stop()
		p.mainSpinner = nil
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
