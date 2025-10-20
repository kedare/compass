// Package gcp provides Google Cloud Platform integration for instance discovery and connection
package gcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

const (
	InstanceStatusRunning = "RUNNING"
)

var (
	ErrProjectRequired          = errors.New("project must be specified via --project flag or gcloud default project")
	ErrInstanceNotFound         = errors.New("instance not found in any zone")
	ErrMIGNotFound              = errors.New("MIG not found in any region or zone")
	ErrNoInstancesInMIG         = errors.New("no instances found in MIG")
	ErrNoInstancesInRegionalMIG = errors.New("no instances found in regional MIG")
	ErrCacheNotAvailable        = errors.New("cache not available")
	ErrNoCacheEntry             = errors.New("no cache entry found")
	ErrProjectMismatch          = errors.New("project mismatch")
	ErrMIGNotInAggregatedList   = errors.New("MIG not found in aggregated list")
	ErrUnknownScopeType         = errors.New("unknown scope type")
)

type Client struct {
	service *compute.Service
	cache   *cache.Cache
	project string
}

type Instance struct {
	Name        string
	Zone        string
	ExternalIP  string
	InternalIP  string
	Status      string
	MachineType string
	CanUseIAP   bool
}

type ManagedInstanceRef struct {
	Name   string
	Zone   string
	Status string
}

func (r ManagedInstanceRef) IsRunning() bool {
	return r.Status == InstanceStatusRunning
}

func NewClient(ctx context.Context, project string) (*Client, error) {
	logger.Log.Debug("Creating new GCP client")

	if project == "" {
		logger.Log.Debug("No project specified, attempting to get default project")
		// Try to get default project from gcloud config
		project = getDefaultProject()
		if project == "" {
			logger.Log.Error("No project specified and no default project found")

			return nil, ErrProjectRequired
		}

		logger.Log.Debugf("Using default project: %s", project)
	} else {
		logger.Log.Debugf("Using specified project: %s", project)
	}

	logger.Log.Debug("Creating compute service")

	httpClient, err := newHTTPClientWithLogging(ctx, compute.ComputeScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client: %v", err)

		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	service, err := compute.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create compute service: %v", err)

		return nil, fmt.Errorf("failed to create compute service: %w", err)
	}

	logger.Log.Debug("GCP client created successfully")

	// Initialize cache
	c, err := cache.New()
	if err != nil {
		logger.Log.Warnf("Failed to initialize cache: %v", err)
		// Continue without cache
		c = nil
	}

	return &Client{
		service: service,
		project: project,
		cache:   c,
	}, nil
}

func (c *Client) FindInstance(ctx context.Context, instanceName, zone string) (*Instance, error) {
	logger.Log.Debugf("Looking for instance: %s", instanceName)

	if zone != "" {
		logger.Log.Debugf("Searching in specific zone: %s (cache bypass)", zone)
		// Zone specified - bypass cache and search directly
		instance, err := c.findInstanceInZone(ctx, instanceName, zone)
		if err == nil && c.cache != nil {
			// Cache the result
			c.cacheInstance(instanceName, instance)
		}

		return instance, err
	}

	// Try cache first if available
	if c.cache != nil {
		if cachedInfo, found := c.cache.Get(instanceName); found && cachedInfo.Type == cache.ResourceTypeInstance {
			logger.Log.Debugf("Found instance location in cache: project=%s, zone=%s", cachedInfo.Project, cachedInfo.Zone)

			// Verify cached project matches current project
			if cachedInfo.Project == c.project {
				instance, err := c.findInstanceInZone(ctx, instanceName, cachedInfo.Zone)
				if err == nil {
					logger.Log.Debug("Successfully retrieved instance using cached location")

					return instance, nil
				}

				logger.Log.Debugf("Cached location invalid, performing full search: %v", err)
			} else {
				logger.Log.Debugf("Cached project %s doesn't match current project %s, performing full search",
					cachedInfo.Project, c.project)
			}
		}
	}

	logger.Log.Debug("Zone not specified, using aggregated list for auto-discovery")

	instanceData, err := c.findInstanceAcrossAggregatedPages(ctx, instanceName)
	if err != nil {
		if errors.Is(err, ErrInstanceNotFound) {
			logger.Log.Warnf("Instance '%s' not found in any zone", instanceName)

			return nil, fmt.Errorf("instance '%s': %w", instanceName, ErrInstanceNotFound)
		}

		return nil, err
	}

	result := c.convertInstance(instanceData)
	logger.Log.Debugf("Found instance %s in zone %s", instanceName, result.Zone)

	if c.cache != nil {
		c.cacheInstance(instanceName, result)
	}

	return result, nil
}

