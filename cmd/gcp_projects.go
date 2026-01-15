package cmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var gcpProjectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Manage GCP projects in cache",
	Long:  `Manage the list of GCP projects that compass will search across when no --project flag is specified.`,
}

var gcpProjectsImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import GCP projects and scan their resources into cache",
	Long: `Discover all GCP projects you have access to and select which ones to cache.

After selecting projects, this command will scan and cache:
  - Zones available in each project
  - All compute instances (for faster SSH lookups)
  - All subnets (for faster IP lookups)

This pre-populates the cache so subsequent commands run faster.
Compass will search across these cached projects when looking for instances/MIGs without a --project flag.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		runProjectsImport(ctx)
	},
}

func init() {
	gcpCmd.AddCommand(gcpProjectsCmd)
	gcpProjectsCmd.AddCommand(gcpProjectsImportCmd)
}

// runProjectsImport discovers all accessible GCP projects and prompts the user to select
// which projects to cache for future use.
func runProjectsImport(ctx context.Context) {
	// Discover all projects
	logger.Log.Info("Discovering all accessible GCP projects...")
	projects, err := gcp.ListAllProjects(ctx)
	if err != nil {
		logger.Log.Fatalf("Failed to discover projects: %v", err)
	}

	if len(projects) == 0 {
		logger.Log.Info("No projects found. Make sure you have the necessary permissions.")
		return
	}

	logger.Log.Infof("Found %d projects", len(projects))

	// Use pterm interactive multiselect
	selectedProjects, err := pterm.DefaultInteractiveMultiselect.
		WithOptions(projects).
		WithDefaultText("Select projects to cache (use arrow keys, space to select, enter to confirm):").
		WithMaxHeight(15).
		Show()

	if err != nil {
		logger.Log.Fatalf("Failed to show project selector: %v", err)
	}

	if len(selectedProjects) == 0 {
		logger.Log.Info("No projects selected. Cache unchanged.")
		return
	}

	logger.Log.Infof("Selected %d projects", len(selectedProjects))

	// Load or create cache
	cache, err := gcp.LoadCache()
	if err != nil {
		logger.Log.Fatalf("Failed to load cache: %v", err)
	}

	// Add selected projects to cache
	successCount := 0
	for _, project := range selectedProjects {
		if err := cache.AddProject(project); err != nil {
			logger.Log.Warnf("Failed to add project %s: %v", project, err)
		} else {
			successCount++
		}
	}

	logger.Log.Infof("Successfully cached %d projects", successCount)

	// Scan resources for all selected projects
	logger.Log.Info("Scanning resources for selected projects...")
	scanProjectResources(ctx, selectedProjects)

	pterm.Success.Printfln("Projects imported and resources cached! You can now use 'compass gcp ssh' without --project flag.")
}

// scanProjectResources scans zones, instances, and subnets for each project and caches them.
func scanProjectResources(ctx context.Context, projects []string) {
	// Create a multi-progress display
	multi := pterm.DefaultMultiPrinter

	// Start the multi printer
	_, _ = multi.Start()

	// Track results
	type scanResult struct {
		project string
		stats   *gcp.ScanStats
		err     error
	}

	results := make(chan scanResult, len(projects))

	// Create spinners for each project
	spinners := make(map[string]*pterm.SpinnerPrinter)
	var spinnerMu sync.Mutex

	for _, project := range projects {
		spinner, _ := pterm.DefaultSpinner.WithWriter(multi.NewWriter()).Start(fmt.Sprintf("Scanning %s...", project))
		spinners[project] = spinner
	}

	// Scan each project concurrently
	var wg sync.WaitGroup
	for _, project := range projects {
		wg.Add(1)
		go func(proj string) {
			defer wg.Done()

			client, err := gcp.NewClient(ctx, proj)
			if err != nil {
				results <- scanResult{project: proj, err: err}
				return
			}

			stats, err := client.ScanProjectResources(ctx, func(resource string, count int) {
				spinnerMu.Lock()
				if spinner, ok := spinners[proj]; ok {
					spinner.UpdateText(fmt.Sprintf("Scanning %s: %d %s found...", proj, count, resource))
				}
				spinnerMu.Unlock()
			})

			results <- scanResult{project: proj, stats: stats, err: err}
		}(project)
	}

	// Wait for all scans to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect and display results
	totalStats := &gcp.ScanStats{}
	for result := range results {
		spinnerMu.Lock()
		spinner := spinners[result.project]
		spinnerMu.Unlock()

		if result.err != nil {
			spinner.Fail(fmt.Sprintf("%s: failed (%v)", result.project, result.err))
		} else if result.stats != nil {
			totalStats.Zones += result.stats.Zones
			totalStats.Instances += result.stats.Instances
			totalStats.Subnets += result.stats.Subnets
			spinner.Success(fmt.Sprintf("%s: %d zones, %d instances, %d subnets",
				result.project, result.stats.Zones, result.stats.Instances, result.stats.Subnets))
		}
	}

	// Stop the multi printer
	_, _ = multi.Stop()

	// Summary
	logger.Log.Infof("Total cached: %d zones, %d instances, %d subnets across %d projects",
		totalStats.Zones, totalStats.Instances, totalStats.Subnets, len(projects))
}
