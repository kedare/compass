// Package search provides modular search capabilities for GCP resources.
package search

import (
	"context"
	"strings"
)

// ResourceKind identifies the category of a searchable resource.
type ResourceKind string

const (
	// KindComputeInstance represents a Compute Engine VM instance.
	KindComputeInstance ResourceKind = "compute.instance"
	// KindManagedInstanceGroup represents a Managed Instance Group.
	KindManagedInstanceGroup ResourceKind = "compute.mig"
	// KindInstanceTemplate represents an Instance Template.
	KindInstanceTemplate ResourceKind = "compute.instanceTemplate"
	// KindAddress represents an IP address reservation.
	KindAddress ResourceKind = "compute.address"
	// KindDisk represents a Compute Engine persistent disk.
	KindDisk ResourceKind = "compute.disk"
	// KindSnapshot represents a Compute Engine disk snapshot.
	KindSnapshot ResourceKind = "compute.snapshot"
	// KindBucket represents a Cloud Storage bucket.
	KindBucket ResourceKind = "storage.bucket"
	// KindForwardingRule represents a forwarding rule (load balancer frontend).
	KindForwardingRule ResourceKind = "compute.forwardingRule"
	// KindBackendService represents a backend service.
	KindBackendService ResourceKind = "compute.backendService"
	// KindTargetPool represents a target pool (legacy load balancing).
	KindTargetPool ResourceKind = "compute.targetPool"
	// KindHealthCheck represents a health check.
	KindHealthCheck ResourceKind = "compute.healthCheck"
	// KindURLMap represents a URL map (HTTP(S) load balancer routing).
	KindURLMap ResourceKind = "compute.urlMap"
	// KindCloudSQLInstance represents a Cloud SQL database instance.
	KindCloudSQLInstance ResourceKind = "sqladmin.instance"
	// KindGKECluster represents a GKE Kubernetes cluster.
	KindGKECluster ResourceKind = "container.cluster"
	// KindGKENodePool represents a GKE node pool.
	KindGKENodePool ResourceKind = "container.nodePool"
	// KindVPCNetwork represents a VPC network.
	KindVPCNetwork ResourceKind = "compute.network"
	// KindSubnet represents a VPC subnet.
	KindSubnet ResourceKind = "compute.subnet"
	// KindCloudRunService represents a Cloud Run service.
	KindCloudRunService ResourceKind = "run.service"
	// KindFirewallRule represents a VPC firewall rule.
	KindFirewallRule ResourceKind = "compute.firewall"
	// KindSecret represents a Secret Manager secret.
	KindSecret ResourceKind = "secretmanager.secret"
)

// AllResourceKinds returns all available resource kind values for use in validation and completion.
func AllResourceKinds() []ResourceKind {
	return []ResourceKind{
		KindComputeInstance,
		KindManagedInstanceGroup,
		KindInstanceTemplate,
		KindAddress,
		KindDisk,
		KindSnapshot,
		KindBucket,
		KindForwardingRule,
		KindBackendService,
		KindTargetPool,
		KindHealthCheck,
		KindURLMap,
		KindCloudSQLInstance,
		KindGKECluster,
		KindGKENodePool,
		KindVPCNetwork,
		KindSubnet,
		KindCloudRunService,
		KindFirewallRule,
		KindSecret,
	}
}

// IsValidResourceKind checks if the provided string is a valid resource kind.
func IsValidResourceKind(kind string) bool {
	for _, k := range AllResourceKinds() {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// Query captures the user supplied search term and matching behaviour.
type Query struct {
	Term  string
	Types []ResourceKind // Filter results to these types (empty means all types)
}

// NormalizedTerm returns the lowercase trimmed representation of the query.
func (q Query) NormalizedTerm() string {
	return strings.ToLower(strings.TrimSpace(q.Term))
}

// Matches reports whether the provided value satisfies the query.
func (q Query) Matches(value string) bool {
	normalized := q.NormalizedTerm()
	if normalized == "" {
		return false
	}

	return strings.Contains(strings.ToLower(value), normalized)
}

// MatchesType reports whether the provided resource kind is included in the query's type filter.
// Returns true if no type filter is set (empty Types slice).
func (q Query) MatchesType(kind ResourceKind) bool {
	if len(q.Types) == 0 {
		return true
	}

	for _, t := range q.Types {
		if t == kind {
			return true
		}
	}

	return false
}

// Result describes a single resource that matched the query.
type Result struct {
	Type     ResourceKind
	Name     string
	Project  string
	Location string
	Details  map[string]string
}

// SearchWarning captures a non-fatal error that occurred during search.
// These allow the search to continue with partial results while reporting issues.
type SearchWarning struct {
	Project  string
	Provider ResourceKind
	Err      error
}

// Error implements the error interface for SearchWarning.
func (w SearchWarning) Error() string {
	return w.Err.Error()
}

// SearchOutput contains the results and any warnings from a search operation.
type SearchOutput struct {
	Results  []Result
	Warnings []SearchWarning
}

// Provider defines a search backend for a specific resource type.
type Provider interface {
	// Kind returns the resource kind this provider handles.
	Kind() ResourceKind
	// Search inspects resources in the provided project and returns matches.
	Search(ctx context.Context, project string, query Query) ([]Result, error)
}
