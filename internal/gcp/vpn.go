// Package gcp provides types and methods for interacting with Google Cloud Platform resources.
// This file contains VPN-related functionality for Cloud VPN gateways, tunnels, and BGP sessions.
package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"codeberg.org/kedare/compass/internal/logger"
	"google.golang.org/api/compute/v1"
)

// VPNOverview aggregates the complete Cloud VPN infrastructure state for a GCP project.
// It organizes HA VPN gateways with their associated tunnels and BGP sessions, while
// also tracking resources that lack proper associations (orphan tunnels and sessions).
type VPNOverview struct {
	// Gateways contains all HA VPN gateways discovered in the project, with their
	// associated tunnels and BGP sessions populated.
	Gateways []*VPNGatewayInfo

	// OrphanTunnels contains VPN tunnels that are not associated with any HA VPN gateway.
	// This typically includes legacy Classic VPN tunnels or tunnels with missing gateway links.
	OrphanTunnels []*VPNTunnelInfo

	// OrphanSessions contains BGP peers that could not be matched to any VPN tunnel.
	// This can occur when a router interface is not linked to a tunnel or when
	// the tunnel reference is invalid.
	OrphanSessions []*BGPSessionInfo
}

// VPNGatewayInfo represents an HA VPN gateway with its complete configuration and state.
// HA VPN gateways provide highly available VPN connectivity with 99.99% SLA when properly configured.
type VPNGatewayInfo struct {
	Name        string                                   // Gateway name
	Region      string                                   // GCP region where the gateway is deployed
	Network     string                                   // VPC network to which the gateway is attached
	Description string                                   // User-provided description
	SelfLink    string                                   // Full GCP resource URL
	Labels      map[string]string                        // Resource labels for organization and billing
	Interfaces  []*compute.VpnGatewayVpnGatewayInterface // Gateway interfaces (typically 2 for HA)
	Tunnels     []*VPNTunnelInfo                         // VPN tunnels attached to this gateway
}

// VPNTunnelInfo captures the complete state of a Cloud VPN tunnel including its
// IPSec configuration, status, and associated BGP sessions for dynamic routing.
type VPNTunnelInfo struct {
	TargetGatewayLink string
	Description       string
	Region            string
	SelfLink          string
	PeerIP            string
	PeerGateway       string
	LocalGatewayIP    string
	DetailedStatus    string
	Status            string
	RouterSelfLink    string
	SharedSecretHash  string
	LabelFingerprint  string
	RouterName        string
	RouterRegion      string
	Name              string
	GatewayLink       string
	BgpSessions       []*BGPSessionInfo
	GatewayInterface  int64
	IkeVersion        int64
}

// BGPSessionInfo represents a Cloud Router BGP peering session attached to a VPN tunnel.
// BGP sessions enable dynamic route exchange between GCP and the remote network.
type BGPSessionInfo struct {
	SessionState       string
	RouterName         string
	RouterSelfLink     string
	Region             string
	VpnTunnelLink      string
	Interface          string
	PeerIP             string
	LocalIP            string
	AdvertisedMode     string
	Name               string
	SessionStatus      string
	AdvertisedIPRanges []*compute.RouterAdvertisedIpRange
	AdvertisedGroups   []string
	AdvertisedPrefixes []string
	LearnedPrefixes    []string
	BestRoutePrefixes  []string // Subset of LearnedPrefixes that are selected as best routes
	RoutePriority      int64
	LocalASN           int64
	PeerASN            int64
	LearnedRoutes      int64
	AdvertisedCount    int
	Enabled            bool
}

type routerRef struct {
	Region string
	Name   string
}

var errAPIPageBreak = errors.New("page break")

