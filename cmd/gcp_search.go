package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/kedare/compass/internal/gcp"
	"github.com/kedare/compass/internal/gcp/search"
	"github.com/kedare/compass/internal/logger"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

// resourceSearchEngine captures the engine behaviour used by the Cobra command.
type resourceSearchEngine interface {
	Search(ctx context.Context, projects []string, query search.Query) ([]search.Result, error)
}

var (
	cachedProjectsProvider = func() ([]string, bool, error) {
		cacheStore, err := gcp.LoadCache()
		if err != nil {
			return nil, false, err
		}

		if cacheStore == nil {
			return nil, false, nil
		}

		return cacheStore.GetProjects(), true, nil
	}
	instanceProviderFactory = func() search.Provider {
		return &search.InstanceProvider{
			NewClient: func(ctx context.Context, project string) (search.InstanceClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	searchEngineFactory = func(providers ...search.Provider) resourceSearchEngine {
		return search.NewEngine(providers...)
	}
)

var gcpSearchCmd = &cobra.Command{
	Use:   "search <name-fragment>",
	Short: "Search cached GCP projects for resources by name",
	Long: `Search across the cached GCP projects (or a --project override) for resources that
contain the provided text. The search currently covers Compute Engine instances and
returns every match along with the project and location.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		projects, err := resolveSearchProjects()
		if err != nil {
			return err
		}

		engine := searchEngineFactory(instanceProviderFactory())
		query := search.Query{Term: args[0]}

		spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Searching %d project(s)...", len(projects)))
		results, searchErr := engine.Search(ctx, projects, query)
		if searchErr != nil {
			if spinner != nil {
				spinner.Fail("Search failed")
			}

			return searchErr
		}
		if spinner != nil {
			spinner.Success(fmt.Sprintf("Found %d matching resource(s)", len(results)))
		}

		displaySearchResults(results)

		return nil
	},
}

// resolveSearchProjects returns the list of projects to search.
func resolveSearchProjects() ([]string, error) {
	if project != "" {
		return []string{strings.TrimSpace(project)}, nil
	}

	projects, cacheAvailable, err := cachedProjectsProvider()
	if err != nil {
		logger.Log.Debugf("Failed to load cache for search: %v", err)
		return nil, fmt.Errorf("failed to load project cache: %w", err)
	}

	if !cacheAvailable {
		return nil, errors.New("project cache disabled. Use --project to target a specific project")
	}

	trimmed := enumerateProjects(projects)
	if len(trimmed) == 0 {
		return nil, errors.New("no cached projects available. Run 'compass gcp projects import' or pass --project")
	}

	return trimmed, nil
}

// displaySearchResults renders the search matches to stdout using pterm tables.
func displaySearchResults(results []search.Result) {
	if len(results) == 0 {
		pterm.Info.Println("No resources matched your query.")
		return
	}

	headers := []string{"TYPE", "PROJECT", "LOCATION", "NAME", "DETAILS"}
	rows := make([][]string, 0, len(results)+1)
	rows = append(rows, headers)

	for _, result := range results {
		rows = append(rows, []string{
			string(result.Type),
			result.Project,
			result.Location,
			result.Name,
			formatResultDetails(result.Details),
		})
	}

	if err := pterm.DefaultTable.WithBoxed(true).WithHasHeader().WithData(rows).Render(); err != nil {
		logger.Log.Warnf("Failed to render search results table: %v", err)
		for _, row := range rows[1:] {
			pterm.Println(strings.Join(row, "\t"))
		}
	}
}

// formatResultDetails generates a deterministic details summary for a resource row.
func formatResultDetails(details map[string]string) string {
	if len(details) == 0 {
		return ""
	}

	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(details[key]); value != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", key, value))
		}
	}

	return strings.Join(parts, ", ")
}

func init() {
	gcpCmd.AddCommand(gcpSearchCmd)
}
