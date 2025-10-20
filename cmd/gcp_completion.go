package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kedare/compass/internal/gcp"
	"github.com/spf13/cobra"
)

func gcpProjectCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cache, err := gcp.LoadCache()
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

	var client *gcp.Client
	var err error

	if !noProjectOrZone {
		client, err = gcp.NewClient(ctx, projectID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	toComplete = strings.TrimSpace(toComplete)
	completions := make([]string, 0)
	seen := make(map[string]struct{})

	if noProjectOrZone {
		cache, err := gcp.LoadCache()
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

	if includeInstances {
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

	if includeMIGs {
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

func gcpSSHZoneCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	projectID := readStringFlag(cmd, "project")
	if projectID == "" {
		projectID = project
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := gcp.NewClient(ctx, projectID)
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

func zoneLooksLikeZone(value string) bool {
	parts := strings.Split(value, "-")

	return len(parts) >= 3 && len(parts[len(parts)-1]) == 1
}

func regionFromZone(zone string) string {
	parts := strings.Split(zone, "-")
	if len(parts) < 3 {
		return ""
	}

	return strings.Join(parts[:len(parts)-1], "-")
}

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
