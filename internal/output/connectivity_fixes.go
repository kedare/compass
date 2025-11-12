package output

import (
	"fmt"

	"github.com/kedare/compass/internal/gcp"
)

// fixContext contains the context needed for evaluating suggested fixes
type fixContext struct {
	result      *gcp.ConnectivityTestResult
	details     *gcp.ReachabilityDetails
	source      *gcp.EndpointInfo
	destination *gcp.EndpointInfo
	reverse     bool
}

// suggestedFixChecker defines the interface for all fix checkers
type suggestedFixChecker interface {
	// Check evaluates whether this fix applies and returns a suggestion message
	// Returns empty string if the fix doesn't apply
	Check(ctx fixContext, step *gcp.TraceStep) string
}

// viewerPermissionChecker checks for missing viewer permissions
type viewerPermissionChecker struct{}

func (c *viewerPermissionChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.State == "VIEWER_PERMISSION_MISSING" {
		return "Insufficient permissions to view configuration in this step\nGrant compute.networkViewer role or higher to view complete trace"
	}
	return ""
}

// abortCauseChecker checks for analysis abort conditions
type abortCauseChecker struct{}

func (c *abortCauseChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.AbortCause != "" {
		return fmt.Sprintf("Analysis aborted: %s\nReview the configuration and permissions", step.AbortCause)
	}
	return ""
}

// egressFirewallChecker checks for egress firewall blocking
type egressFirewallChecker struct{}

func (c *egressFirewallChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if !step.CausesDrop || step.Firewall == "" {
		return ""
	}

	if step.State == "APPLY_EGRESS_FIREWALL_RULE" {
		msg := fmt.Sprintf("Egress firewall rule blocks outbound %s traffic\n", ctx.result.Protocol)
		if ctx.destination != nil && ctx.destination.IPAddress != "" {
			msg += fmt.Sprintf("Add egress rule allowing traffic to %s", ctx.destination.IPAddress)
			if ctx.destination.Port > 0 {
				msg += fmt.Sprintf(":%d", ctx.destination.Port)
			}
		}
		return msg
	}
	return ""
}

// ingressFirewallChecker checks for ingress firewall blocking
type ingressFirewallChecker struct{}

func (c *ingressFirewallChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if !step.CausesDrop || step.Firewall == "" {
		return ""
	}

	if step.State != "APPLY_EGRESS_FIREWALL_RULE" {
		msg := fmt.Sprintf("Add firewall rule allowing %s traffic", ctx.result.Protocol)
		if ctx.source != nil && ctx.source.IPAddress != "" {
			msg += fmt.Sprintf(" from %s", ctx.source.IPAddress)
		}
		if ctx.destination != nil {
			if ctx.destination.IPAddress != "" {
				msg += fmt.Sprintf(" to %s", ctx.destination.IPAddress)
			}
			if ctx.destination.Port > 0 {
				msg += fmt.Sprintf(":%d", ctx.destination.Port)
			}
		}
		return msg
	}
	return ""
}

// blackholeRouteChecker checks for blackhole routes
type blackholeRouteChecker struct{}

func (c *blackholeRouteChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.CausesDrop && step.Route != "" && step.RouteNextHopType == "NEXT_HOP_BLACKHOLE" {
		return "Route next hop is blackhole (unreachable)\nUpdate route to use valid next hop or remove blackhole route"
	}
	return ""
}

// vpnTunnelChecker checks for VPN tunnel issues
type vpnTunnelChecker struct{}

func (c *vpnTunnelChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.CausesDrop && step.Route != "" && step.RouteNextHopType == "NEXT_HOP_VPN_TUNNEL" {
		return "VPN tunnel is down or misconfigured\nCheck VPN tunnel status and ensure IKE configuration matches on both ends"
	}
	return ""
}

// interconnectChecker checks for Interconnect attachment issues
type interconnectChecker struct{}

func (c *interconnectChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.CausesDrop && step.Route != "" && step.RouteNextHopType == "NEXT_HOP_INTERCONNECT" {
		return "Interconnect attachment is down or misconfigured\nVerify VLAN attachment status and BGP session state"
	}
	return ""
}

