package cmd

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/logger"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var regexFilter string

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
Compass will search across these cached projects when looking for instances/MIGs without a --project flag.

Examples:
  # Interactive selection
  compass gcp projects import

  # Import all projects matching a regex pattern
  compass gcp projects import --regex "^prod-.*"
  compass gcp projects import -r "dev|staging"`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		runProjectsImport(ctx, regexFilter)
	},
}

var gcpProjectsRemoveCmd = &cobra.Command{
	Use:   "remove <project-name>",
	Short: "Remove a GCP project from cache",
	Long: `Remove a GCP project and all its associated cached resources.

This removes the project from the cache along with all related data:
  - The project entry itself
  - All cached instances for this project
  - All cached zones for this project
  - All cached subnets for this project

Examples:
  compass gcp projects remove my-project`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runProjectsRemove(args[0])
	},
	ValidArgsFunction: projectNameCompletion,
}

var gcpProjectsRefreshCmd = &cobra.Command{
	Use:   "refresh <project-name>",
	Short: "Refresh cached resources for a GCP project",
	Long: `Re-scan and update cached resources for a specific GCP project.

This will refresh the cache with current data from GCP:
  - Zones available in the project
  - All compute instances
  - All subnets

The project must already be in the cache. Use 'compass gcp projects import' to add new projects.

