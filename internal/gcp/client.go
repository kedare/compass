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
	"sort"
	"strings"
	"sync"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

const (
	InstanceStatusRunning    = "RUNNING"
	DefaultLookupConcurrency = 10
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
	Project     string
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

// ProgressEvent describes a progress update emitted during resource discovery.
type ProgressEvent struct {
	// Key identifies the logical task (e.g. "region:us-central1").
	Key string
	// Message contains the human-readable update to display.
	Message string
	// Err is set when the task finishes with an error.
	Err error
	// Done reports whether the task has completed (successfully or with error).
	Done bool
	// Info marks the completion as informational (neither success nor failure).
	Info bool
}

// ProgressCallback receives progress updates for long running discovery tasks.
type ProgressCallback func(ProgressEvent)

// FormatProgressKey normalises labels for spinners to keep output concise.
func FormatProgressKey(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return label
	}

	if idx := strings.IndexRune(label, ' '); idx > 0 {
		return label[:idx]
	}

	return label
}

func (r ManagedInstanceRef) IsRunning() bool {
	return r.Status == InstanceStatusRunning
}

// ManagedInstanceGroup represents a managed instance group scope.
type ManagedInstanceGroup struct {
	Name       string
	Location   string
	IsRegional bool
}

var (
	cacheOnce      sync.Once
	sharedCache    *cache.Cache
	cacheInitError error
)

func progressUpdate(cb ProgressCallback, key, message string) {
	if cb == nil {
		return
	}

	cb(ProgressEvent{Key: key, Message: message})
}

func progressSuccess(cb ProgressCallback, key, message string) {
	if cb == nil {
		return
	}

	cb(ProgressEvent{Key: key, Message: message, Done: true})
}

func progressFailure(cb ProgressCallback, key, message string, err error) {
	if cb == nil {
		return
	}

	cb(ProgressEvent{Key: key, Message: message, Err: err, Done: true})
}

// getSharedCache returns the process-wide cache instance, creating it once on demand.
func getSharedCache() (*cache.Cache, error) {
	if !cache.Enabled() {
		return nil, nil
	}

	cacheOnce.Do(func() {
		if !cache.Enabled() {
			sharedCache = nil
			cacheInitError = nil

			return
		}

		sharedCache, cacheInitError = cache.New()
	})

	if !cache.Enabled() {
		return nil, nil
	}

	return sharedCache, cacheInitError
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

	client := &Client{
		service: service,
		project: project,
	}

	c, err := getSharedCache()
	if err != nil {
		logger.Log.Warnf("Failed to initialize cache: %v", err)
	} else {
		client.cache = c
	}

	return client, nil
}

// ProjectID returns the project associated with the client.
func (c *Client) ProjectID() string {
	if c == nil {
		return ""
	}

	return c.project
}

