package gcp

import (
	"strings"

	"google.golang.org/api/compute/v1"
)

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

// extractRegionFromZone extracts the region from a zone name (e.g., "us-central1-a" -> "us-central1").
func extractRegionFromZone(zone string) string {
	parts := strings.Split(zone, "-")
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-1], "-")
	}

	return zone
}

func extractResourceName(url string) string {
	if url == "" {
		return ""
	}

	parts := strings.Split(url, "/")

	return parts[len(parts)-1]
}

// extractMIGNameFromCreatedBy extracts the MIG name from a created-by metadata URL.
// The URL format is like:
// https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/instanceGroupManagers/my-mig
// or for regional MIGs:
// https://www.googleapis.com/compute/v1/projects/my-project/regions/us-central1/instanceGroupManagers/my-mig
func extractMIGNameFromCreatedBy(createdByURL string) string {
	if createdByURL == "" {
		return ""
	}

	// Check if it contains instanceGroupManagers
	if !strings.Contains(createdByURL, "instanceGroupManagers") {
		return ""
	}

	// The MIG name is the last component of the URL
	parts := strings.Split(createdByURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return ""
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