Examples:
  compass gcp projects refresh my-project`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		runProjectsRefresh(ctx, args[0])
	},
	ValidArgsFunction: projectNameCompletion,
}

func init() {
	gcpCmd.AddCommand(gcpProjectsCmd)
	gcpProjectsCmd.AddCommand(gcpProjectsImportCmd)
	gcpProjectsCmd.AddCommand(gcpProjectsRemoveCmd)
	gcpProjectsCmd.AddCommand(gcpProjectsRefreshCmd)
	gcpProjectsImportCmd.Flags().StringVarP(&regexFilter, "regex", "r", "", "Regex pattern to filter projects (bypasses interactive selection)")
}

// projectNameCompletion provides shell completion for cached project names.
func projectNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	cache, err := gcp.LoadCache()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	projects := cache.GetProjects()

	return projects, cobra.ShellCompDirectiveNoFileComp
}

// runProjectsRemove removes a project and all its cached resources.
func runProjectsRemove(projectName string) {
	cache, err := gcp.LoadCache()
	if err != nil {
		logger.Log.Fatalf("Failed to load cache: %v", err)
	}

	if !cache.HasProject(projectName) {
		logger.Log.Fatalf("Project '%s' is not in the cache", projectName)
	}

	if err := cache.DeleteProject(projectName); err != nil {
		logger.Log.Fatalf("Failed to remove project: %v", err)
	}

	pterm.Success.Printfln("Project '%s' removed from cache", projectName)
}

// runProjectsRefresh rescans and updates cached resources for a specific project.
func runProjectsRefresh(ctx context.Context, projectName string) {
	cache, err := gcp.LoadCache()
	if err != nil {
		logger.Log.Fatalf("Failed to load cache: %v", err)
	}

	if !cache.HasProject(projectName) {
		logger.Log.Fatalf("Project '%s' is not in the cache. Use 'compass gcp projects import' to add it first.", projectName)
	}

	logger.Log.Infof("Refreshing cached resources for project: %s", projectName)

	scanProjectResources(ctx, []string{projectName})

	pterm.Success.Printfln("Project '%s' cache refreshed!", projectName)
}

// runProjectsImport discovers all accessible GCP projects and prompts the user to select
// which projects to cache for future use. If regexPattern is provided, projects matching
// the pattern are automatically selected without interactive prompting.
func runProjectsImport(ctx context.Context, regexPattern string) {
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

	var selectedProjects []string

	if regexPattern != "" {
		// Filter projects by regex pattern
		var err error
		selectedProjects, err = filterProjectsByRegex(projects, regexPattern)
		if err != nil {
			logger.Log.Fatalf("Invalid regex pattern: %v", err)
		}

		if len(selectedProjects) == 0 {
			logger.Log.Infof("No projects matched the pattern '%s'", regexPattern)
			return
		}

		logger.Log.Infof("Found %d projects matching pattern '%s'", len(selectedProjects), regexPattern)
	} else {
		// Use pterm interactive multiselect
		selectedProjects, err = pterm.DefaultInteractiveMultiselect.
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
	}

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
	// 3 API operations per project: zones, instances, subnets
	totalAPICalls := len(projects) * 3
	var completedAPICalls int
	var apiCallsMu sync.Mutex

	// Track totals
	var totalStats gcp.ScanStats
	var statsMu sync.Mutex

	// Track errors
	var errors []string
	var errorsMu sync.Mutex

	// Create progress bar - title will be updated with current status
	progressBar, _ := pterm.DefaultProgressbar.
		WithTotal(totalAPICalls).
		WithTitle("Starting scan...").
		WithRemoveWhenDone(true).
		WithShowPercentage(true).
		WithShowCount(true).
		Start()

	// Function to update progress bar title with current stats
	updateTitle := func(currentProject, currentOp string) {
		statsMu.Lock()
		zones := totalStats.Zones
		instances := totalStats.Instances
		subnets := totalStats.Subnets
		statsMu.Unlock()

		progressBar.UpdateTitle(fmt.Sprintf("%s (%s) | %d zones, %d instances, %d subnets",
			currentProject, currentOp, zones, instances, subnets))
	}

	// Scan each project concurrently with limited parallelism
	semaphore := make(chan struct{}, 10) // Limit to 10 concurrent scans
	var wg sync.WaitGroup

	for _, project := range projects {
		wg.Add(1)
		go func(proj string) {
			defer wg.Done()

			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			client, err := gcp.NewClient(ctx, proj)
			if err != nil {
				errorsMu.Lock()
				errors = append(errors, fmt.Sprintf("%s: %v", proj, err))
				errorsMu.Unlock()

				// Count all 3 operations as done (failed)
				apiCallsMu.Lock()
				completedAPICalls += 3
				progressBar.Add(3)
				apiCallsMu.Unlock()
				return
			}

			_, err = client.ScanProjectResources(ctx, func(resource string, count int) {
				// Update totals based on resource type
				statsMu.Lock()
				switch resource {
				case "zones":
					totalStats.Zones += count
				case "instances":
					totalStats.Instances += count
				case "subnets":
					totalStats.Subnets += count
				}
				statsMu.Unlock()

				// Increment progress for this operation
				apiCallsMu.Lock()
				completedAPICalls++
				progressBar.Increment()
				apiCallsMu.Unlock()

				updateTitle(proj, resource)
			})

			if err != nil {
				errorsMu.Lock()
				errors = append(errors, fmt.Sprintf("%s: %v", proj, err))
				errorsMu.Unlock()
			}
		}(project)
	}

	// Wait for all scans to complete
	wg.Wait()

	// Ensure progress bar completes
	apiCallsMu.Lock()
	remaining := totalAPICalls - completedAPICalls
	if remaining > 0 {
		progressBar.Add(remaining)
	}
	apiCallsMu.Unlock()

	// Stop removes the progress bar, then we print summary
	_, _ = progressBar.Stop()

	// Print summary
	statsMu.Lock()
	pterm.Success.Printfln("Cached: %d zones, %d instances, %d subnets across %d projects",
		totalStats.Zones, totalStats.Instances, totalStats.Subnets, len(projects))
	statsMu.Unlock()

	// Print errors if any
	errorsMu.Lock()
	if len(errors) > 0 {
		pterm.Warning.Printfln("%d project(s) had errors:", len(errors))
		for _, e := range errors {
			pterm.Warning.Printfln("  - %s", e)
		}
	}
	errorsMu.Unlock()
}

// filterProjectsByRegex filters a list of projects by a regex pattern.
// Returns the matching projects or an error if the pattern is invalid.
func filterProjectsByRegex(projects []string, pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	var matched []string
	for _, project := range projects {
		if re.MatchString(project) {
			matched = append(matched, project)
		}
	}

	return matched, nil
}