// ListAllProjects lists all GCP projects the user has access to using Cloud Resource Manager API
func ListAllProjects(ctx context.Context) ([]string, error) {
	logger.Log.Debug("Listing all accessible GCP projects")

	httpClient, err := newHTTPClientWithLogging(ctx, cloudresourcemanager.CloudPlatformReadOnlyScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client for Resource Manager: %v", err)
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	service, err := cloudresourcemanager.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create Resource Manager service: %v", err)
		return nil, fmt.Errorf("failed to create resource manager service: %w", err)
	}

	var projects []string
	pageToken := ""

	for {
		call := service.Projects.List()
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Context(ctx).Do()
		if err != nil {
			logger.Log.Errorf("Failed to list projects: %v", err)
			return nil, fmt.Errorf("failed to list projects: %w", err)
		}

		for _, project := range resp.Projects {
			if project.LifecycleState == "ACTIVE" && project.ProjectId != "" {
				projects = append(projects, project.ProjectId)
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	logger.Log.Debugf("Found %d active projects", len(projects))

	// Sort projects by name for better UX
	sort.Strings(projects)

	return projects, nil
}

// RememberProject persists the client's project in the local cache when available.
func (c *Client) RememberProject() {
	if c == nil {
		return
	}

	if c.cache == nil || !cache.Enabled() {
		return
	}

	if err := c.cache.AddProject(c.project); err != nil {
		logger.Log.Warnf("Failed to remember project %s: %v", c.project, err)
	}
}

func (c *Client) FindInstance(ctx context.Context, instanceName, zone string, progress ...ProgressCallback) (*Instance, error) {
	logger.Log.Debugf("Looking for instance: %s", instanceName)

	var cb ProgressCallback
	if len(progress) > 0 {
		cb = progress[0]
	}

	if zone != "" {
		logger.Log.Debugf("Searching in specific zone: %s (cache bypass)", zone)
		progressUpdate(cb, "", fmt.Sprintf("Checking zone %s for instance %s", zone, instanceName))
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
			//progressUpdate(cb, "", fmt.Sprintf("Checking cached zone %s for instance %s", cachedInfo.Zone, instanceName))

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
	progressUpdate(cb, FormatProgressKey("aggregate"), fmt.Sprintf("Scanning aggregated instance list for %s", instanceName))

	instanceData, err := c.findInstanceAcrossAggregatedPages(ctx, instanceName, cb)
	if err != nil {
		if errors.Is(err, ErrInstanceNotFound) {
			logger.Log.Debugf("Instance '%s' not found in any zone", instanceName)

			return nil, fmt.Errorf("instance '%s': %w", instanceName, ErrInstanceNotFound)
		}

		return nil, err
	}

	result := c.convertInstance(instanceData)
	logger.Log.Debugf("Found instance %s in zone %s", instanceName, result.Zone)
	progressSuccess(cb, FormatProgressKey("aggregate"), fmt.Sprintf("Located instance %s in zone %s", instanceName, result.Zone))

	if c.cache != nil {
		c.cacheInstance(instanceName, result)
	}

	return result, nil
}

// ListInstances returns the instances available in the current project, optionally filtered by zone.
func (c *Client) ListInstances(ctx context.Context, zone string) ([]*Instance, error) {
	logger.Log.Debugf("Listing instances (zone filter: %s)", zone)

	if zone != "" {
		items, err := collectInstances(func(pageToken string) ([]*compute.Instance, string, error) {
			call := c.service.Instances.List(c.project, zone).Context(ctx)
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}

			resp, err := call.Do()
			if err != nil {
				logger.Log.Errorf("Failed to list instances in zone %s: %v", zone, err)

				return nil, "", fmt.Errorf("failed to list instances in zone %s: %w", zone, err)
			}

			return resp.Items, resp.NextPageToken, nil
		})
		if err != nil {
			return nil, err
		}

		results := make([]*Instance, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			results = append(results, c.convertInstance(item))
		}

		return results, nil
	}

	pageToken := ""
	var results []*Instance

	for {
		call := c.service.Instances.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list aggregated instances: %v", err)

			return nil, fmt.Errorf("failed to list instances: %w", err)
		}

		for scope, scopedList := range list.Items {
			if len(scopedList.Instances) == 0 {
				continue
			}

			zoneName := extractZoneFromScope(scope)

			for _, inst := range scopedList.Instances {
				if inst == nil {
					continue
				}

				converted := c.convertInstance(inst)
				if converted.Zone == "" {
					converted.Zone = zoneName
				}

				results = append(results, converted)
			}
		}

		if list.NextPageToken == "" {
			break
		}

		pageToken = list.NextPageToken
	}

	return results, nil
}

func (c *Client) findInstanceAcrossAggregatedPages(ctx context.Context, instanceName string, progress ProgressCallback) (*compute.Instance, error) {
	page := 0
	fetch := func(pageToken string) (*compute.InstanceAggregatedList, error) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		call := c.service.Instances.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			// Don't log as error if context was cancelled
			if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
				logger.Log.Errorf("Failed to perform aggregated list: %v", err)
			}

			return nil, fmt.Errorf("failed to list instances: %w", err)
		}

		page++
		progressUpdate(progress, FormatProgressKey("aggregate"), fmt.Sprintf("Scanning instance aggregate page %d (next token: %s)", page, list.NextPageToken))

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
func (c *Client) ListMIGInstances(ctx context.Context, migName, location string, progress ...ProgressCallback) ([]ManagedInstanceRef, bool, error) {
	var cb ProgressCallback
	if len(progress) > 0 {
		cb = progress[0]
	}

	return c.listMIGInstances(ctx, migName, location, cb)
}

func (c *Client) listMIGInstances(ctx context.Context, migName, location string, progress ProgressCallback) ([]ManagedInstanceRef, bool, error) {
	logger.Log.Debugf("Listing instances for MIG: %s", migName)

	if location != "" {
		scopeKey := FormatProgressKey(fmt.Sprintf("scope:%s", strings.TrimSpace(location)))
		progressUpdate(progress, scopeKey, fmt.Sprintf("Locating MIG %s in %s", migName, strings.TrimSpace(location)))

		refs, isRegional, err := c.listMIGInstancesWithSpecifiedLocation(ctx, migName, location)
		if err != nil {
			progressFailure(progress, scopeKey, fmt.Sprintf("Failed to locate MIG %s in %s", migName, strings.TrimSpace(location)), err)

			return nil, isRegional, err
		}

		progressSuccess(progress, scopeKey, fmt.Sprintf("Found MIG %s in %s", migName, strings.TrimSpace(location)))

		c.cacheMIGFromRefs(migName, refs, isRegional)

		return refs, isRegional, nil
	}

	if refs, isRegional, err := c.listMIGInstancesFromCache(ctx, migName); err == nil {
		progressUpdate(progress, "", fmt.Sprintf("Using cached location for MIG %s", migName))
		c.cacheMIGFromRefs(migName, refs, isRegional)

		return refs, isRegional, nil
	} else if err != ErrCacheNotAvailable && err != ErrNoCacheEntry && err != ErrProjectMismatch {
		return nil, false, err
	}

	if refs, isRegional, err := c.listMIGInstancesFromAggregated(ctx, migName, progress); err == nil {
		c.cacheMIGFromRefs(migName, refs, isRegional)

		return refs, isRegional, nil
	} else if !errors.Is(err, ErrMIGNotInAggregatedList) {
		return nil, false, err
	}

	refs, isRegional, err := c.listMIGInstancesByRegionScan(ctx, migName, progress)
	if err != nil {
		return nil, false, err
	}

	c.cacheMIGFromRefs(migName, refs, isRegional)

	return refs, isRegional, nil
}

// ListManagedInstanceGroups returns the managed instance groups available in the project, optionally filtered by location.
func (c *Client) ListManagedInstanceGroups(ctx context.Context, location string) ([]ManagedInstanceGroup, error) {
	logger.Log.Debugf("Listing managed instance groups (location filter: %s)", location)

	if location != "" {
		isRegional := c.isRegion(location)
		items, err := collectInstanceGroupManagers(func(pageToken string) ([]*compute.InstanceGroupManager, string, error) {
			if isRegional {
				call := c.service.RegionInstanceGroupManagers.List(c.project, location).Context(ctx)
				if pageToken != "" {
					call = call.PageToken(pageToken)
				}

				resp, err := call.Do()
				if err != nil {
					logger.Log.Errorf("Failed to list managed instance groups in region %s: %v", location, err)

					return nil, "", fmt.Errorf("failed to list managed instance groups in region %s: %w", location, err)
				}

				return resp.Items, resp.NextPageToken, nil
			}

			call := c.service.InstanceGroupManagers.List(c.project, location).Context(ctx)
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}

			resp, err := call.Do()
			if err != nil {
				logger.Log.Errorf("Failed to list managed instance groups in zone %s: %v", location, err)

				return nil, "", fmt.Errorf("failed to list managed instance groups in zone %s: %w", location, err)
			}

			return resp.Items, resp.NextPageToken, nil
		})
		if err != nil {
			return nil, err
		}

		results := make([]ManagedInstanceGroup, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}

			results = append(results, ManagedInstanceGroup{
				Name:       item.Name,
				Location:   location,
				IsRegional: isRegional,
			})
		}

		return results, nil
	}

	pageToken := ""
	var results []ManagedInstanceGroup

	for {
		call := c.service.InstanceGroupManagers.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list aggregated managed instance groups: %v", err)

			return nil, fmt.Errorf("failed to list managed instance groups: %w", err)
		}

		for scope, scopedList := range list.Items {
			if len(scopedList.InstanceGroupManagers) == 0 {
				continue
			}

			isRegional := strings.HasPrefix(scope, "regions/")
			var locationName string
			if isRegional {
				locationName = extractRegionFromScope(scope)
			} else {
				locationName = extractZoneFromScope(scope)
			}

			for _, mig := range scopedList.InstanceGroupManagers {
				if mig == nil {
					continue
				}

				results = append(results, ManagedInstanceGroup{
					Name:       mig.Name,
					Location:   locationName,
					IsRegional: isRegional,
				})
			}
		}

		if list.NextPageToken == "" {
			break
		}

		pageToken = list.NextPageToken
	}

	return results, nil
}