// vpcPeeringChecker checks for VPC peering issues
type vpcPeeringChecker struct{}

func (c *vpcPeeringChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.CausesDrop && step.Route != "" {
		if step.RouteType == "PEERING_SUBNET" || step.RouteType == "PEERING_STATIC" || step.RouteType == "PEERING_DYNAMIC" {
			return "VPC peering route exists but connection failed\nCheck VPC peering configuration and ensure import/export custom routes are enabled"
		}
	}
	return ""
}

// genericRouteChecker checks for generic routing issues
type genericRouteChecker struct{}

func (c *genericRouteChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.CausesDrop && step.Route != "" {
		if ctx.reverse {
			msg := "Check return-path routing configuration and ensure proper route exists\n"
			if ctx.source != nil && ctx.source.IPAddress != "" {
				msg += fmt.Sprintf("Add a route in the destination network to reach source %s", ctx.source.IPAddress)
			}
			return msg
		}
		return "Check routing configuration and ensure proper route exists"
	}
	return ""
}

// loadBalancerChecker checks for load balancer backend issues
type loadBalancerChecker struct{}

func (c *loadBalancerChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.CausesDrop && step.LoadBalancer != "" {
		return "Load balancer backend is unavailable or unhealthy\nCheck backend instance health and firewall rules allowing health checks"
	}
	return ""
}

// sharedVPCChecker checks for shared VPC cross-project issues
type sharedVPCChecker struct{}

func (c *sharedVPCChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.CausesDrop && step.ProjectID != "" && ctx.result.ProjectID != "" && step.ProjectID != ctx.result.ProjectID {
		return "Resource belongs to different project (possible Shared VPC issue)\nVerify Shared VPC configuration and service project permissions"
	}
	return ""
}

// dropReasonChecker checks for generic drop with reason
type dropReasonChecker struct{}

func (c *dropReasonChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.CausesDrop && step.DropReason != "" {
		return fmt.Sprintf("Traffic dropped: %s", step.DropReason)
	}
	return ""
}

// rfc1918InternetGatewayChecker checks for RFC1918 IPs routed to internet gateway
type rfc1918InternetGatewayChecker struct{}

func (c *rfc1918InternetGatewayChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.Route != "" && !step.CausesDrop && step.RouteNextHopType == "NEXT_HOP_INTERNET_GATEWAY" {
		if ctx.destination != nil && ctx.destination.IPAddress != "" && isPrivateIP(ctx.destination.IPAddress) {
			return fmt.Sprintf("Destination IP %s is private (RFC1918) but route next hop is Internet Gateway\nConsider using a VPN tunnel, Interconnect, or internal routing instead", ctx.destination.IPAddress)
		}
	}
	return ""
}

// missingNATChecker checks for instances without external IP or NAT trying to reach internet
type missingNATChecker struct{}

func (c *missingNATChecker) Check(ctx fixContext, step *gcp.TraceStep) string {
	if step.Instance != "" && !step.HasExternalIP && ctx.destination != nil && ctx.destination.IPAddress != "" {
		if !isPrivateIP(ctx.destination.IPAddress) && !hasNATInTraces(ctx.details.Traces) {
			return "Instance has no external IP and no Cloud NAT configured\nAdd external IP to instance or configure Cloud NAT for the subnet"
		}
	}
	return ""
}

// getAllCheckers returns all available fix checkers in priority order
func getAllCheckers() []suggestedFixChecker {
	return []suggestedFixChecker{
		// Permission and analysis issues (highest priority)
		&viewerPermissionChecker{},
		&abortCauseChecker{},

		// Firewall issues
		&egressFirewallChecker{},
		&ingressFirewallChecker{},

		// Specific route issues
		&blackholeRouteChecker{},
		&vpnTunnelChecker{},
		&interconnectChecker{},
		&vpcPeeringChecker{},

		// Load balancer issues
		&loadBalancerChecker{},

		// Project/VPC issues
		&sharedVPCChecker{},

		// Generic drop with reason
		&dropReasonChecker{},

		// Generic routing issue (lower priority, catch-all)
		&genericRouteChecker{},

		// Non-drop issues
		&rfc1918InternetGatewayChecker{},
		&missingNATChecker{},
	}
}