func (c *Client) findInstanceAcrossAggregatedPages(ctx context.Context, instanceName string) (*compute.Instance, error) {
	fetch := func(pageToken string) (*compute.InstanceAggregatedList, error) {
		call := c.service.Instances.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to perform aggregated list: %v", err)

			return nil, fmt.Errorf("failed to list instances: %w", err)
		}

		logger.Log.Debugf("Scanning aggregated instance page (next token: %s)", list.NextPageToken)

		return list, nil
	}

	return findInstanceInAggregatedPages(instanceName, fetch)
}

func (c *Client) findInstanceInZone(ctx context.Context, instanceName, zone string) (*Instance, error) {
	logger.Log.Tracef("Searching for instance %s in zone %s", instanceName, zone)

	instance, err := c.service.Instances.Get(c.project, zone, instanceName).Context(ctx).Do()
	if err != nil {
		logger.Log.Tracef("Instance %s not found in zone %s: %v", instanceName, zone, err)

		return nil, err
	}

	logger.Log.Debugf("Found instance %s in zone %s (status: %s)", instanceName, zone, instance.Status)

	return c.convertInstance(instance), nil
}

func (c *Client) FindInstanceInMIG(ctx context.Context, migName, zone string) (*Instance, error) {
	logger.Log.Debugf("Looking for MIG: %s", migName)

	refs, isRegional, err := c.ListMIGInstances(ctx, migName, zone)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, fmt.Errorf("MIG '%s': %w", migName, ErrNoInstancesInMIG)
	}

	selectedRef := selectPreferredManagedInstance(refs)

	instance, err := c.findInstanceInZone(ctx, selectedRef.Name, selectedRef.Zone)
	if err != nil {
		return nil, err
	}

	if c.cache != nil {
		c.cacheMIG(migName, instance, isRegional)
	}

	return instance, nil
}

// ListMIGInstances returns the instances managed by the specified MIG along with whether the MIG is regional.
func (c *Client) ListMIGInstances(ctx context.Context, migName, location string) ([]ManagedInstanceRef, bool, error) {
	logger.Log.Debugf("Listing instances for MIG: %s", migName)

	if location != "" {
		refs, isRegional, err := c.listMIGInstancesWithSpecifiedLocation(ctx, migName, location)
		if err != nil {
			return nil, isRegional, err
		}

		c.cacheMIGFromRefs(migName, refs, isRegional)

		return refs, isRegional, nil
	}

	if refs, isRegional, err := c.listMIGInstancesFromCache(ctx, migName); err == nil {
		c.cacheMIGFromRefs(migName, refs, isRegional)

		return refs, isRegional, nil
	} else if err != ErrCacheNotAvailable && err != ErrNoCacheEntry && err != ErrProjectMismatch {
		return nil, false, err
	}

	if refs, isRegional, err := c.listMIGInstancesFromAggregated(ctx, migName); err == nil {
		c.cacheMIGFromRefs(migName, refs, isRegional)

		return refs, isRegional, nil
	} else if !errors.Is(err, ErrMIGNotInAggregatedList) {
		return nil, false, err
	}

	refs, isRegional, err := c.listMIGInstancesByRegionScan(ctx, migName)
	if err != nil {
		return nil, false, err
	}

	c.cacheMIGFromRefs(migName, refs, isRegional)

	return refs, isRegional, nil
}

