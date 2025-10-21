package cmd

import (
	"context"

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
	Short: "Import GCP projects into cache",
	Long: `Discover all GCP projects you have access to and select which ones to cache.
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
	pterm.Success.Printfln("Projects imported successfully! You can now use 'compass gcp ssh' without --project flag.")
}