// ListRegions returns the available regions in the project.
func (c *Client) ListRegions(ctx context.Context) ([]string, error) {
	if c.cache != nil {
		if zones, ok := c.cache.GetZones(c.project); ok && len(zones) > 0 {
			regionSet := make(map[string]struct{})
			for _, zone := range zones {
				region := extractRegionFromZone(zone)
				if region == "" {
					continue
				}

				regionSet[region] = struct{}{}
			}

			regions := make([]string, 0, len(regionSet))
			for region := range regionSet {
				regions = append(regions, region)
			}

			sort.Strings(regions)

			logger.Log.Debugf("Derived %d regions from cached zones", len(regions))

			return regions, nil
		}
	}

	regions, err := c.listRegions(ctx)
	if err != nil {
		return nil, err
	}

	return regions, nil
}

// ListZones returns the available zones in the project.
func (c *Client) ListZones(ctx context.Context) ([]string, error) {
	logger.Log.Debug("Listing compute zones")

	if c.cache != nil {
		if zones, ok := c.cache.GetZones(c.project); ok {
			logger.Log.Debugf("Using %d zones from cache", len(zones))

			return zones, nil
		}
	}

	items, err := collectZones(func(pageToken string) ([]*compute.Zone, string, error) {
		call := c.service.Zones.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list zones: %v", err)

			return nil, "", fmt.Errorf("failed to list zones: %w", err)
		}

		return resp.Items, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(items))
	for _, zone := range items {
		if zone == nil {
			continue
		}

		if strings.ToUpper(zone.Status) != "UP" {
			continue
		}

		names = append(names, zone.Name)
	}

	sort.Strings(names)

	logger.Log.Debugf("Found %d zones", len(names))

	if c.cache != nil {
		if err := c.cache.SetZones(c.project, names); err != nil {
			logger.Log.Warnf("Failed to cache zones for project %s: %v", c.project, err)
		}
	}

	return names, nil
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

func (c *Client) listMIGInstancesFromAggregated(ctx context.Context, migName string, progress ProgressCallback) ([]ManagedInstanceRef, bool, error) {
	scopeName, err := c.findMIGScopeAcrossAggregatedPages(ctx, migName, progress)
	if err != nil {
		return nil, false, err
	}

	progressSuccess(progress, FormatProgressKey("aggregate"), fmt.Sprintf("Discovered MIG %s in %s", migName, scopeName))

	return c.listMIGInstancesInScope(ctx, migName, scopeName, progress)
}

func (c *Client) listMIGInstancesByRegionScan(ctx context.Context, migName string, progress ProgressCallback) ([]ManagedInstanceRef, bool, error) {
	logger.Log.Debug("MIG not found in aggregated list, searching regions individually")

	regions, err := c.listRegions(ctx)
	if err != nil {
		logger.Log.Errorf("Failed to list regions for regional MIG search: %v", err)

		return nil, false, fmt.Errorf("failed to list regions: %w", err)
	}

	if len(regions) == 0 {
		logger.Log.Warnf("No regions available while searching for MIG '%s'", migName)

		return nil, false, fmt.Errorf("MIG '%s': %w", migName, ErrMIGNotFound)
	}

	logger.Log.Debugf("Searching for regional MIG through %d regions", len(regions))

	type regionResult struct {
		refs   []ManagedInstanceRef
		region string
	}

	resultCh := make(chan regionResult, len(regions))

	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	eg, egCtx := errgroup.WithContext(searchCtx)
	eg.SetLimit(DefaultLookupConcurrency)

	slotCount := DefaultLookupConcurrency
	if len(regions) < slotCount {
		slotCount = len(regions)
	}

	// Pre-initialize progress bar with worker slots
	for i := 0; i < slotCount; i++ {
		workerKey := fmt.Sprintf("worker:%02d", i+1)
		progressUpdate(progress, workerKey, "")
	}

	slotCh := make(chan int, slotCount)
	for i := 0; i < slotCount; i++ {
		slotCh <- i
	}

	for _, region := range regions {
		region := region
		eg.Go(func() error {
			select {
			case <-egCtx.Done():
				return egCtx.Err()
			case slotID := <-slotCh:
				defer func() { slotCh <- slotID }()

				workerKey := fmt.Sprintf("worker:%02d", slotID+1)
				progressUpdate(progress, workerKey, fmt.Sprintf("Scanning %s", region))
				logger.Log.Tracef("Checking for regional MIG in region: %s", region)

				refs, err := c.listManagedInstancesInRegionalMIG(egCtx, migName, region)
				if err == nil {
					progressUpdate(progress, workerKey, fmt.Sprintf("✓ Found in %s", region))
					select {
					case resultCh <- regionResult{refs: refs, region: region}:
						cancel()
					case <-egCtx.Done():
					}

					return nil
				}

				if errors.Is(err, context.Canceled) || errors.Is(egCtx.Err(), context.Canceled) {
					progressUpdate(progress, workerKey, "Canceled")

					return context.Canceled
				}

				// Just update status without marking as done
				progressUpdate(progress, workerKey, fmt.Sprintf("✓ Not in %s", region))

				return nil
			}
		})
	}

	go func() {
		_ = eg.Wait()
		close(resultCh)
	}()

	for res := range resultCh {
		if res.refs != nil {
			return res.refs, true, nil
		}
	}

	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return nil, false, err
	}

	logger.Log.Debugf("MIG '%s' not found in any region or zone", migName)
	progressFailure(progress, "", fmt.Sprintf("MIG %s not found in any region", migName), ErrMIGNotFound)

	return nil, false, fmt.Errorf("MIG '%s': %w", migName, ErrMIGNotFound)
}