func (c *Client) listMIGInstancesWithSpecifiedLocation(ctx context.Context, migName, location string) ([]ManagedInstanceRef, bool, error) {
	if c.isRegion(location) {
		refs, err := c.listManagedInstancesInRegionalMIG(ctx, migName, location)

		return refs, true, err
	}

	refs, err := c.listManagedInstancesInMIGZone(ctx, migName, location)

	return refs, false, err
}

func (c *Client) listMIGInstancesFromCache(ctx context.Context, migName string) ([]ManagedInstanceRef, bool, error) {
	if c.cache == nil {
		return nil, false, ErrCacheNotAvailable
	}

	cachedInfo, found := c.cache.Get(migName)
	if !found || cachedInfo.Type != cache.ResourceTypeMIG {
		return nil, false, ErrNoCacheEntry
	}

	logger.Log.Debugf("Found MIG location in cache: project=%s, zone=%s, region=%s, isRegional=%v",
		cachedInfo.Project, cachedInfo.Zone, cachedInfo.Region, cachedInfo.IsRegional)

	if cachedInfo.Project != c.project {
		logger.Log.Debugf("Cached project %s doesn't match current project %s, performing full search",
			cachedInfo.Project, c.project)

		return nil, false, ErrProjectMismatch
	}

	if cachedInfo.IsRegional {
		refs, err := c.listManagedInstancesInRegionalMIG(ctx, migName, cachedInfo.Region)

		return refs, true, err
	}

	refs, err := c.listManagedInstancesInMIGZone(ctx, migName, cachedInfo.Zone)

	return refs, false, err
}

func (c *Client) listMIGInstancesFromAggregated(ctx context.Context, migName string) ([]ManagedInstanceRef, bool, error) {
	scopeName, err := c.findMIGScopeAcrossAggregatedPages(ctx, migName)
	if err != nil {
		return nil, false, err
	}

	return c.listMIGInstancesInScope(ctx, migName, scopeName)
}

func (c *Client) listMIGInstancesByRegionScan(ctx context.Context, migName string) ([]ManagedInstanceRef, bool, error) {
	logger.Log.Debug("MIG not found in aggregated list, searching regions individually")

	regions, err := c.listRegions(ctx)
	if err != nil {
		logger.Log.Errorf("Failed to list regions for regional MIG search: %v", err)

		return nil, false, fmt.Errorf("failed to list regions: %w", err)
	}

	logger.Log.Debugf("Searching for regional MIG through %d regions", len(regions))

	for _, region := range regions {
		logger.Log.Tracef("Checking for regional MIG in region: %s", region)

		refs, err := c.listManagedInstancesInRegionalMIG(ctx, migName, region)
		if err == nil {
			return refs, true, nil
		}
	}

	logger.Log.Warnf("MIG '%s' not found in any region or zone", migName)

	return nil, false, fmt.Errorf("MIG '%s': %w", migName, ErrMIGNotFound)
}

func (c *Client) listMIGInstancesInScope(ctx context.Context, migName, scopeName string) ([]ManagedInstanceRef, bool, error) {
	if strings.HasPrefix(scopeName, "zones/") {
		zone := extractZoneFromScope(scopeName)
		logger.Log.Debugf("Found zonal MIG %s in zone %s", migName, zone)

		refs, err := c.listManagedInstancesInMIGZone(ctx, migName, zone)

		return refs, false, err
	} else if strings.HasPrefix(scopeName, "regions/") {
		region := extractRegionFromScope(scopeName)
		logger.Log.Debugf("Found regional MIG %s in region %s", migName, region)

		refs, err := c.listManagedInstancesInRegionalMIG(ctx, migName, region)

		return refs, true, err
	}

	return nil, false, fmt.Errorf("%w: %s", ErrUnknownScopeType, scopeName)
}

