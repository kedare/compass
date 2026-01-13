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
	"google.golang.org/api/container/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/run/v1"
	"google.golang.org/api/secretmanager/v1"
	"google.golang.org/api/sqladmin/v1beta4"
	"google.golang.org/api/storage/v1"
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
	// Extended fields
	Description       string
	CreationTimestamp string
	CPUPlatform       string
	// Network
	Network    string
	Subnetwork string
	NetworkTags []string
	// Disks
	Disks []InstanceDisk
	// Scheduling
	Preemptible       bool
	AutomaticRestart  bool
	OnHostMaintenance string
	// Service accounts
	ServiceAccounts []string
	// Labels
	Labels map[string]string
	// Metadata keys (not values for security)
	MetadataKeys []string
}

// InstanceDisk represents a disk attached to an instance.
type InstanceDisk struct {
	Name       string
	Boot       bool
	AutoDelete bool
	Mode       string
	Type       string
	DiskSizeGb int64
	DiskType   string
	Source     string
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
	// Extended fields
	Description        string
	TargetSize         int64
	CurrentSize        int64
	InstanceTemplate   string
	BaseInstanceName   string
	// Status
	IsStable           bool
	// Auto-scaling
	AutoscalingEnabled bool
	MinReplicas        int64
	MaxReplicas        int64
	// Update policy
	UpdateType         string
	MaxSurge           string
	MaxUnavailable     string
	// Named ports
	NamedPorts         map[string]int64
	// Distribution policy (for regional MIGs)
	TargetZones        []string
}

// InstanceTemplate represents a Compute Engine instance template.
type InstanceTemplate struct {
	Name           string
	Description    string
	MachineType    string
	CanIPForward   bool
	MinCPUPlatform string
	// Disk information
	Disks []InstanceTemplateDisk
	// Network information
	NetworkInterfaces []InstanceTemplateNetwork
	// Service accounts
	ServiceAccounts []string
	// Tags (network tags)
	Tags []string
	// Labels
	Labels map[string]string
	// Scheduling
	Preemptible       bool
	AutomaticRestart  bool
	OnHostMaintenance string
	// GPU accelerators
	GPUAccelerators []InstanceTemplateGPU
	// Metadata keys (not values for security)
	MetadataKeys []string
}

// InstanceTemplateDisk represents disk configuration in an instance template.
type InstanceTemplateDisk struct {
	Boot       bool
	AutoDelete bool
	Mode       string
	Type       string
	DiskType   string
	DiskSizeGb int64
	SourceImage string
}

// InstanceTemplateNetwork represents network interface in an instance template.
type InstanceTemplateNetwork struct {
	Network    string
	Subnetwork string
	HasExternalIP bool
}

// InstanceTemplateGPU represents GPU accelerator configuration.
type InstanceTemplateGPU struct {
	Type  string
	Count int64
}

// Address represents an IP address reservation.
type Address struct {
	Name        string
	Address     string
	Region      string
	AddressType string
	Status      string
}

// Disk represents a Compute Engine persistent disk.
type Disk struct {
	Name   string
	Zone   string
	SizeGb int64
	Type   string
	Status string
}

// Snapshot represents a Compute Engine disk snapshot.
type Snapshot struct {
	Name         string
	DiskSizeGb   int64
	StorageBytes int64
	Status       string
	SourceDisk   string
}

// Bucket represents a Cloud Storage bucket.
type Bucket struct {
	Name         string
	Location     string
	StorageClass string
	LocationType string
}

// ForwardingRule represents a forwarding rule (load balancer frontend).
type ForwardingRule struct {
	Name                string
	Region              string
	IPAddress           string
	IPProtocol          string
	PortRange           string
	LoadBalancingScheme string
}

// BackendService represents a backend service.
type BackendService struct {
	Name                string
	Region              string
	Protocol            string
	LoadBalancingScheme string
	HealthCheckCount    int
	BackendCount        int
}

// TargetPool represents a target pool (legacy load balancing).
type TargetPool struct {
	Name            string
	Region          string
	SessionAffinity string
	InstanceCount   int
}

// HealthCheck represents a health check.
type HealthCheck struct {
	Name string
	Type string
	Port int64
}

// URLMap represents a URL map (HTTP(S) load balancer routing).
type URLMap struct {
	Name             string
	DefaultService   string
	HostRuleCount    int
	PathMatcherCount int
}