func (c *Client) listMIGInstancesInScope(ctx context.Context, migName, scopeName string, progress ProgressCallback) ([]ManagedInstanceRef, bool, error) {
	if strings.HasPrefix(scopeName, "zones/") {
		zone := extractZoneFromScope(scopeName)
		logger.Log.Debugf("Found zonal MIG %s in zone %s", migName, zone)
		key := FormatProgressKey("zone:" + zone)
		progressUpdate(progress, key, fmt.Sprintf("Inspecting zone %s for MIG %s", zone, migName))

		refs, err := c.listManagedInstancesInMIGZone(ctx, migName, zone)
		if err != nil {
			progressFailure(progress, key, fmt.Sprintf("Failed to list instances for MIG %s in zone %s", migName, zone), err)

			return nil, false, err
		}

		progressSuccess(progress, key, fmt.Sprintf("Found MIG %s in zone %s", migName, zone))

		return refs, false, nil
	} else if strings.HasPrefix(scopeName, "regions/") {
		region := extractRegionFromScope(scopeName)
		logger.Log.Debugf("Found regional MIG %s in region %s", migName, region)
		key := FormatProgressKey("region:" + region)
		progressUpdate(progress, key, fmt.Sprintf("Inspecting region %s for MIG %s", region, migName))

		refs, err := c.listManagedInstancesInRegionalMIG(ctx, migName, region)
		if err != nil {
			progressFailure(progress, key, fmt.Sprintf("Failed to list instances for MIG %s in region %s", migName, region), err)

			return nil, true, err
		}

		progressSuccess(progress, key, fmt.Sprintf("Found MIG %s in region %s", migName, region))

		return refs, true, nil
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

func (c *Client) findMIGScopeAcrossAggregatedPages(ctx context.Context, migName string, progress ProgressCallback) (string, error) {
	page := 0
	fetch := func(pageToken string) (*compute.InstanceGroupManagerAggregatedList, error) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		call := c.service.InstanceGroupManagers.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to perform MIG aggregated list: %v", err)

			return nil, err
		}

		page++
		//progressUpdate(progress, "aggregate", fmt.Sprintf("Scanning MIG aggregate page %d (next token: %s)", page, list.NextPageToken))

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

	sort.Strings(regionNames)

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
		Project:     c.project,
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

// cacheInstance stores instance location information in the cache while preserving
// any previously saved user preferences (like the IAP flag).
func (c *Client) cacheInstance(instanceName string, instance *Instance) {
	if c == nil || c.cache == nil || instance == nil {
		return
	}

	info := &cache.LocationInfo{
		Project: c.project,
		Zone:    instance.Zone,
		Type:    cache.ResourceTypeInstance,
	}

	if stored, found := c.cache.Get(instanceName); found && stored != nil && stored.IAP != nil {
		info.IAP = stored.IAP
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

func collectInstances(fetch func(pageToken string) ([]*compute.Instance, string, error)) ([]*compute.Instance, error) {
	pageToken := ""
	var all []*compute.Instance

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

func collectInstanceGroupManagers(fetch func(pageToken string) ([]*compute.InstanceGroupManager, string, error)) ([]*compute.InstanceGroupManager, error) {
	pageToken := ""
	var all []*compute.InstanceGroupManager

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

func collectZones(fetch func(pageToken string) ([]*compute.Zone, string, error)) ([]*compute.Zone, error) {
	pageToken := ""
	var all []*compute.Zone

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
	if !cache.Enabled() {
		return nil, nil
	}

	return getSharedCache()
}