// ListVPNOverview retrieves a comprehensive view of all Cloud VPN infrastructure in the project.
//
// This function performs the following operations:
//  1. Lists all HA VPN gateways across all regions
//  2. Lists all VPN tunnels (both HA and Classic VPN)
//  3. Lists all Cloud Router BGP peering sessions
//  4. Associates tunnels with their parent gateways
//  5. Attaches BGP sessions to their corresponding tunnels
//  6. Fetches advertised and learned BGP route prefixes for each session
//
// Resources that cannot be properly associated are collected as "orphans":
//   - OrphanTunnels: tunnels not linked to any HA VPN gateway (e.g., Classic VPN)
//   - OrphanSessions: BGP peers not linked to any tunnel
//
// The progress callback, if provided, is called periodically with status messages
// about the data collection process. It can be nil if progress tracking is not needed.
//
// Returns an error if any API calls fail or if the context is canceled.
func (c *Client) ListVPNOverview(ctx context.Context, progress func(string)) (*VPNOverview, error) {
	if progress == nil {
		progress = func(string) {}
	}

	progress("Loading HA VPN gateways")

	gateways, err := c.listVpnGateways(ctx, progress)
	if err != nil {
		return nil, err
	}

	progress("Loading Cloud VPN tunnels")

	tunnels, err := c.listVpnTunnels(ctx, progress)
	if err != nil {
		return nil, err
	}

	progress("Loading Cloud Router BGP peers")

	sessions, err := c.listRouterBgpPeers(ctx, progress)
	if err != nil {
		return nil, err
	}

	progress("Assembling Cloud VPN topology")

	overview := &VPNOverview{
		Gateways:       gateways,
		OrphanTunnels:  []*VPNTunnelInfo{},
		OrphanSessions: []*BGPSessionInfo{},
	}

	tunnelByLink := make(map[string]*VPNTunnelInfo, len(tunnels))

	for _, t := range tunnels {
		key := normalizeLink(t.SelfLink)
		if key != "" {
			tunnelByLink[key] = t
		}
	}

	// Attach BGP sessions to tunnels when possible.
	for _, peer := range sessions {
		link := normalizeLink(peer.VpnTunnelLink)
		if link == "" {
			overview.OrphanSessions = append(overview.OrphanSessions, peer)

			continue
		}

		if t, ok := tunnelByLink[link]; ok {
			t.BgpSessions = append(t.BgpSessions, peer)
		} else {
			overview.OrphanSessions = append(overview.OrphanSessions, peer)
		}
	}

	// Associate tunnels with gateways.
	attached := make(map[string]bool)

	for _, gw := range gateways {
		for _, tunnel := range tunnels {
			if sameResource(gw.SelfLink, tunnel.GatewayLink) {
				gw.Tunnels = append(gw.Tunnels, tunnel)
				attached[normalizeLink(tunnel.SelfLink)] = true
			}
		}
	}

	for _, tunnel := range tunnels {
		if attached[normalizeLink(tunnel.SelfLink)] {
			continue
		}

		overview.OrphanTunnels = append(overview.OrphanTunnels, tunnel)
	}

	// Populate BGP routes for all tunnels
	progress("Fetching BGP advertised and learned routes")

	allTunnels := make([]*VPNTunnelInfo, 0, len(tunnels))
	for _, gw := range gateways {
		allTunnels = append(allTunnels, gw.Tunnels...)
	}

	allTunnels = append(allTunnels, overview.OrphanTunnels...)

	if err := c.populateTunnelBGP(ctx, allTunnels, progress, true); err != nil {
		return nil, err
	}

	return overview, nil
}

// GetVPNGatewayOverview retrieves comprehensive details for a specific HA VPN gateway.
//
// This function fetches the gateway configuration and enriches it with:
//   - All VPN tunnels attached to the gateway
//   - BGP sessions configured on each tunnel
//   - Advertised and learned route prefixes for each BGP session
//   - Local gateway IP addresses for each tunnel
//
// The region parameter is optional. If provided, it performs a direct lookup in that
// region for better performance. If empty, the function searches across all regions
// using an aggregated list query.
//
// The progress callback, if provided, receives status messages during data collection.
//
// Returns an error if the gateway is not found, if API calls fail, or if the context
// is canceled.
func (c *Client) GetVPNGatewayOverview(ctx context.Context, region, name string, progress func(string)) (*VPNGatewayInfo, error) {
	if progress == nil {
		progress = func(string) {}
	}

	if region != "" {
		progress(fmt.Sprintf("Getting VPN gateway %s in %s", name, region))

		gw, err := c.service.VpnGateways.Get(c.project, region, name).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to get VPN gateway %s: %w", name, err)
		}

		info := convertVpnGateway(region, gw)
		if info == nil {
			return nil, fmt.Errorf("unable to convert VPN gateway %s", name)
		}

		if err := c.attachTunnelsForGateway(ctx, info, progress); err != nil {
			return nil, err
		}

		return info, nil
	}

	progress(fmt.Sprintf("Searching VPN gateway %s", name))
	call := c.service.VpnGateways.AggregatedList(c.project).
		Context(ctx).
		Filter(nameFilter(name))

	var result *VPNGatewayInfo

	err := call.Pages(ctx, func(page *compute.VpnGatewayAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.VpnGateways) == 0 {
				continue
			}

			region := scopeSuffix(scope)
			for _, gw := range scopedList.VpnGateways {
				info := convertVpnGateway(region, gw)
				if info == nil {
					continue
				}
				result = info

				return errAPIPageBreak
			}
		}

		return nil
	})
	if err != nil && !errors.Is(err, errAPIPageBreak) {
		logger.Log.Errorf("Failed to search VPN gateway %s: %v", name, err)

		return nil, fmt.Errorf("failed to search VPN gateway %s: %w", name, err)
	}

	if result == nil {
		return nil, fmt.Errorf("vpn gateway %s not found", name)
	}

	if err := c.attachTunnelsForGateway(ctx, result, progress); err != nil {
		return nil, err
	}

	return result, nil
}

