package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kedare/compass/internal/cache"
	"github.com/kedare/compass/internal/logger"
	"google.golang.org/api/compute/v1"
)

// FindInstance finds an instance by name, optionally in a specific zone.
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

	// Try cache first if available - use GetWithProject for precise lookup
	if c.cache != nil {
		if cachedInfo, found := c.cache.GetWithProject(instanceName, c.project); found && cachedInfo.Type == cache.ResourceTypeInstance {
			logger.Log.Debugf("Found instance location in cache: project=%s, zone=%s", cachedInfo.Project, cachedInfo.Zone)

			instance, err := c.findInstanceInZone(ctx, instanceName, cachedInfo.Zone)
			if err == nil {
				logger.Log.Debug("Successfully retrieved instance using cached location")

				return instance, nil
			}

			logger.Log.Debugf("Cached location invalid, performing full search: %v", err)
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

// FindInstanceInMIG finds an instance in a managed instance group.
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
				// Extract MIG name from created-by metadata
				if meta.Key == "created-by" && meta.Value != nil {
					result.MIGName = extractMIGNameFromCreatedBy(*meta.Value)
					logger.Log.Debugf("Instance %s has created-by metadata: %s -> MIG: %s", instance.Name, *meta.Value, result.MIGName)
				}
			}
		}
	}

	// Debug: log if instance looks like a MIG member but has no MIGName
	if result.MIGName == "" && strings.Contains(result.Name, "gke-") {
		logger.Log.Debugf("Instance %s looks like GKE node but has no MIGName. Metadata keys: %v", result.Name, result.MetadataKeys)
	}

	// Prefer IAP only when no external IP is available.
	result.CanUseIAP = !hasExternalIP

	return result
}
