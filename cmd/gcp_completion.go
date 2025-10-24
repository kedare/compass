package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/gcp"
	"github.com/spf13/cobra"
)

// connectivityClient exposes the subset of the connectivity API required for shell completions.
type connectivityClient interface {
	ListTests(ctx context.Context, filter string) ([]*gcp.ConnectivityTestResult, error)
}

// computeClient exposes the subset of compute discovery calls used by completions.
type computeClient interface {
	ListInstances(ctx context.Context, zone string) ([]*gcp.Instance, error)
	ListManagedInstanceGroups(ctx context.Context, location string) ([]gcp.ManagedInstanceGroup, error)
	ListZones(ctx context.Context) ([]string, error)
	ListRegions(ctx context.Context) ([]string, error)
}

// completionCache provides the minimal cache operations needed for completions.
type completionCache interface {
	GetProjects() []string
	GetLocationsByProject(project string) ([]cache.CachedLocation, bool)
}

// completionCacheWrapper adapts the real cache to the completionCache interface.
type completionCacheWrapper struct {
	cache *cache.Cache
}

// GetProjects returns cached project identifiers.
func (w *completionCacheWrapper) GetProjects() []string {
	if w == nil || w.cache == nil {
		return nil
	}

	return w.cache.GetProjects()
}

// GetLocationsByProject returns cached locations for the given project.
func (w *completionCacheWrapper) GetLocationsByProject(project string) ([]cache.CachedLocation, bool) {
	if w == nil || w.cache == nil {
		return nil, false
	}

	return w.cache.GetLocationsByProject(project)
}

var (
	newConnectivityClientFunc = func(ctx context.Context, projectID string) (connectivityClient, error) {
		return gcp.NewConnectivityClient(ctx, projectID)
	}
	newComputeClientFunc = func(ctx context.Context, projectID string) (computeClient, error) {
		return gcp.NewClient(ctx, projectID)
	}
	loadCompletionCache = func() (completionCache, error) {
		c, err := gcp.LoadCache()
		if err != nil {
			return nil, err
		}

		return &completionCacheWrapper{cache: c}, nil
	}
)