// GetVPNTunnelOverview retrieves comprehensive details for a specific VPN tunnel.
//
// This function fetches the tunnel configuration and enriches it with:
//   - Local gateway IP address (derived from the parent gateway interface)
//   - All BGP sessions configured on the tunnel
//   - Advertised and learned route prefixes for each BGP session
//   - Router status and session state information
//
// The region parameter is optional. If provided, it performs a direct lookup in that
// region for better performance. If empty, the function searches across all regions
// using an aggregated list query.
//
// The progress callback, if provided, receives status messages during data collection.
//
// Returns an error if the tunnel is not found, if API calls fail, or if the context
// is canceled.
func (c *Client) GetVPNTunnelOverview(ctx context.Context, region, name string, progress func(string)) (*VPNTunnelInfo, error) {
	if progress == nil {
		progress = func(string) {}
	}

	var info *VPNTunnelInfo

	if region != "" {
		progress(fmt.Sprintf("Getting VPN tunnel %s in %s", name, region))

		tunnel, err := c.service.VpnTunnels.Get(c.project, region, name).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to get VPN tunnel %s: %w", name, err)
		}
		info = convertVpnTunnel(region, tunnel)
	} else {
		progress(fmt.Sprintf("Searching VPN tunnel %s", name))
		call := c.service.VpnTunnels.AggregatedList(c.project).
			Context(ctx).
			Filter(nameFilter(name))

		err := call.Pages(ctx, func(page *compute.VpnTunnelAggregatedList) error {
			for scope, scopedList := range page.Items {
				if len(scopedList.VpnTunnels) == 0 {
					continue
				}

				region := scopeSuffix(scope)
				for _, t := range scopedList.VpnTunnels {
					info = convertVpnTunnel(region, t)
					if info != nil {
						return errAPIPageBreak
					}
				}
			}

			return nil
		})
		if err != nil && !errors.Is(err, errAPIPageBreak) {
			return nil, fmt.Errorf("failed to search VPN tunnel %s: %w", name, err)
		}
	}

	if info == nil {
		return nil, fmt.Errorf("vpn tunnel %s not found", name)
	}

	if err := c.populateTunnelGatewayIP(ctx, []*VPNTunnelInfo{info}, progress); err != nil {
		return nil, err
	}

	if err := c.populateTunnelBGP(ctx, []*VPNTunnelInfo{info}, progress, true); err != nil {
		return nil, err
	}

	return info, nil
}

// listVpnGateways retrieves all HA VPN gateways across all regions using aggregated list.
func (c *Client) listVpnGateways(ctx context.Context, progress func(string)) ([]*VPNGatewayInfo, error) {
	logger.Log.Debug("Listing VPN gateways (aggregated)")

	var gateways []*VPNGatewayInfo

	if progress != nil {
		progress("Calling compute.v1 VpnGateways.AggregatedList")
	}

	call := c.service.VpnGateways.AggregatedList(c.project).Context(ctx)

	err := call.Pages(ctx, func(page *compute.VpnGatewayAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.VpnGateways) == 0 {
				continue
			}

			region := scopeSuffix(scope)
			if progress != nil {
				progress(fmt.Sprintf("Fetched HA VPN gateways in %s", region))
			}

			for _, gw := range scopedList.VpnGateways {
				if info := convertVpnGateway(region, gw); info != nil {
					gateways = append(gateways, info)
				}
			}
		}

		return nil
	})
	if err != nil {
		logger.Log.Errorf("Failed to list VPN gateways: %v", err)

		return nil, fmt.Errorf("failed to list VPN gateways: %w", err)
	}

	return gateways, nil
}