// CloudSQLInstance represents a Cloud SQL database instance.
type CloudSQLInstance struct {
	Name            string
	Region          string
	DatabaseVersion string
	Tier            string
	State           string
}

// GKECluster represents a GKE Kubernetes cluster.
type GKECluster struct {
	Name                 string
	Location             string
	Status               string
	CurrentMasterVersion string
	NodeCount            int
}

// GKENodePool represents a GKE node pool.
type GKENodePool struct {
	Name        string
	ClusterName string
	Location    string
	MachineType string
	NodeCount   int
	Status      string
}

// VPCNetwork represents a VPC network.
type VPCNetwork struct {
	Name                  string
	AutoCreateSubnetworks bool
	SubnetCount           int
}

// Subnet represents a VPC subnet.
type Subnet struct {
	Name        string
	Region      string
	Network     string
	IPCidrRange string
	Purpose     string
}

// CloudRunService represents a Cloud Run service.
type CloudRunService struct {
	Name           string
	Region         string
	URL            string
	LatestRevision string
}

// FirewallRule represents a VPC firewall rule.
type FirewallRule struct {
	Name      string
	Network   string
	Direction string
	Priority  int64
	Disabled  bool
}

// Secret represents a Secret Manager secret.
type Secret struct {
	Name         string
	Replication  string
	VersionCount int
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

			results = append(results, convertManagedInstanceGroup(item, location, isRegional))
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

				results = append(results, convertManagedInstanceGroup(mig, locationName, isRegional))
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

// ListInstanceTemplates returns the instance templates available in the project.
func (c *Client) ListInstanceTemplates(ctx context.Context) ([]*InstanceTemplate, error) {
	logger.Log.Debug("Listing instance templates")

	items, err := collectInstanceTemplates(func(pageToken string) ([]*compute.InstanceTemplate, string, error) {
		call := c.service.InstanceTemplates.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list instance templates: %v", err)

			return nil, "", fmt.Errorf("failed to list instance templates: %w", err)
		}

		return resp.Items, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]*InstanceTemplate, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}

		tmpl := &InstanceTemplate{
			Name:        item.Name,
			Description: item.Description,
		}

		if item.Properties != nil {
			props := item.Properties
			tmpl.MachineType = props.MachineType
			tmpl.CanIPForward = props.CanIpForward
			tmpl.MinCPUPlatform = props.MinCpuPlatform

			// Parse disks
			for _, disk := range props.Disks {
				if disk == nil {
					continue
				}
				d := InstanceTemplateDisk{
					Boot:       disk.Boot,
					AutoDelete: disk.AutoDelete,
					Mode:       disk.Mode,
					Type:       disk.Type,
				}
				if disk.InitializeParams != nil {
					d.DiskType = extractResourceName(disk.InitializeParams.DiskType)
					d.DiskSizeGb = disk.InitializeParams.DiskSizeGb
					d.SourceImage = extractResourceName(disk.InitializeParams.SourceImage)
				}
				tmpl.Disks = append(tmpl.Disks, d)
			}

			// Parse network interfaces
			for _, nic := range props.NetworkInterfaces {
				if nic == nil {
					continue
				}
				n := InstanceTemplateNetwork{
					Network:       extractResourceName(nic.Network),
					Subnetwork:    extractResourceName(nic.Subnetwork),
					HasExternalIP: len(nic.AccessConfigs) > 0,
				}
				tmpl.NetworkInterfaces = append(tmpl.NetworkInterfaces, n)
			}

			// Parse service accounts
			for _, sa := range props.ServiceAccounts {
				if sa != nil && sa.Email != "" {
					tmpl.ServiceAccounts = append(tmpl.ServiceAccounts, sa.Email)
				}
			}

			// Parse tags
			if props.Tags != nil {
				tmpl.Tags = props.Tags.Items
			}

			// Parse labels
			tmpl.Labels = props.Labels

			// Parse scheduling
			if props.Scheduling != nil {
				tmpl.Preemptible = props.Scheduling.Preemptible
				if props.Scheduling.AutomaticRestart != nil {
					tmpl.AutomaticRestart = *props.Scheduling.AutomaticRestart
				}
				tmpl.OnHostMaintenance = props.Scheduling.OnHostMaintenance
			}

			// Parse GPU accelerators
			for _, gpu := range props.GuestAccelerators {
				if gpu == nil {
					continue
				}
				tmpl.GPUAccelerators = append(tmpl.GPUAccelerators, InstanceTemplateGPU{
					Type:  extractResourceName(gpu.AcceleratorType),
					Count: gpu.AcceleratorCount,
				})
			}

			// Parse metadata keys (not values for security)
			if props.Metadata != nil {
				for _, meta := range props.Metadata.Items {
					if meta != nil {
						tmpl.MetadataKeys = append(tmpl.MetadataKeys, meta.Key)
					}
				}
			}
		}

		results = append(results, tmpl)
	}

	return results, nil
}