// gcpConnectivityTestNameCompletion returns connectivity test names matching the provided prefix.
func gcpConnectivityTestNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Respect any project flag value provided in the current completion request.
	projectID, err := cmd.Flags().GetString("project")
	if err != nil {
		projectID = ""
	}

	if projectID == "" {
		projectID = project
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := newConnectivityClientFunc(ctx, projectID)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	tests, err := client.ListTests(ctx, "")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	completions := make([]string, 0, len(tests))

	for _, test := range tests {
		if toComplete != "" && !strings.HasPrefix(test.Name, toComplete) {
			continue
		}

		entry := test.Name
		if test.DisplayName != "" && test.DisplayName != test.Name {
			entry = fmt.Sprintf("%s\t%s", test.Name, test.DisplayName)
		}

		completions = append(completions, entry)
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// gcpProjectCompletion completes GCP project IDs using cached history and the active flag.
func gcpProjectCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cache, err := loadCompletionCache()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	prefix := strings.TrimSpace(toComplete)
	projects := cache.GetProjects()
	completions := make([]string, 0, len(projects))
	seen := make(map[string]struct{})

	for _, name := range projects {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}

		completions = append(completions, name)
		seen[name] = struct{}{}
	}

	current := readStringFlag(cmd, "project")
	if current == "" {
		current = project
	}

	if current != "" {
		current = strings.TrimSpace(current)
		if prefix == "" || strings.HasPrefix(current, prefix) {
			if _, exists := seen[current]; !exists {
				completions = append(completions, current)
			}
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// gcpSSHCompletion returns instance or managed instance group names for SSH command completion.
func gcpSSHCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	projectID := readStringFlag(cmd, "project")
	if projectID == "" {
		projectID = project
	}

	zoneFilter := readStringFlag(cmd, "zone")
	if zoneFilter == "" {
		zoneFilter = zone
	}

	noProjectOrZone := projectID == "" && zoneFilter == ""

	typeFilter := strings.ToLower(readStringFlag(cmd, "type"))
	if typeFilter == "" {
		typeFilter = strings.ToLower(resourceType)
	}

	includeInstances := typeFilter == "" || typeFilter == resourceTypeInstance
	includeMIGs := typeFilter == "" || typeFilter == resourceTypeMIG

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var client computeClient
	var err error

	if !noProjectOrZone {
		client, err = newComputeClientFunc(ctx, projectID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	toComplete = strings.TrimSpace(toComplete)
	completions := make([]string, 0)
	seen := make(map[string]struct{})

	if noProjectOrZone {
		cache, err := loadCompletionCache()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		cachedProjects := cache.GetProjects()
		for _, projectName := range cachedProjects {
			entries, ok := cache.GetLocationsByProject(projectName)
			if !ok {
				continue
			}

			for _, entry := range entries {
				if toComplete != "" && !strings.HasPrefix(entry.Name, toComplete) {
					continue
				}

				if _, exists := seen[entry.Name]; exists {
					continue
				}

				desc := fmt.Sprintf("Cached %s (%s)", entry.Type, entry.Location)
				completions = append(completions, fmt.Sprintf("%s\t%s", entry.Name, desc))
				seen[entry.Name] = struct{}{}
			}
		}

		return completions, cobra.ShellCompDirectiveNoFileComp
	}

	if includeInstances && client != nil {
		if instances, err := client.ListInstances(ctx, zoneFilter); err == nil {
			for _, inst := range instances {
				if inst == nil {
					continue
				}

				if toComplete != "" && !strings.HasPrefix(inst.Name, toComplete) {
					continue
				}

				if _, exists := seen[inst.Name]; exists {
					continue
				}

				desc := "Compute instance"
				if inst.Zone != "" {
					desc = fmt.Sprintf("Compute instance (%s)", inst.Zone)
				}

				completions = append(completions, fmt.Sprintf("%s\t%s", inst.Name, desc))
				seen[inst.Name] = struct{}{}
			}
		}
	}

	if includeMIGs && client != nil {
		var groups []gcp.ManagedInstanceGroup

		for _, location := range migCompletionLocations(zoneFilter) {
			migs, err := client.ListManagedInstanceGroups(ctx, location)
			if err != nil {
				continue
			}

			groups = append(groups, migs...)
		}

		for _, mig := range groups {
			if toComplete != "" && !strings.HasPrefix(mig.Name, toComplete) {
				continue
			}

			if _, exists := seen[mig.Name]; exists {
				continue
			}

			scope := "zone"
			if mig.IsRegional {
				scope = "region"
			}

			completions = append(completions, fmt.Sprintf("%s\tManaged instance group (%s %s)", mig.Name, scope, mig.Location))
			seen[mig.Name] = struct{}{}
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// gcpSSHZoneCompletion returns zone and region suggestions for the SSH command.
func gcpSSHZoneCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	projectID := readStringFlag(cmd, "project")
	if projectID == "" {
		projectID = project
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := newComputeClientFunc(ctx, projectID)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	toComplete = strings.TrimSpace(toComplete)
	completions := make([]string, 0)
	seen := make(map[string]struct{})

	regionSet := make(map[string]struct{})

	if zones, err := client.ListZones(ctx); err == nil {
		for _, name := range zones {
			if toComplete != "" && !strings.HasPrefix(name, toComplete) {
				continue
			}

			if _, exists := seen[name]; exists {
				continue
			}

			completions = append(completions, fmt.Sprintf("%s\tCompute zone", name))
			seen[name] = struct{}{}

			if region := regionFromZone(name); region != "" {
				regionSet[region] = struct{}{}
			}
		}
	}

	if len(regionSet) > 0 {
		regionNames := make([]string, 0, len(regionSet))
		for name := range regionSet {
			regionNames = append(regionNames, name)
		}

		sort.Strings(regionNames)

		for _, name := range regionNames {
			if toComplete != "" && !strings.HasPrefix(name, toComplete) {
				continue
			}

			if _, exists := seen[name]; exists {
				continue
			}

			completions = append(completions, fmt.Sprintf("%s\tCompute region", name))
			seen[name] = struct{}{}
		}
	} else if regions, err := client.ListRegions(ctx); err == nil {
		for _, name := range regions {
			if toComplete != "" && !strings.HasPrefix(name, toComplete) {
				continue
			}

			if _, exists := seen[name]; exists {
				continue
			}

			completions = append(completions, fmt.Sprintf("%s\tCompute region", name))
			seen[name] = struct{}{}
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// readStringFlag returns the string value for the given flag, scanning inherited flags as needed.
func readStringFlag(cmd *cobra.Command, name string) string {
	if cmd == nil {
		return ""
	}

	if value, err := cmd.Flags().GetString(name); err == nil && value != "" {
		return value
	}

	if value, err := cmd.InheritedFlags().GetString(name); err == nil && value != "" {
		return value
	}

	return ""
}

// migCompletionLocations returns candidate locations for managed instance group searches.
func migCompletionLocations(zone string) []string {
	if zone == "" {
		return []string{""}
	}

	locations := []string{zone}

	if zoneLooksLikeZone(zone) {
		if region := regionFromZone(zone); region != "" {
			locations = append(locations, region)
		}
	}

	return deduplicateStrings(locations)
}

// zoneLooksLikeZone reports whether the provided value resembles a zone identifier.
func zoneLooksLikeZone(value string) bool {
	parts := strings.Split(value, "-")

	return len(parts) >= 3 && len(parts[len(parts)-1]) == 1
}

// regionFromZone converts a zone identifier into its parent region where applicable.
func regionFromZone(zone string) string {
	parts := strings.Split(zone, "-")
	if len(parts) < 3 {
		return ""
	}

	return strings.Join(parts[:len(parts)-1], "-")
}

// deduplicateStrings removes duplicate strings while preserving their first occurrence order.
func deduplicateStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}