// listVpnTunnels retrieves all VPN tunnels (HA and Classic) across all regions using aggregated list.
func (c *Client) listVpnTunnels(ctx context.Context, progress func(string)) ([]*VPNTunnelInfo, error) {
	logger.Log.Debug("Listing VPN tunnels (aggregated)")

	var tunnels []*VPNTunnelInfo

	if progress != nil {
		progress("Calling compute.v1 VpnTunnels.AggregatedList")
	}

	call := c.service.VpnTunnels.AggregatedList(c.project).Context(ctx)

	err := call.Pages(ctx, func(page *compute.VpnTunnelAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.VpnTunnels) == 0 {
				continue
			}

			region := scopeSuffix(scope)
			if progress != nil {
				progress(fmt.Sprintf("Fetched VPN tunnels in %s", region))
			}

			for _, t := range scopedList.VpnTunnels {
				if info := convertVpnTunnel(region, t); info != nil {
					tunnels = append(tunnels, info)
				}
			}
		}

		return nil
	})
	if err != nil {
		logger.Log.Errorf("Failed to list VPN tunnels: %v", err)

		return nil, fmt.Errorf("failed to list VPN tunnels: %w", err)
	}

	return tunnels, nil
}

// listRouterBgpPeers retrieves all BGP peer configurations from Cloud Routers across all regions.
// This includes basic peer configuration but does NOT include advertised/learned route prefixes.
func (c *Client) listRouterBgpPeers(ctx context.Context, progress func(string)) ([]*BGPSessionInfo, error) {
	logger.Log.Debug("Listing Cloud Routers (aggregated) for BGP peers")

	var peers []*BGPSessionInfo

	if progress != nil {
		progress("Calling compute.v1 Routers.AggregatedList")
	}

	call := c.service.Routers.AggregatedList(c.project).Context(ctx)

	err := call.Pages(ctx, func(page *compute.RouterAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.Routers) == 0 {
				continue
			}

			region := scopeSuffix(scope)
			if progress != nil {
				progress(fmt.Sprintf("Fetched Cloud Routers in %s", region))
			}

			for _, router := range scopedList.Routers {
				if len(router.BgpPeers) == 0 {
					continue
				}

				interfaceToTunnel := map[string]string{}

				for _, iface := range router.Interfaces {
					if iface == nil || strings.TrimSpace(iface.Name) == "" {
						continue
					}

					if iface.LinkedVpnTunnel != "" {
						interfaceToTunnel[iface.Name] = iface.LinkedVpnTunnel
					}
				}

				statusMap := map[string]*compute.RouterStatusBgpPeerStatus{}

				if progress != nil {
					progress(fmt.Sprintf("Fetching router status for %s", router.Name))
				}

				statusResp, err := c.service.Routers.GetRouterStatus(c.project, region, router.Name).Context(ctx).Do()
				if err != nil {
					logger.Log.Warnf("Failed to fetch router status for %s: %v", router.Name, err)
				} else if statusResp != nil && statusResp.Result != nil {
					for _, peerStatus := range statusResp.Result.BgpPeerStatus {
						statusMap[peerStatus.Name] = peerStatus
					}
				}

				for _, peer := range router.BgpPeers {
					link := ""
					if candidate, ok := interfaceToTunnel[peer.InterfaceName]; ok {
						link = normalizeLink(candidate)
					}

					localASN := int64(0)
					if router.Bgp != nil {
						localASN = router.Bgp.Asn
					}

					peerInfo := &BGPSessionInfo{
						Name:               peer.Name,
						RouterName:         router.Name,
						RouterSelfLink:     router.SelfLink,
						Region:             region,
						VpnTunnelLink:      link,
						Interface:          peer.InterfaceName,
						PeerIP:             firstNonEmpty(peer.PeerIpAddress, peer.PeerIpv4NexthopAddress, peer.PeerIpv6NexthopAddress),
						LocalIP:            firstNonEmpty(peer.IpAddress, peer.Ipv4NexthopAddress, peer.Ipv6NexthopAddress),
						LocalASN:           localASN,
						PeerASN:            peer.PeerAsn,
						AdvertisedMode:     peer.AdvertiseMode,
						RoutePriority:      peer.AdvertisedRoutePriority,
						Enabled:            !strings.EqualFold(peer.Enable, "FALSE"),
						AdvertisedGroups:   append([]string{}, peer.AdvertisedGroups...),
						AdvertisedIPRanges: append([]*compute.RouterAdvertisedIpRange{}, peer.AdvertisedIpRanges...),
					}

					if status := statusMap[peer.Name]; status != nil {
						peerInfo.SessionStatus = status.Status
						peerInfo.SessionState = status.State
						peerInfo.LearnedRoutes = status.NumLearnedRoutes
						peerInfo.AdvertisedCount = len(status.AdvertisedRoutes)
					}

					peers = append(peers, peerInfo)
				}
			}
		}

		return nil
	})
	if err != nil {
		logger.Log.Errorf("Failed to list Cloud Router BGP peers: %v", err)

		return nil, fmt.Errorf("failed to list Cloud Router BGP peers: %w", err)
	}

	return peers, nil
}

