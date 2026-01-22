package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/compute/v1"
)

// GetManagedInstanceGroup returns details of a specific managed instance group.
func (c *Client) GetManagedInstanceGroup(ctx context.Context, migName, location string) (*ManagedInstanceGroup, error) {
	logger.Log.Debugf("Getting managed instance group: %s in %s", migName, location)

	isRegional := c.isRegion(location)

	var mig *compute.InstanceGroupManager
	var err error

	if isRegional {
		mig, err = c.service.RegionInstanceGroupManagers.Get(c.project, location, migName).Context(ctx).Do()
	} else {
		mig, err = c.service.InstanceGroupManagers.Get(c.project, location, migName).Context(ctx).Do()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get managed instance group %s: %w", migName, err)
	}

	result := convertManagedInstanceGroup(mig, location, isRegional)
	return &result, nil
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
