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
	SearchWithWarnings(ctx context.Context, projects []string, query search.Query) (*search.SearchOutput, error)
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
	migProviderFactory = func() search.Provider {
		return &search.MIGProvider{
			NewClient: func(ctx context.Context, project string) (search.MIGClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	instanceTemplateProviderFactory = func() search.Provider {
		return &search.InstanceTemplateProvider{
			NewClient: func(ctx context.Context, project string) (search.InstanceTemplateClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	addressProviderFactory = func() search.Provider {
		return &search.AddressProvider{
			NewClient: func(ctx context.Context, project string) (search.AddressClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	diskProviderFactory = func() search.Provider {
		return &search.DiskProvider{
			NewClient: func(ctx context.Context, project string) (search.DiskClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	snapshotProviderFactory = func() search.Provider {
		return &search.SnapshotProvider{
			NewClient: func(ctx context.Context, project string) (search.SnapshotClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	bucketProviderFactory = func() search.Provider {
		return &search.BucketProvider{
			NewClient: func(ctx context.Context, project string) (search.BucketClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	forwardingRuleProviderFactory = func() search.Provider {
		return &search.ForwardingRuleProvider{
			NewClient: func(ctx context.Context, project string) (search.ForwardingRuleClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	backendServiceProviderFactory = func() search.Provider {
		return &search.BackendServiceProvider{
			NewClient: func(ctx context.Context, project string) (search.BackendServiceClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	targetPoolProviderFactory = func() search.Provider {
		return &search.TargetPoolProvider{
			NewClient: func(ctx context.Context, project string) (search.TargetPoolClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	healthCheckProviderFactory = func() search.Provider {
		return &search.HealthCheckProvider{
			NewClient: func(ctx context.Context, project string) (search.HealthCheckClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	urlMapProviderFactory = func() search.Provider {
		return &search.URLMapProvider{
			NewClient: func(ctx context.Context, project string) (search.URLMapClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	cloudSQLProviderFactory = func() search.Provider {
		return &search.CloudSQLProvider{
			NewClient: func(ctx context.Context, project string) (search.CloudSQLClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	gkeClusterProviderFactory = func() search.Provider {
		return &search.GKEClusterProvider{
			NewClient: func(ctx context.Context, project string) (search.GKEClusterClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	gkeNodePoolProviderFactory = func() search.Provider {
		return &search.GKENodePoolProvider{
			NewClient: func(ctx context.Context, project string) (search.GKENodePoolClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	vpcNetworkProviderFactory = func() search.Provider {
		return &search.VPCNetworkProvider{
			NewClient: func(ctx context.Context, project string) (search.VPCNetworkClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	subnetProviderFactory = func() search.Provider {
		return &search.SubnetProvider{
			NewClient: func(ctx context.Context, project string) (search.SubnetClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	cloudRunProviderFactory = func() search.Provider {
		return &search.CloudRunProvider{
			NewClient: func(ctx context.Context, project string) (search.CloudRunClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	firewallRuleProviderFactory = func() search.Provider {
		return &search.FirewallRuleProvider{
			NewClient: func(ctx context.Context, project string) (search.FirewallRuleClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	secretProviderFactory = func() search.Provider {
		return &search.SecretProvider{
			NewClient: func(ctx context.Context, project string) (search.SecretClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	vpnGatewayProviderFactory = func() search.Provider {
		return &search.VPNGatewayProvider{
			NewClient: func(ctx context.Context, project string) (search.VPNGatewayClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	vpnTunnelProviderFactory = func() search.Provider {
		return &search.VPNTunnelProvider{
			NewClient: func(ctx context.Context, project string) (search.VPNTunnelClient, error) {
				return gcp.NewClient(ctx, project)
			},
		}
	}
	searchEngineFactory = func(parallelism int, providers ...search.Provider) resourceSearchEngine {
		engine := search.NewEngine(providers...)
		engine.MaxConcurrentProjects = parallelism
		return engine
	}
	// useSpinner controls whether to show a spinner during search (disabled in tests to avoid races)
	useSpinner = true
)

var searchTypes []string
var searchNoTypes []string
var searchParallelism int

var gcpSearchCmd = &cobra.Command{
	Use:   "search <name-fragment>",
	Short: "Search cached GCP projects for resources by name",
	Long: `Search across the cached GCP projects (or a --project override) for resources that
contain the provided text. The search covers Compute Engine instances, managed instance
groups, instance templates, IP address reservations, disks, snapshots, Cloud Storage
buckets, load balancer resources (forwarding rules, backend services, target pools,
health checks, URL maps), Cloud SQL instances, GKE clusters and node pools, VPC networks
and subnets, Cloud Run services, firewall rules, Secret Manager secrets, and Cloud VPN
resources (HA VPN gateways and tunnels). Returns every match along with the project and
location.

Use --type to filter results to specific resource types. Multiple types can be specified
by using the flag multiple times (e.g., --type compute.instance --type compute.disk).

Use --no-type to exclude specific resource types from the results. Multiple types can be
specified by using the flag multiple times (e.g., --no-type storage.bucket --no-type compute.disk).
When both --type and --no-type are used, --no-type is applied to the --type filter.`,
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

		// Parse and validate type filters
		typeFilters, err := parseSearchTypes(searchTypes, searchNoTypes)
		if err != nil {
			return err
		}

		engine := searchEngineFactory(
			searchParallelism,
			instanceProviderFactory(),
			migProviderFactory(),
			instanceTemplateProviderFactory(),
			addressProviderFactory(),
			diskProviderFactory(),
			snapshotProviderFactory(),
			bucketProviderFactory(),
			forwardingRuleProviderFactory(),
			backendServiceProviderFactory(),
			targetPoolProviderFactory(),
			healthCheckProviderFactory(),
			urlMapProviderFactory(),
			cloudSQLProviderFactory(),
			gkeClusterProviderFactory(),
			gkeNodePoolProviderFactory(),
			vpcNetworkProviderFactory(),
			subnetProviderFactory(),
			cloudRunProviderFactory(),
			firewallRuleProviderFactory(),
			secretProviderFactory(),
			vpnGatewayProviderFactory(),
			vpnTunnelProviderFactory(),
		)
		query := search.Query{Term: args[0], Types: typeFilters}

		var spinner *pterm.SpinnerPrinter
		if useSpinner {
			spinner, _ = pterm.DefaultSpinner.Start(fmt.Sprintf("Searching %d project(s)...", len(projects)))
		}
		output, searchErr := engine.SearchWithWarnings(ctx, projects, query)
		if searchErr != nil {
			if spinner != nil {
				spinner.Fail("Search failed")
			}

			return searchErr
		}
		if spinner != nil {
			spinner.Success(fmt.Sprintf("Found %d matching resource(s)", len(output.Results)))
		}

		displaySearchResults(output.Results)
		displaySearchWarnings(output.Warnings)

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

// displaySearchWarnings shows any warnings from providers that failed during search.
func displaySearchWarnings(warnings []search.SearchWarning) {
	if len(warnings) == 0 {
		return
	}

	// Group warnings by provider to avoid repeating the same API error for every project
	providerErrors := make(map[search.ResourceKind]int)
	for _, w := range warnings {
		providerErrors[w.Provider]++
	}

	pterm.Println()
	pterm.Warning.Printf("Some providers encountered errors (%d total):\n", len(warnings))
	for provider, count := range providerErrors {
		// Find a sample error for this provider
		var sampleErr error
		for _, w := range warnings {
			if w.Provider == provider {
				sampleErr = w.Err
				break
			}
		}
		pterm.Warning.Printf("  - %s: %d project(s) failed - %v\n", provider, count, sampleErr)
	}
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

// parseSearchTypes validates and converts string type filters to ResourceKind values.
// It handles both inclusion (types) and exclusion (noTypes) filters.
// If types is empty but noTypes is specified, it returns all types except the excluded ones.
// If both are specified, it returns the intersection (types minus noTypes).
func parseSearchTypes(types []string, noTypes []string) ([]search.ResourceKind, error) {
	// Validate and parse excluded types first
	excludedTypes := make(map[search.ResourceKind]struct{})
	for _, t := range noTypes {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		if !search.IsValidResourceKind(t) {
			validKinds := make([]string, 0, len(search.AllResourceKinds()))
			for _, k := range search.AllResourceKinds() {
				validKinds = append(validKinds, string(k))
			}

			return nil, fmt.Errorf("invalid resource type in --no-type %q. Valid types: %s", t, strings.Join(validKinds, ", "))
		}

		excludedTypes[search.ResourceKind(t)] = struct{}{}
	}

	// If no inclusion types specified
	if len(types) == 0 {
		// If no exclusions either, return nil (all types)
		if len(excludedTypes) == 0 {
			return nil, nil
		}

		// Return all types except excluded ones
		allKinds := search.AllResourceKinds()
		result := make([]search.ResourceKind, 0, len(allKinds))
		for _, kind := range allKinds {
			if _, excluded := excludedTypes[kind]; !excluded {
				result = append(result, kind)
			}
		}

		return result, nil
	}

	// Parse and filter inclusion types
	result := make([]search.ResourceKind, 0, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		if !search.IsValidResourceKind(t) {
			validKinds := make([]string, 0, len(search.AllResourceKinds()))
			for _, k := range search.AllResourceKinds() {
				validKinds = append(validKinds, string(k))
			}

			return nil, fmt.Errorf("invalid resource type %q. Valid types: %s", t, strings.Join(validKinds, ", "))
		}

		kind := search.ResourceKind(t)
		// Only add if not excluded
		if _, excluded := excludedTypes[kind]; !excluded {
			result = append(result, kind)
		}
	}

	return result, nil
}

// completeSearchTypes provides shell completion for the --type flag.
func completeSearchTypes(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	allKinds := search.AllResourceKinds()
	completions := make([]string, 0, len(allKinds))

	toComplete = strings.ToLower(toComplete)
	for _, kind := range allKinds {
		kindStr := string(kind)
		if toComplete == "" || strings.Contains(strings.ToLower(kindStr), toComplete) {
			completions = append(completions, kindStr)
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	gcpSearchCmd.Flags().StringArrayVarP(&searchTypes, "type", "t", nil,
		"Filter results by resource type (can be specified multiple times)")
	_ = gcpSearchCmd.RegisterFlagCompletionFunc("type", completeSearchTypes)

	gcpSearchCmd.Flags().StringArrayVar(&searchNoTypes, "no-type", nil,
		"Exclude specific resource types from results (can be specified multiple times)")
	_ = gcpSearchCmd.RegisterFlagCompletionFunc("no-type", completeSearchTypes)

	gcpSearchCmd.Flags().IntVar(&searchParallelism, "parallelism", 8,
		"Number of projects to search in parallel (default 8)")

	gcpCmd.AddCommand(gcpSearchCmd)
}