// attachTunnelsForGateway finds and attaches all tunnels associated with a specific gateway.
// It also populates local gateway IPs and fetches BGP session details including route prefixes.
func (c *Client) attachTunnelsForGateway(ctx context.Context, gw *VPNGatewayInfo, progress func(string)) error {
	if gw == nil {
		return nil
	}

	gw.Tunnels = make([]*VPNTunnelInfo, 0)
	if progress != nil {
		progress(fmt.Sprintf("Listing tunnels in %s", gw.Region))
	}

	call := c.service.VpnTunnels.List(c.project, gw.Region).Context(ctx)

	err := call.Pages(ctx, func(list *compute.VpnTunnelList) error {
		for _, t := range list.Items {
			info := convertVpnTunnel(gw.Region, t)
			if info == nil {
				continue
			}

			if sameResource(info.GatewayLink, gw.SelfLink) || sameResource(info.TargetGatewayLink, gw.SelfLink) {
				if info.LocalGatewayIP == "" {
					if idx := info.GatewayInterface; idx >= 0 && int(idx) < len(gw.Interfaces) {
						info.LocalGatewayIP = strings.TrimSpace(gw.Interfaces[idx].IpAddress)
					}

					if info.LocalGatewayIP == "" && len(gw.Interfaces) == 1 {
						info.LocalGatewayIP = strings.TrimSpace(gw.Interfaces[0].IpAddress)
					}
				}

				gw.Tunnels = append(gw.Tunnels, info)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to list tunnels for gateway %s: %w", gw.Name, err)
	}

	if err := c.populateTunnelGatewayIP(ctx, gw.Tunnels, progress); err != nil {
		return err
	}

	return c.populateTunnelBGP(ctx, gw.Tunnels, progress, true)
}

// populateTunnelGatewayIP fills in the local gateway IP address for tunnels by fetching
// the parent gateway configuration and looking up the interface IP.
func (c *Client) populateTunnelGatewayIP(ctx context.Context, tunnels []*VPNTunnelInfo, progress func(string)) error {
	for _, tunnel := range tunnels {
		if tunnel == nil || strings.TrimSpace(tunnel.LocalGatewayIP) != "" {
			continue
		}

		region, name := gatewayReference(tunnel.GatewayLink, tunnel.Region)
		if name == "" {
			continue
		}

		if progress != nil {
			progress(fmt.Sprintf("Fetching gateway %s in %s", name, region))
		}

		gw, err := c.service.VpnGateways.Get(c.project, region, name).Context(ctx).Do()
		if err != nil {
			logger.Log.Warnf("Failed to fetch gateway %s: %v", name, err)

			continue
		}

		if idx := tunnel.GatewayInterface; idx >= 0 && int(idx) < len(gw.VpnInterfaces) {
			tunnel.LocalGatewayIP = strings.TrimSpace(gw.VpnInterfaces[idx].IpAddress)
		}

		if tunnel.LocalGatewayIP == "" && len(gw.VpnInterfaces) == 1 {
			tunnel.LocalGatewayIP = strings.TrimSpace(gw.VpnInterfaces[0].IpAddress)
		}
	}

	return nil
}

// populateTunnelBGP enriches tunnel information with BGP session details from Cloud Routers.
//
// This function performs the following:
//  1. Builds a map of tunnels by their self-link for efficient lookup
//  2. Identifies unique routers associated with the tunnels
//  3. Fetches router configuration and status for each unique router
//  4. Matches BGP peers to tunnels via router interface associations
//  5. Optionally fetches advertised and learned route prefixes if includeRoutes is true
//
// The includeRoutes parameter controls whether to fetch actual route prefixes. When false,
// only basic BGP session info (status, route counts) is populated. When true, the actual
// advertised and learned route prefixes are fetched from the router status API.
func (c *Client) populateTunnelBGP(ctx context.Context, tunnels []*VPNTunnelInfo, progress func(string), includeRoutes bool) error {
	if len(tunnels) == 0 {
		return nil
	}

	tunnelMap := make(map[string]*VPNTunnelInfo, len(tunnels))
	routerTargets := make(map[string]routerRef)

	for _, t := range tunnels {
		if t == nil {
			continue
		}

		key := normalizeLink(t.SelfLink)
		if key == "" {
			continue
		}

		t.BgpSessions = make([]*BGPSessionInfo, 0)
		tunnelMap[key] = t

		region, name := routerReference(t.RouterSelfLink, t.Region)
		if name != "" {
			routerTargets[region+"/"+name] = routerRef{Region: region, Name: name}
		}
	}

	if len(routerTargets) == 0 {
		return nil
	}

	for _, ref := range routerTargets {
		if progress != nil {
			progress(fmt.Sprintf("Fetching router %s in %s", ref.Name, ref.Region))
		}

		router, err := c.service.Routers.Get(c.project, ref.Region, ref.Name).Context(ctx).Do()
		if err != nil {
			logger.Log.Warnf("Failed to fetch router %s: %v", ref.Name, err)

			continue
		}

		statusResp, err := c.service.Routers.GetRouterStatus(c.project, ref.Region, ref.Name).Context(ctx).Do()
		if err != nil {
			logger.Log.Warnf("Failed to fetch router status for %s: %v", ref.Name, err)
		}

		statusMap := map[string]*compute.RouterStatusBgpPeerStatus{}

		if statusResp != nil && statusResp.Result != nil {
			for _, status := range statusResp.Result.BgpPeerStatus {
				statusMap[status.Name] = status
			}
		}

		// Collect ALL routes learned from each BGP peer using ListBgpRoutes API
		allLearnedByPeer := make(map[string][]string) // key: peer name, value: learned prefixes

		if includeRoutes {
			// Use ListBgpRoutes API to get all learned routes per BGP peer
			for _, peer := range router.BgpPeers {
				if peer == nil || strings.TrimSpace(peer.Name) == "" {
					continue
				}

				// Check which address families are enabled from the status
				peerStatus := statusMap[peer.Name]
				enabledFamilies := make([]string, 0, 2)

				if peerStatus != nil {
					if peerStatus.EnableIpv4 {
						enabledFamilies = append(enabledFamilies, "IPV4")
					}

					if peerStatus.EnableIpv6 {
						enabledFamilies = append(enabledFamilies, "IPV6")
					}
				}

				// Fallback: if no status or both disabled, try IPv4 by default
				if len(enabledFamilies) == 0 {
					enabledFamilies = append(enabledFamilies, "IPV4")
				}

				var learnedPrefixes []string

				// Fetch routes for each enabled address family
				for _, addressFamily := range enabledFamilies {
					if progress != nil {
						progress(fmt.Sprintf("Fetching %s routes for peer %s on router %s", addressFamily, peer.Name, ref.Name))
					}

					call := c.service.Routers.ListBgpRoutes(c.project, ref.Region, ref.Name).
						Context(ctx).
						Peer(peer.Name).
						RouteType("LEARNED").
						PolicyApplied(true).
						AddressFamily(addressFamily).
						PolicyApplied(false).
						MaxResults(500)

					err := call.Pages(ctx, func(page *compute.RoutersListBgpRoutes) error {
						for _, route := range page.Result {
							if route == nil || route.Destination == nil {
								continue
							}

							prefix := strings.TrimSpace(route.Destination.Prefix)
							if prefix != "" {
								learnedPrefixes = append(learnedPrefixes, prefix)
							}
						}

						return nil
					})
					if err != nil {
						// Only log as debug if it's an expected "not configured" error
						if strings.Contains(err.Error(), "address family") && strings.Contains(err.Error(), "is not configured") {
							logger.Log.Debugf("Address family %s not configured for peer %s on router %s", addressFamily, peer.Name, ref.Name)
						} else {
							logger.Log.Warnf("Failed to list %s BGP routes for peer %s on router %s: %v", addressFamily, peer.Name, ref.Name, err)
						}
					}
				}

				if len(learnedPrefixes) > 0 {
					allLearnedByPeer[peer.Name] = learnedPrefixes
				}
			}
		}

		// Also keep track of best routes separately
		bestByTunnel := make(map[string][]string)
		bestByPeerIP := make(map[string][]string)

		if includeRoutes && statusResp != nil && statusResp.Result != nil {
			for _, route := range statusResp.Result.BestRoutesForRouter {
				if route == nil || strings.TrimSpace(route.DestRange) == "" {
					continue
				}

				prefix := strings.TrimSpace(route.DestRange)
				if key := normalizeLink(route.NextHopVpnTunnel); key != "" {
					bestByTunnel[key] = append(bestByTunnel[key], prefix)

					continue
				}

				if ip := strings.TrimSpace(route.NextHopIp); ip != "" {
					bestByPeerIP[ip] = append(bestByPeerIP[ip], prefix)
				}
			}
		}

		interfaceToTunnel := map[string]string{}

		for _, iface := range router.Interfaces {
			if iface == nil || iface.LinkedVpnTunnel == "" {
				continue
			}
			interfaceToTunnel[iface.Name] = normalizeLink(iface.LinkedVpnTunnel)
		}

		for _, peer := range router.BgpPeers {
			link, ok := interfaceToTunnel[peer.InterfaceName]
			if !ok {
				continue
			}

			tunnel := tunnelMap[link]
			if tunnel == nil {
				continue
			}

			localASN := int64(0)
			if router.Bgp != nil {
				localASN = router.Bgp.Asn
			}

			info := &BGPSessionInfo{
				Name:               peer.Name,
				RouterName:         router.Name,
				RouterSelfLink:     router.SelfLink,
				Region:             ref.Region,
				VpnTunnelLink:      link,
				Interface:          peer.InterfaceName,
				PeerIP:             firstNonEmpty(peer.PeerIpAddress, peer.PeerIpv4NexthopAddress, peer.PeerIpv6NexthopAddress),
				LocalIP:            firstNonEmpty(peer.IpAddress, peer.Ipv4NexthopAddress, peer.Ipv6NexthopAddress),
				LocalASN:           localASN,
				PeerASN:            peer.PeerAsn,
				AdvertisedMode:     peer.AdvertiseMode,
				RoutePriority:      peer.AdvertisedRoutePriority,
				Enabled:            !strings.EqualFold(peer.Enable, "FALSE"),
				AdvertisedGroups:   append([]string{}, peer.AdvertisedGroups...),
				AdvertisedIPRanges: append([]*compute.RouterAdvertisedIpRange{}, peer.AdvertisedIpRanges...),
			}

			if status := statusMap[peer.Name]; status != nil {
				info.SessionStatus = status.Status
				info.SessionState = status.State
				info.LearnedRoutes = status.NumLearnedRoutes
				info.AdvertisedCount = len(status.AdvertisedRoutes)

				if includeRoutes {
					info.AdvertisedPrefixes = extractDestRanges(status.AdvertisedRoutes)
				}
			}

			if includeRoutes {
				// Populate learned prefixes from the peer-specific map
				if v := allLearnedByPeer[peer.Name]; len(v) > 0 {
					info.LearnedPrefixes = append([]string{}, v...)
				}

				// Populate best route prefixes
				if v := bestByTunnel[link]; len(v) > 0 {
					info.BestRoutePrefixes = append([]string{}, v...)
				} else if v := bestByPeerIP[info.PeerIP]; len(v) > 0 {
					info.BestRoutePrefixes = append([]string{}, v...)
				}
			}

			tunnel.BgpSessions = append(tunnel.BgpSessions, info)
		}
	}

	return nil
}

func sameResource(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}

	return normalizeLink(a) == normalizeLink(b)
}

func normalizeLink(link string) string {
	return strings.TrimSuffix(strings.TrimSpace(link), "/")
}

func convertVpnGateway(region string, gw *compute.VpnGateway) *VPNGatewayInfo {
	if gw == nil {
		return nil
	}

	return &VPNGatewayInfo{
		Name:        gw.Name,
		Region:      region,
		Network:     gw.Network,
		Description: gw.Description,
		SelfLink:    gw.SelfLink,
		Labels:      copyLabels(gw.Labels),
		Interfaces:  gw.VpnInterfaces,
		Tunnels:     []*VPNTunnelInfo{},
	}
}

func convertVpnTunnel(region string, t *compute.VpnTunnel) *VPNTunnelInfo {
	if t == nil {
		return nil
	}

	info := &VPNTunnelInfo{
		Name:              t.Name,
		Description:       t.Description,
		Region:            region,
		SelfLink:          t.SelfLink,
		PeerIP:            t.PeerIp,
		PeerGateway:       firstNonEmpty(t.PeerGcpGateway, t.PeerExternalGateway),
		Status:            t.Status,
		DetailedStatus:    t.DetailedStatus,
		IkeVersion:        t.IkeVersion,
		RouterSelfLink:    t.Router,
		RouterName:        resourceName(t.Router),
		RouterRegion:      regionFromResource(t.Router),
		TargetGatewayLink: t.TargetVpnGateway,
		GatewayLink:       firstNonEmpty(t.VpnGateway, t.TargetVpnGateway),
		GatewayInterface:  t.VpnGatewayInterface,
		LabelFingerprint:  t.LabelFingerprint,
		SharedSecretHash:  t.SharedSecretHash,
		BgpSessions:       []*BGPSessionInfo{},
	}

	if info.RouterRegion == "" {
		info.RouterRegion = region
	}

	return info
}

func scopeSuffix(scope string) string {
	if scope == "" {
		return ""
	}

	parts := strings.Split(scope, "/")
	if len(parts) == 1 {
		return parts[0]
	}

	return parts[len(parts)-1]
}

func resourceName(link string) string {
	if link == "" {
		return ""
	}

	parts := strings.Split(link, "/")
	if len(parts) == 0 {
		return link
	}

	return parts[len(parts)-1]
}

func regionFromResource(link string) string {
	if link == "" {
		return ""
	}

	for _, segment := range strings.Split(link, "/") {
		if strings.HasPrefix(segment, "regions/") {
			return strings.TrimPrefix(segment, "regions/")
		}
	}

	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}

	return ""
}

func copyLabels(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}

	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}

	return dst
}

