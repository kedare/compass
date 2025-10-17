package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"compass/internal/logger"
	"google.golang.org/api/compute/v1"
)

// VPNOverview groups HA VPN gateways, tunnels, and BGP peers for display.
type VPNOverview struct {
	Gateways       []*VPNGatewayInfo
	OrphanTunnels  []*VPNTunnelInfo
	OrphanSessions []*BGPSessionInfo
}

// VPNGatewayInfo represents an HA VPN gateway and associated resources.
type VPNGatewayInfo struct {
	Name        string
	Region      string
	Network     string
	Description string
	SelfLink    string
	Labels      map[string]string
	Interfaces  []*compute.VpnGatewayVpnGatewayInterface
	Tunnels     []*VPNTunnelInfo
}

// VPNTunnelInfo captures core tunnel metadata and associated BGP sessions.
type VPNTunnelInfo struct {
	Name              string
	Description       string
	Region            string
	SelfLink          string
	PeerIP            string
	PeerGateway       string
	PeerExternal      string
	LocalGatewayIP    string
	Status            string
	DetailedStatus    string
	IkeVersion        int64
	SharedSecretHash  string
	RouterSelfLink    string
	RouterName        string
	RouterRegion      string
	TargetGatewayLink string
	GatewayLink       string
	GatewayInterface  int64
	LabelFingerprint  string
	BgpSessions       []*BGPSessionInfo
}

// BGPSessionInfo represents a Cloud Router BGP peer attached to a VPN tunnel.
type BGPSessionInfo struct {
	Name               string
	RouterName         string
	RouterSelfLink     string
	Region             string
	VpnTunnelLink      string
	Interface          string
	PeerIP             string
	LocalIP            string
	PeerASN            int64
	AdvertisedMode     string
	RoutePriority      int64
	Enabled            bool
	AdvertisedGroups   []string
	AdvertisedIPRanges []*compute.RouterAdvertisedIpRange
	SessionStatus      string
	SessionState       string
	LearnedRoutes      int64
	AdvertisedCount    int
}

type routerRef struct {
	Region string
	Name   string
}

var apiPageBreak = errors.New("page break")

// ListVPNOverview retrieves HA VPN gateways along with associated tunnels and BGP peers.
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

	return overview, nil
}

// GetVPNGatewayOverview retrieves details for a specific HA VPN gateway.
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
				return apiPageBreak
			}
		}

		return nil
	})
	if err != nil && !errors.Is(err, apiPageBreak) {
		logger.Log.Errorf("Failed to search VPN gateway %s: %v", name, err)
		return nil, fmt.Errorf("failed to search VPN gateway %s: %w", name, err)
	}
	if errors.Is(err, apiPageBreak) {
		err = nil
	}

	if result == nil {
		return nil, fmt.Errorf("vpn gateway %s not found", name)
	}

	if err := c.attachTunnelsForGateway(ctx, result, progress); err != nil {
		return nil, err
	}

	return result, nil
}

// GetVPNTunnelOverview retrieves details for a specific VPN tunnel.
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
						return apiPageBreak
					}
				}
			}

			return nil
		})
		if err != nil && !errors.Is(err, apiPageBreak) {
			return nil, fmt.Errorf("failed to search VPN tunnel %s: %w", name, err)
		}
		if errors.Is(err, apiPageBreak) {
			err = nil
		}
	}

	if info == nil {
		return nil, fmt.Errorf("vpn tunnel %s not found", name)
	}

	if err := c.populateTunnelGatewayIP(ctx, []*VPNTunnelInfo{info}, progress); err != nil {
		return nil, err
	}

	if err := c.populateTunnelBGP(ctx, []*VPNTunnelInfo{info}, progress); err != nil {
		return nil, err
	}

	return info, nil
}

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
					peerInfo := &BGPSessionInfo{
						Name:               peer.Name,
						RouterName:         router.Name,
						RouterSelfLink:     router.SelfLink,
						Region:             region,
						VpnTunnelLink:      link,
						Interface:          peer.InterfaceName,
						PeerIP:             firstNonEmpty(peer.PeerIpAddress, peer.PeerIpv4NexthopAddress, peer.PeerIpv6NexthopAddress),
						LocalIP:            firstNonEmpty(peer.IpAddress, peer.Ipv4NexthopAddress, peer.Ipv6NexthopAddress),
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

	return c.populateTunnelBGP(ctx, gw.Tunnels, progress)
}

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

func (c *Client) populateTunnelBGP(ctx context.Context, tunnels []*VPNTunnelInfo, progress func(string)) error {
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

			info := &BGPSessionInfo{
				Name:               peer.Name,
				RouterName:         router.Name,
				RouterSelfLink:     router.SelfLink,
				Region:             ref.Region,
				VpnTunnelLink:      link,
				Interface:          peer.InterfaceName,
				PeerIP:             firstNonEmpty(peer.PeerIpAddress, peer.PeerIpv4NexthopAddress, peer.PeerIpv6NexthopAddress),
				LocalIP:            firstNonEmpty(peer.IpAddress, peer.Ipv4NexthopAddress, peer.Ipv6NexthopAddress),
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
		PeerExternal:      t.PeerExternalGateway,
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

	for i := 0; i < len(parts); i++ {
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

func routerReference(link, fallbackRegion string) (string, string) {
	if strings.TrimSpace(link) == "" {
		return fallbackRegion, ""
	}

	parts := strings.Split(link, "/")
	var region, name string
	for i := 0; i < len(parts); i++ {
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
