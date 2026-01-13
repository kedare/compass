package gcp

import (
	"context"
	"fmt"

	"github.com/kedare/compass/internal/logger"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/run/v1"
	"google.golang.org/api/secretmanager/v1"
	"google.golang.org/api/sqladmin/v1beta4"
	"google.golang.org/api/storage/v1"
)

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