func nameFilter(name string) string {
	escaped := strings.ReplaceAll(name, "\"", "\\\"")

	return fmt.Sprintf("name eq \"%s\"", escaped)
}

func gatewayReference(link, fallbackRegion string) (string, string) {
	if strings.TrimSpace(link) == "" {
		return fallbackRegion, ""
	}

	parts := strings.Split(link, "/")
	var region, name string

	for i := range len(parts) {
		switch parts[i] {
		case "regions":
			if i+1 < len(parts) {
				region = parts[i+1]
			}
		case "vpnGateways":
			if i+1 < len(parts) {
				name = parts[i+1]
			}
		}
	}

	if region == "" {
		region = fallbackRegion
	}

	return region, name
}

func extractDestRanges(routes []*compute.Route) []string {
	if len(routes) == 0 {
		return nil
	}

	result := make([]string, 0, len(routes))

	for _, route := range routes {
		if route == nil {
			continue
		}

		if dest := strings.TrimSpace(route.DestRange); dest != "" {
			result = append(result, dest)
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func routerReference(link, fallbackRegion string) (string, string) {
	if strings.TrimSpace(link) == "" {
		return fallbackRegion, ""
	}

	parts := strings.Split(link, "/")
	var region, name string

	for i := range len(parts) {
		switch parts[i] {
		case "regions":
			if i+1 < len(parts) {
				region = parts[i+1]
			}
		case "routers":
			if i+1 < len(parts) {
				name = parts[i+1]
			}
		}
	}

	if region == "" {
		region = fallbackRegion
	}

	return region, name
}