func (c *Client) cacheMIGFromRefs(migName string, refs []ManagedInstanceRef, isRegional bool) {
	if c.cache == nil || len(refs) == 0 {
		return
	}

	c.cacheMIG(migName, &Instance{
		Name: refs[0].Name,
		Zone: refs[0].Zone,
	}, isRegional)
}

func selectPreferredManagedInstance(refs []ManagedInstanceRef) ManagedInstanceRef {
	for _, ref := range refs {
		if ref.IsRunning() {
			return ref
		}
	}

	return refs[0]
}

func (c *Client) findMIGScopeAcrossAggregatedPages(ctx context.Context, migName string) (string, error) {
	fetch := func(pageToken string) (*compute.InstanceGroupManagerAggregatedList, error) {
		call := c.service.InstanceGroupManagers.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to perform MIG aggregated list: %v", err)

			return nil, err
		}

		logger.Log.Debugf("Scanning aggregated MIG page (next token: %s)", list.NextPageToken)

		return list, nil
	}

	scope, err := findMIGScopeAcrossPages(migName, fetch)
	if err != nil {
		return "", err
	}

	return scope, nil
}

func (c *Client) listManagedInstancesInMIGZone(ctx context.Context, migName, zone string) ([]ManagedInstanceRef, error) {
	logger.Log.Tracef("Searching for MIG %s in zone %s", migName, zone)

	// Check if the managed instance group exists
	_, err := c.service.InstanceGroupManagers.Get(c.project, zone, migName).Context(ctx).Do()
	if err != nil {
		logger.Log.Tracef("MIG %s not found in zone %s: %v", migName, zone, err)

		return nil, err
	}

	logger.Log.Debugf("Found MIG %s in zone %s, listing instances", migName, zone)

	managedInstances, err := collectManagedInstances(func(pageToken string) ([]*compute.ManagedInstance, string, error) {
		call := c.service.InstanceGroupManagers.ListManagedInstances(c.project, zone, migName).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list instances in MIG %s: %v", migName, err)

			return nil, "", fmt.Errorf("failed to list MIG instances: %w", err)
		}

		return resp.ManagedInstances, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	if len(managedInstances) == 0 {
		logger.Log.Warnf("No instances found in MIG '%s'", migName)

		return nil, fmt.Errorf("MIG '%s': %w", migName, ErrNoInstancesInMIG)
	}

	logger.Log.Debugf("Found %d instances in MIG %s", len(managedInstances), migName)

	refs := make([]ManagedInstanceRef, 0, len(managedInstances))
	for _, managedInstance := range managedInstances {
		logger.Log.Tracef("Found instance %s (status: %s)", managedInstance.Instance, managedInstance.InstanceStatus)

		refs = append(refs, ManagedInstanceRef{
			Name:   extractInstanceName(managedInstance.Instance),
			Zone:   zone,
			Status: managedInstance.InstanceStatus,
		})
	}

	return refs, nil
}

func (c *Client) listRegions(ctx context.Context) ([]string, error) {
	logger.Log.Debug("Listing available regions")

	regions, err := c.service.Regions.List(c.project).Context(ctx).Do()
	if err != nil {
		logger.Log.Errorf("Failed to list regions: %v", err)

		return nil, err
	}

	var regionNames []string

	for _, region := range regions.Items {
		if region.Status == "UP" {
			regionNames = append(regionNames, region.Name)
			logger.Log.Tracef("Available region: %s", region.Name)
		}
	}

	logger.Log.Debugf("Found %d available regions", len(regionNames))

	return regionNames, nil
}

func (c *Client) isRegion(location string) bool {
	// Simple heuristic: regions typically have format like "us-central1", "europe-west2"
	// zones have format like "us-central1-a", "europe-west2-b"
	parts := strings.Split(location, "-")

	return len(parts) == 2 || (len(parts) == 3 && len(parts[2]) > 1)
}

func (c *Client) listManagedInstancesInRegionalMIG(ctx context.Context, migName, region string) ([]ManagedInstanceRef, error) {
	logger.Log.Tracef("Searching for regional MIG %s in region %s", migName, region)

	// Check if the regional managed instance group exists
	_, err := c.service.RegionInstanceGroupManagers.Get(c.project, region, migName).Context(ctx).Do()
	if err != nil {
		logger.Log.Tracef("Regional MIG %s not found in region %s: %v", migName, region, err)

		return nil, err
	}

	logger.Log.Debugf("Found regional MIG %s in region %s, listing instances", migName, region)

	managedInstances, err := collectManagedInstances(func(pageToken string) ([]*compute.ManagedInstance, string, error) {
		call := c.service.RegionInstanceGroupManagers.ListManagedInstances(c.project, region, migName).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list instances in regional MIG %s: %v", migName, err)

			return nil, "", fmt.Errorf("failed to list regional MIG instances: %w", err)
		}

		return resp.ManagedInstances, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	if len(managedInstances) == 0 {
		logger.Log.Warnf("No instances found in regional MIG '%s'", migName)

		return nil, fmt.Errorf("regional MIG '%s': %w", migName, ErrNoInstancesInRegionalMIG)
	}

	logger.Log.Debugf("Found %d instances in regional MIG %s", len(managedInstances), migName)

	refs := make([]ManagedInstanceRef, 0, len(managedInstances))
	for _, managedInstance := range managedInstances {
		instanceZone := extractZoneFromInstanceURL(managedInstance.Instance)
		logger.Log.Tracef("Found instance %s (status: %s) in zone %s", managedInstance.Instance, managedInstance.InstanceStatus, instanceZone)

		refs = append(refs, ManagedInstanceRef{
			Name:   extractInstanceName(managedInstance.Instance),
			Zone:   instanceZone,
			Status: managedInstance.InstanceStatus,
		})
	}

	return refs, nil
}

func (c *Client) convertInstance(instance *compute.Instance) *Instance {
	result := &Instance{
		Name:        instance.Name,
		Zone:        extractZoneName(instance.Zone),
		Status:      instance.Status,
		MachineType: extractMachineType(instance.MachineType),
	}

	// Extract IP addresses
	hasExternalIP := false

	for _, networkInterface := range instance.NetworkInterfaces {
		if networkInterface.NetworkIP != "" {
			result.InternalIP = networkInterface.NetworkIP
		}

		for _, accessConfig := range networkInterface.AccessConfigs {
			if accessConfig.NatIP != "" {
				result.ExternalIP = accessConfig.NatIP
				hasExternalIP = true
			}
		}
	}

	// Prefer IAP only when no external IP is available.
	result.CanUseIAP = !hasExternalIP

	return result
}

func extractInstanceName(instanceURL string) string {
	parts := strings.Split(instanceURL, "/")

	return parts[len(parts)-1]
}

func extractZoneName(zoneURL string) string {
	parts := strings.Split(zoneURL, "/")

	return parts[len(parts)-1]
}

func extractZoneFromInstanceURL(instanceURL string) string {
	// Instance URL format: https://www.googleapis.com/compute/v1/projects/PROJECT/zones/ZONE/instances/INSTANCE
	parts := strings.Split(instanceURL, "/")
	for i, part := range parts {
		if part == "zones" && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	return ""
}

func extractZoneFromScope(scope string) string {
	// Scope format: "zones/us-central1-a" or full URL
	if strings.Contains(scope, "/") {
		parts := strings.Split(scope, "/")

		return parts[len(parts)-1]
	}

	return scope
}

func extractRegionFromScope(scope string) string {
	// Scope format: "regions/us-central1" or full URL
	if strings.Contains(scope, "/") {
		parts := strings.Split(scope, "/")

		return parts[len(parts)-1]
	}

	return scope
}

func extractMachineType(machineTypeURL string) string {
	parts := strings.Split(machineTypeURL, "/")

	return parts[len(parts)-1]
}

func getDefaultProject() string {
	envVars := []string{
		"CLOUDSDK_CORE_PROJECT",
		"GOOGLE_CLOUD_PROJECT",
		"GCLOUD_PROJECT",
		"GCP_PROJECT",
	}

	for _, key := range envVars {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			logger.Log.Debugf("Using default project from %s", key)

			return value
		}
	}

	project, err := readProjectFromGCloudConfig()
	if err == nil && project != "" {
		logger.Log.Debug("Using default project from gcloud configuration")

		return project
	}

	return ""
}

func readProjectFromGCloudConfig() (string, error) {
	configDir := os.Getenv("CLOUDSDK_CONFIG")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}

		configDir = filepath.Join(home, ".config", "gcloud")
	}

	configPath := filepath.Join(configDir, "configurations", "config_default")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	inCore := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.Trim(line, "[]")
			inCore = strings.EqualFold(section, "core")

			continue
		}

		if inCore && strings.HasPrefix(line, "project") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", errors.New("project not found in gcloud config")
}