// ListAddresses returns the IP address reservations available in the project.
func (c *Client) ListAddresses(ctx context.Context) ([]*Address, error) {
	logger.Log.Debug("Listing IP addresses")

	pageToken := ""
	var results []*Address

	for {
		call := c.service.Addresses.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list aggregated addresses: %v", err)

			return nil, fmt.Errorf("failed to list addresses: %w", err)
		}

		for scope, scopedList := range list.Items {
			if len(scopedList.Addresses) == 0 {
				continue
			}

			regionName := extractRegionFromScope(scope)

			for _, addr := range scopedList.Addresses {
				if addr == nil {
					continue
				}

				results = append(results, &Address{
					Name:        addr.Name,
					Address:     addr.Address,
					Region:      regionName,
					AddressType: addr.AddressType,
					Status:      addr.Status,
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

// ListDisks returns the persistent disks available in the project.
func (c *Client) ListDisks(ctx context.Context) ([]*Disk, error) {
	logger.Log.Debug("Listing disks")

	pageToken := ""
	var results []*Disk

	for {
		call := c.service.Disks.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list aggregated disks: %v", err)

			return nil, fmt.Errorf("failed to list disks: %w", err)
		}

		for scope, scopedList := range list.Items {
			if len(scopedList.Disks) == 0 {
				continue
			}

			zoneName := extractZoneFromScope(scope)

			for _, disk := range scopedList.Disks {
				if disk == nil {
					continue
				}

				results = append(results, &Disk{
					Name:   disk.Name,
					Zone:   zoneName,
					SizeGb: disk.SizeGb,
					Type:   extractDiskType(disk.Type),
					Status: disk.Status,
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

// ListSnapshots returns the disk snapshots available in the project.
func (c *Client) ListSnapshots(ctx context.Context) ([]*Snapshot, error) {
	logger.Log.Debug("Listing snapshots")

	items, err := collectSnapshots(func(pageToken string) ([]*compute.Snapshot, string, error) {
		call := c.service.Snapshots.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list snapshots: %v", err)

			return nil, "", fmt.Errorf("failed to list snapshots: %w", err)
		}

		return resp.Items, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]*Snapshot, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}

		results = append(results, &Snapshot{
			Name:         item.Name,
			DiskSizeGb:   item.DiskSizeGb,
			StorageBytes: item.StorageBytes,
			Status:       item.Status,
			SourceDisk:   extractDiskName(item.SourceDisk),
		})
	}

	return results, nil
}

// ListBuckets returns the Cloud Storage buckets available in the project.
func (c *Client) ListBuckets(ctx context.Context) ([]*Bucket, error) {
	logger.Log.Debug("Listing Cloud Storage buckets")

	httpClient, err := newHTTPClientWithLogging(ctx, storage.CloudPlatformReadOnlyScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client for Storage: %v", err)

		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	storageService, err := storage.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create Storage service: %v", err)

		return nil, fmt.Errorf("failed to create storage service: %w", err)
	}

	items, err := collectBuckets(func(pageToken string) ([]*storage.Bucket, string, error) {
		call := storageService.Buckets.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list buckets: %v", err)

			return nil, "", fmt.Errorf("failed to list buckets: %w", err)
		}

		return resp.Items, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]*Bucket, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}

		results = append(results, &Bucket{
			Name:         item.Name,
			Location:     item.Location,
			StorageClass: item.StorageClass,
			LocationType: item.LocationType,
		})
	}

	return results, nil
}

// ListForwardingRules returns the forwarding rules available in the project.
func (c *Client) ListForwardingRules(ctx context.Context) ([]*ForwardingRule, error) {
	logger.Log.Debug("Listing forwarding rules")

	pageToken := ""
	var results []*ForwardingRule

	for {
		call := c.service.ForwardingRules.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list aggregated forwarding rules: %v", err)

			return nil, fmt.Errorf("failed to list forwarding rules: %w", err)
		}

		for scope, scopedList := range list.Items {
			if len(scopedList.ForwardingRules) == 0 {
				continue
			}

			regionName := extractRegionFromScope(scope)

			for _, rule := range scopedList.ForwardingRules {
				if rule == nil {
					continue
				}

				results = append(results, &ForwardingRule{
					Name:                rule.Name,
					Region:              regionName,
					IPAddress:           rule.IPAddress,
					IPProtocol:          rule.IPProtocol,
					PortRange:           rule.PortRange,
					LoadBalancingScheme: rule.LoadBalancingScheme,
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

// ListBackendServices returns the backend services available in the project.
func (c *Client) ListBackendServices(ctx context.Context) ([]*BackendService, error) {
	logger.Log.Debug("Listing backend services")

	pageToken := ""
	var results []*BackendService

	for {
		call := c.service.BackendServices.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list aggregated backend services: %v", err)

			return nil, fmt.Errorf("failed to list backend services: %w", err)
		}

		for scope, scopedList := range list.Items {
			if len(scopedList.BackendServices) == 0 {
				continue
			}

			regionName := extractRegionFromScope(scope)

			for _, svc := range scopedList.BackendServices {
				if svc == nil {
					continue
				}

				results = append(results, &BackendService{
					Name:                svc.Name,
					Region:              regionName,
					Protocol:            svc.Protocol,
					LoadBalancingScheme: svc.LoadBalancingScheme,
					HealthCheckCount:    len(svc.HealthChecks),
					BackendCount:        len(svc.Backends),
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

// ListTargetPools returns the target pools available in the project.
func (c *Client) ListTargetPools(ctx context.Context) ([]*TargetPool, error) {
	logger.Log.Debug("Listing target pools")

	pageToken := ""
	var results []*TargetPool

	for {
		call := c.service.TargetPools.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list aggregated target pools: %v", err)

			return nil, fmt.Errorf("failed to list target pools: %w", err)
		}

		for scope, scopedList := range list.Items {
			if len(scopedList.TargetPools) == 0 {
				continue
			}

			regionName := extractRegionFromScope(scope)

			for _, pool := range scopedList.TargetPools {
				if pool == nil {
					continue
				}

				results = append(results, &TargetPool{
					Name:            pool.Name,
					Region:          regionName,
					SessionAffinity: pool.SessionAffinity,
					InstanceCount:   len(pool.Instances),
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

// ListHealthChecks returns the health checks available in the project.
func (c *Client) ListHealthChecks(ctx context.Context) ([]*HealthCheck, error) {
	logger.Log.Debug("Listing health checks")

	items, err := collectHealthChecks(func(pageToken string) ([]*compute.HealthCheck, string, error) {
		call := c.service.HealthChecks.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list health checks: %v", err)

			return nil, "", fmt.Errorf("failed to list health checks: %w", err)
		}

		return resp.Items, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]*HealthCheck, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}

		results = append(results, &HealthCheck{
			Name: item.Name,
			Type: item.Type,
			Port: extractHealthCheckPort(item),
		})
	}

	return results, nil
}

// ListURLMaps returns the URL maps available in the project.
func (c *Client) ListURLMaps(ctx context.Context) ([]*URLMap, error) {
	logger.Log.Debug("Listing URL maps")

	items, err := collectURLMaps(func(pageToken string) ([]*compute.UrlMap, string, error) {
		call := c.service.UrlMaps.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list URL maps: %v", err)

			return nil, "", fmt.Errorf("failed to list URL maps: %w", err)
		}

		return resp.Items, resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]*URLMap, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}

		results = append(results, &URLMap{
			Name:             item.Name,
			DefaultService:   extractResourceName(item.DefaultService),
			HostRuleCount:    len(item.HostRules),
			PathMatcherCount: len(item.PathMatchers),
		})
	}

	return results, nil
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
		Name:              instance.Name,
		Project:           c.project,
		Zone:              extractZoneName(instance.Zone),
		Status:            instance.Status,
		MachineType:       extractMachineType(instance.MachineType),
		Description:       instance.Description,
		CreationTimestamp: instance.CreationTimestamp,
		CPUPlatform:       instance.CpuPlatform,
		Labels:            instance.Labels,
	}

	// Extract IP addresses and network info
	hasExternalIP := false

	for _, networkInterface := range instance.NetworkInterfaces {
		if networkInterface.NetworkIP != "" {
			result.InternalIP = networkInterface.NetworkIP
		}
		if result.Network == "" {
			result.Network = extractResourceName(networkInterface.Network)
		}
		if result.Subnetwork == "" {
			result.Subnetwork = extractResourceName(networkInterface.Subnetwork)
		}

		for _, accessConfig := range networkInterface.AccessConfigs {
			if accessConfig.NatIP != "" {
				result.ExternalIP = accessConfig.NatIP
				hasExternalIP = true
			}
		}
	}

	// Network tags
	if instance.Tags != nil {
		result.NetworkTags = instance.Tags.Items
	}

	// Disks
	for _, disk := range instance.Disks {
		if disk == nil {
			continue
		}
		d := InstanceDisk{
			Boot:       disk.Boot,
			AutoDelete: disk.AutoDelete,
			Mode:       disk.Mode,
			Type:       disk.Type,
			DiskSizeGb: disk.DiskSizeGb,
			Source:     extractResourceName(disk.Source),
		}
		// Extract disk type from source
		if disk.Source != "" {
			d.Name = extractResourceName(disk.Source)
		}
		result.Disks = append(result.Disks, d)
	}

	// Scheduling
	if instance.Scheduling != nil {
		result.Preemptible = instance.Scheduling.Preemptible
		if instance.Scheduling.AutomaticRestart != nil {
			result.AutomaticRestart = *instance.Scheduling.AutomaticRestart
		}
		result.OnHostMaintenance = instance.Scheduling.OnHostMaintenance
	}

	// Service accounts
	for _, sa := range instance.ServiceAccounts {
		if sa != nil && sa.Email != "" {
			result.ServiceAccounts = append(result.ServiceAccounts, sa.Email)
		}
	}

	// Metadata keys (not values for security)
	if instance.Metadata != nil {
		for _, meta := range instance.Metadata.Items {
			if meta != nil {
				result.MetadataKeys = append(result.MetadataKeys, meta.Key)
			}
		}
	}

	// Prefer IAP only when no external IP is available.
	result.CanUseIAP = !hasExternalIP

	return result
}

func convertManagedInstanceGroup(mig *compute.InstanceGroupManager, location string, isRegional bool) ManagedInstanceGroup {
	result := ManagedInstanceGroup{
		Name:             mig.Name,
		Location:         location,
		IsRegional:       isRegional,
		Description:      mig.Description,
		TargetSize:       mig.TargetSize,
		InstanceTemplate: extractResourceName(mig.InstanceTemplate),
		BaseInstanceName: mig.BaseInstanceName,
	}

	// Status
	if mig.Status != nil {
		result.IsStable = mig.Status.IsStable
	}

	// Current actions (to calculate current size)
	if mig.CurrentActions != nil {
		result.CurrentSize = mig.CurrentActions.None // Instances that are running and stable
	}

	// Update policy
	if mig.UpdatePolicy != nil {
		result.UpdateType = mig.UpdatePolicy.Type
		if mig.UpdatePolicy.MaxSurge != nil {
			if mig.UpdatePolicy.MaxSurge.Fixed > 0 {
				result.MaxSurge = fmt.Sprintf("%d", mig.UpdatePolicy.MaxSurge.Fixed)
			} else if mig.UpdatePolicy.MaxSurge.Percent > 0 {
				result.MaxSurge = fmt.Sprintf("%d%%", mig.UpdatePolicy.MaxSurge.Percent)
			}
		}
		if mig.UpdatePolicy.MaxUnavailable != nil {
			if mig.UpdatePolicy.MaxUnavailable.Fixed > 0 {
				result.MaxUnavailable = fmt.Sprintf("%d", mig.UpdatePolicy.MaxUnavailable.Fixed)
			} else if mig.UpdatePolicy.MaxUnavailable.Percent > 0 {
				result.MaxUnavailable = fmt.Sprintf("%d%%", mig.UpdatePolicy.MaxUnavailable.Percent)
			}
		}
	}

	// Named ports
	if len(mig.NamedPorts) > 0 {
		result.NamedPorts = make(map[string]int64)
		for _, np := range mig.NamedPorts {
			if np != nil {
				result.NamedPorts[np.Name] = np.Port
			}
		}
	}

	// Distribution policy (for regional MIGs)
	if mig.DistributionPolicy != nil && len(mig.DistributionPolicy.Zones) > 0 {
		for _, zone := range mig.DistributionPolicy.Zones {
			if zone != nil {
				result.TargetZones = append(result.TargetZones, extractResourceName(zone.Zone))
			}
		}
	}

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

func extractDiskType(diskTypeURL string) string {
	parts := strings.Split(diskTypeURL, "/")

	return parts[len(parts)-1]
}

func extractDiskName(diskURL string) string {
	parts := strings.Split(diskURL, "/")

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

func collectInstanceTemplates(fetch func(pageToken string) ([]*compute.InstanceTemplate, string, error)) ([]*compute.InstanceTemplate, error) {
	pageToken := ""
	var all []*compute.InstanceTemplate

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

func collectSnapshots(fetch func(pageToken string) ([]*compute.Snapshot, string, error)) ([]*compute.Snapshot, error) {
	pageToken := ""
	var all []*compute.Snapshot

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

func collectBuckets(fetch func(pageToken string) ([]*storage.Bucket, string, error)) ([]*storage.Bucket, error) {
	pageToken := ""
	var all []*storage.Bucket

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

func collectHealthChecks(fetch func(pageToken string) ([]*compute.HealthCheck, string, error)) ([]*compute.HealthCheck, error) {
	pageToken := ""
	var all []*compute.HealthCheck

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

func collectURLMaps(fetch func(pageToken string) ([]*compute.UrlMap, string, error)) ([]*compute.UrlMap, error) {
	pageToken := ""
	var all []*compute.UrlMap

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

func extractHealthCheckPort(hc *compute.HealthCheck) int64 {
	if hc.HttpHealthCheck != nil {
		return hc.HttpHealthCheck.Port
	}
	if hc.HttpsHealthCheck != nil {
		return hc.HttpsHealthCheck.Port
	}
	if hc.TcpHealthCheck != nil {
		return hc.TcpHealthCheck.Port
	}
	if hc.SslHealthCheck != nil {
		return hc.SslHealthCheck.Port
	}
	if hc.Http2HealthCheck != nil {
		return hc.Http2HealthCheck.Port
	}
	if hc.GrpcHealthCheck != nil {
		return hc.GrpcHealthCheck.Port
	}

	return 0
}

func extractResourceName(url string) string {
	if url == "" {
		return ""
	}

	parts := strings.Split(url, "/")

	return parts[len(parts)-1]
}

// ListCloudSQLInstances returns the Cloud SQL instances available in the project.
func (c *Client) ListCloudSQLInstances(ctx context.Context) ([]*CloudSQLInstance, error) {
	logger.Log.Debug("Listing Cloud SQL instances")

	httpClient, err := newHTTPClientWithLogging(ctx, sqladmin.CloudPlatformScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client for SQL Admin: %v", err)

		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	sqlService, err := sqladmin.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create SQL Admin service: %v", err)

		return nil, fmt.Errorf("failed to create sql admin service: %w", err)
	}

	var results []*CloudSQLInstance
	pageToken := ""

	for {
		call := sqlService.Instances.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list Cloud SQL instances: %v", err)

			return nil, fmt.Errorf("failed to list cloud sql instances: %w", err)
		}

		for _, item := range resp.Items {
			if item == nil {
				continue
			}

			results = append(results, &CloudSQLInstance{
				Name:            item.Name,
				Region:          item.Region,
				DatabaseVersion: item.DatabaseVersion,
				Tier:            item.Settings.Tier,
				State:           item.State,
			})
		}

		if resp.NextPageToken == "" {
			break
		}

		pageToken = resp.NextPageToken
	}

	return results, nil
}

// ListGKEClusters returns the GKE clusters available in the project.
func (c *Client) ListGKEClusters(ctx context.Context) ([]*GKECluster, error) {
	logger.Log.Debug("Listing GKE clusters")

	httpClient, err := newHTTPClientWithLogging(ctx, container.CloudPlatformScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client for Container: %v", err)

		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	containerService, err := container.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create Container service: %v", err)

		return nil, fmt.Errorf("failed to create container service: %w", err)
	}

	// Use "-" to list clusters in all locations
	parent := fmt.Sprintf("projects/%s/locations/-", c.project)
	resp, err := containerService.Projects.Locations.Clusters.List(parent).Context(ctx).Do()
	if err != nil {
		logger.Log.Errorf("Failed to list GKE clusters: %v", err)

		return nil, fmt.Errorf("failed to list gke clusters: %w", err)
	}

	results := make([]*GKECluster, 0, len(resp.Clusters))
	for _, cluster := range resp.Clusters {
		if cluster == nil {
			continue
		}

		nodeCount := 0
		for _, pool := range cluster.NodePools {
			if pool != nil {
				nodeCount += int(pool.InitialNodeCount)
			}
		}

		results = append(results, &GKECluster{
			Name:                 cluster.Name,
			Location:             cluster.Location,
			Status:               cluster.Status,
			CurrentMasterVersion: cluster.CurrentMasterVersion,
			NodeCount:            nodeCount,
		})
	}

	return results, nil
}

// ListGKENodePools returns the GKE node pools available in the project.
func (c *Client) ListGKENodePools(ctx context.Context) ([]*GKENodePool, error) {
	logger.Log.Debug("Listing GKE node pools")

	httpClient, err := newHTTPClientWithLogging(ctx, container.CloudPlatformScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client for Container: %v", err)

		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	containerService, err := container.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create Container service: %v", err)

		return nil, fmt.Errorf("failed to create container service: %w", err)
	}

	// First list all clusters
	parent := fmt.Sprintf("projects/%s/locations/-", c.project)
	clustersResp, err := containerService.Projects.Locations.Clusters.List(parent).Context(ctx).Do()
	if err != nil {
		logger.Log.Errorf("Failed to list GKE clusters for node pools: %v", err)

		return nil, fmt.Errorf("failed to list gke clusters: %w", err)
	}

	var results []*GKENodePool
	for _, cluster := range clustersResp.Clusters {
		if cluster == nil {
			continue
		}

		for _, pool := range cluster.NodePools {
			if pool == nil {
				continue
			}

			machineType := ""
			if pool.Config != nil {
				machineType = pool.Config.MachineType
			}

			results = append(results, &GKENodePool{
				Name:        pool.Name,
				ClusterName: cluster.Name,
				Location:    cluster.Location,
				MachineType: machineType,
				NodeCount:   int(pool.InitialNodeCount),
				Status:      pool.Status,
			})
		}
	}

	return results, nil
}

// ListVPCNetworks returns the VPC networks available in the project.
func (c *Client) ListVPCNetworks(ctx context.Context) ([]*VPCNetwork, error) {
	logger.Log.Debug("Listing VPC networks")

	pageToken := ""
	var results []*VPCNetwork

	for {
		call := c.service.Networks.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list VPC networks: %v", err)

			return nil, fmt.Errorf("failed to list vpc networks: %w", err)
		}

		for _, network := range resp.Items {
			if network == nil {
				continue
			}

			results = append(results, &VPCNetwork{
				Name:                  network.Name,
				AutoCreateSubnetworks: network.AutoCreateSubnetworks,
				SubnetCount:           len(network.Subnetworks),
			})
		}

		if resp.NextPageToken == "" {
			break
		}

		pageToken = resp.NextPageToken
	}

	return results, nil
}

// ListSubnets returns the VPC subnets available in the project.
func (c *Client) ListSubnets(ctx context.Context) ([]*Subnet, error) {
	logger.Log.Debug("Listing VPC subnets")

	pageToken := ""
	var results []*Subnet

	for {
		call := c.service.Subnetworks.AggregatedList(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list aggregated subnets: %v", err)

			return nil, fmt.Errorf("failed to list subnets: %w", err)
		}

		for scope, scopedList := range list.Items {
			if len(scopedList.Subnetworks) == 0 {
				continue
			}

			regionName := extractRegionFromScope(scope)

			for _, subnet := range scopedList.Subnetworks {
				if subnet == nil {
					continue
				}

				results = append(results, &Subnet{
					Name:        subnet.Name,
					Region:      regionName,
					Network:     extractResourceName(subnet.Network),
					IPCidrRange: subnet.IpCidrRange,
					Purpose:     subnet.Purpose,
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

// ListCloudRunServices returns the Cloud Run services available in the project.
func (c *Client) ListCloudRunServices(ctx context.Context) ([]*CloudRunService, error) {
	logger.Log.Debug("Listing Cloud Run services")

	httpClient, err := newHTTPClientWithLogging(ctx, run.CloudPlatformScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client for Cloud Run: %v", err)

		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	runService, err := run.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create Cloud Run service: %v", err)

		return nil, fmt.Errorf("failed to create cloud run service: %w", err)
	}

	// List services using the namespaces API (v1)
	parent := fmt.Sprintf("namespaces/%s", c.project)
	resp, err := runService.Namespaces.Services.List(parent).Context(ctx).Do()
	if err != nil {
		logger.Log.Errorf("Failed to list Cloud Run services: %v", err)

		return nil, fmt.Errorf("failed to list cloud run services: %w", err)
	}

	results := make([]*CloudRunService, 0, len(resp.Items))
	for _, svc := range resp.Items {
		if svc == nil {
			continue
		}

		region := ""
		if svc.Metadata != nil && svc.Metadata.Labels != nil {
			region = svc.Metadata.Labels["cloud.googleapis.com/location"]
		}

		url := ""
		latestRevision := ""
		if svc.Status != nil {
			url = svc.Status.Url
			latestRevision = svc.Status.LatestReadyRevisionName
		}

		name := ""
		if svc.Metadata != nil {
			name = svc.Metadata.Name
		}

		results = append(results, &CloudRunService{
			Name:           name,
			Region:         region,
			URL:            url,
			LatestRevision: latestRevision,
		})
	}

	return results, nil
}

// ListFirewallRules returns the firewall rules available in the project.
func (c *Client) ListFirewallRules(ctx context.Context) ([]*FirewallRule, error) {
	logger.Log.Debug("Listing firewall rules")

	pageToken := ""
	var results []*FirewallRule

	for {
		call := c.service.Firewalls.List(c.project).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list firewall rules: %v", err)

			return nil, fmt.Errorf("failed to list firewall rules: %w", err)
		}

		for _, rule := range resp.Items {
			if rule == nil {
				continue
			}

			results = append(results, &FirewallRule{
				Name:      rule.Name,
				Network:   extractResourceName(rule.Network),
				Direction: rule.Direction,
				Priority:  rule.Priority,
				Disabled:  rule.Disabled,
			})
		}

		if resp.NextPageToken == "" {
			break
		}

		pageToken = resp.NextPageToken
	}

	return results, nil
}

// ListSecrets returns the Secret Manager secrets available in the project.
func (c *Client) ListSecrets(ctx context.Context) ([]*Secret, error) {
	logger.Log.Debug("Listing Secret Manager secrets")

	httpClient, err := newHTTPClientWithLogging(ctx, secretmanager.CloudPlatformScope)
	if err != nil {
		logger.Log.Errorf("Failed to create HTTP client for Secret Manager: %v", err)

		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	secretService, err := secretmanager.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		logger.Log.Errorf("Failed to create Secret Manager service: %v", err)

		return nil, fmt.Errorf("failed to create secret manager service: %w", err)
	}

	var results []*Secret
	pageToken := ""

	for {
		parent := fmt.Sprintf("projects/%s", c.project)
		call := secretService.Projects.Secrets.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			logger.Log.Errorf("Failed to list secrets: %v", err)

			return nil, fmt.Errorf("failed to list secrets: %w", err)
		}

		for _, secret := range resp.Secrets {
			if secret == nil {
				continue
			}

			replication := ""
			if secret.Replication != nil {
				if secret.Replication.Automatic != nil {
					replication = "automatic"
				} else if secret.Replication.UserManaged != nil {
					replication = "user-managed"
				}
			}

			// Extract just the secret name from the full resource name
			name := extractResourceName(secret.Name)

			results = append(results, &Secret{
				Name:         name,
				Replication:  replication,
				VersionCount: 0, // Would need additional API call to get version count
			})
		}

		if resp.NextPageToken == "" {
			break
		}

		pageToken = resp.NextPageToken
	}

	return results, nil
}

// LoadCache loads and returns the cache instance.
func LoadCache() (*cache.Cache, error) {
	if !cache.Enabled() {
		return nil, nil
	}

	return getSharedCache()
}
