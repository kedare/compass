package gcp

import "errors"

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

// Instance represents a Compute Engine VM instance.
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
	Network     string
	Subnetwork  string
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
	// MIGName is set if this instance belongs to a managed instance group.
	MIGName string
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

// ManagedInstanceRef represents a reference to a managed instance.
type ManagedInstanceRef struct {
	Name   string
	Zone   string
	Status string
}

// IsRunning returns true if the managed instance is in RUNNING state.
func (r ManagedInstanceRef) IsRunning() bool {
	return r.Status == InstanceStatusRunning
}

// ManagedInstanceGroup represents a managed instance group scope.
type ManagedInstanceGroup struct {
	Name       string
	Location   string
	IsRegional bool
	// Extended fields
	Description      string
	TargetSize       int64
	CurrentSize      int64
	InstanceTemplate string
	BaseInstanceName string
	// Status
	IsStable bool
	// Auto-scaling
	AutoscalingEnabled bool
	MinReplicas        int64
	MaxReplicas        int64
	// Update policy
	UpdateType     string
	MaxSurge       string
	MaxUnavailable string
	// Named ports
	NamedPorts map[string]int64
	// Distribution policy (for regional MIGs)
	TargetZones []string
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
	Boot        bool
	AutoDelete  bool
	Mode        string
	Type        string
	DiskType    string
	DiskSizeGb  int64
	SourceImage string
}

// InstanceTemplateNetwork represents network interface in an instance template.
type InstanceTemplateNetwork struct {
	Network       string
	Subnetwork    string
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