// cacheInstance stores instance location information in the cache.
func (c *Client) cacheInstance(instanceName string, instance *Instance) {
	info := &cache.LocationInfo{
		Project: c.project,
		Zone:    instance.Zone,
		Type:    cache.ResourceTypeInstance,
	}

	if err := c.cache.Set(instanceName, info); err != nil {
		logger.Log.Warnf("Failed to cache instance location: %v", err)
	}
}

// cacheMIG stores MIG location information in the cache.
func (c *Client) cacheMIG(migName string, instance *Instance, isRegional bool) {
	info := &cache.LocationInfo{
		Project:    c.project,
		Type:       cache.ResourceTypeMIG,
		IsRegional: isRegional,
	}

	if isRegional {
		info.Region = extractRegionFromZone(instance.Zone)
	} else {
		info.Zone = instance.Zone
	}

	if err := c.cache.Set(migName, info); err != nil {
		logger.Log.Warnf("Failed to cache MIG location: %v", err)
	}
}

// extractRegionFromZone extracts the region from a zone name (e.g., "us-central1-a" -> "us-central1").
func extractRegionFromZone(zone string) string {
	parts := strings.Split(zone, "-")
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-1], "-")
	}

	return zone
}

func findInstanceInAggregatedPages(instanceName string, fetch func(pageToken string) (*compute.InstanceAggregatedList, error)) (*compute.Instance, error) {
	pageToken := ""

	for {
		list, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		for _, scopedList := range list.Items {
			for _, instance := range scopedList.Instances {
				if instance.Name == instanceName {
					return instance, nil
				}
			}
		}

		if list.NextPageToken == "" {
			break
		}

		pageToken = list.NextPageToken
	}

	return nil, ErrInstanceNotFound
}

func findMIGScopeAcrossPages(migName string, fetch func(pageToken string) (*compute.InstanceGroupManagerAggregatedList, error)) (string, error) {
	pageToken := ""

	for {
		list, err := fetch(pageToken)
		if err != nil {
			return "", err
		}

		for scopeName, scopedList := range list.Items {
			for _, mig := range scopedList.InstanceGroupManagers {
				if mig.Name == migName {
					return scopeName, nil
				}
			}
		}

		if list.NextPageToken == "" {
			break
		}

		pageToken = list.NextPageToken
	}

	return "", ErrMIGNotInAggregatedList
}

func collectManagedInstances(fetch func(pageToken string) ([]*compute.ManagedInstance, string, error)) ([]*compute.ManagedInstance, error) {
	pageToken := ""
	var all []*compute.ManagedInstance

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

// LoadCache loads and returns the cache instance.
func LoadCache() (*cache.Cache, error) {
	return cache.New()
}
