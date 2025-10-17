package gcp

import (
	"context"
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
	PeerASN            int64
	AdvertisedMode     string
	RoutePriority      int64
	Enabled            bool
	AdvertisedGroups   []string
	AdvertisedIPRanges []*compute.RouterAdvertisedIpRange
}

// ListVPNOverview retrieves HA VPN gateways along with associated tunnels and BGP peers.
func (c *Client) ListVPNOverview(ctx context.Context) (*VPNOverview, error) {
	gateways, err := c.listVpnGateways(ctx)
	if err != nil {
		return nil, err
	}

	tunnels, err := c.listVpnTunnels(ctx)
	if err != nil {
		return nil, err
	}

	sessions, err := c.listRouterBgpPeers(ctx)
	if err != nil {
		return nil, err
	}

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

func (c *Client) listVpnGateways(ctx context.Context) ([]*VPNGatewayInfo, error) {
	logger.Log.Debug("Listing VPN gateways (aggregated)")

	var gateways []*VPNGatewayInfo

	call := c.service.VpnGateways.AggregatedList(c.project).Context(ctx)
	err := call.Pages(ctx, func(page *compute.VpnGatewayAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.VpnGateways) == 0 {
				continue
			}

			region := scopeSuffix(scope)
			for _, gw := range scopedList.VpnGateways {
				info := &VPNGatewayInfo{
					Name:        gw.Name,
					Region:      region,
					Network:     gw.Network,
					Description: gw.Description,
					SelfLink:    gw.SelfLink,
					Labels:      gw.Labels,
					Interfaces:  gw.VpnInterfaces,
					Tunnels:     []*VPNTunnelInfo{},
				}
				gateways = append(gateways, info)
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

func (c *Client) listVpnTunnels(ctx context.Context) ([]*VPNTunnelInfo, error) {
	logger.Log.Debug("Listing VPN tunnels (aggregated)")

	var tunnels []*VPNTunnelInfo

	call := c.service.VpnTunnels.AggregatedList(c.project).Context(ctx)
	err := call.Pages(ctx, func(page *compute.VpnTunnelAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.VpnTunnels) == 0 {
				continue
			}

			region := scopeSuffix(scope)
			for _, t := range scopedList.VpnTunnels {
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
				tunnels = append(tunnels, info)
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

func (c *Client) listRouterBgpPeers(ctx context.Context) ([]*BGPSessionInfo, error) {
	logger.Log.Debug("Listing Cloud Routers (aggregated) for BGP peers")

	var peers []*BGPSessionInfo

	call := c.service.Routers.AggregatedList(c.project).Context(ctx)
	err := call.Pages(ctx, func(page *compute.RouterAggregatedList) error {
		for scope, scopedList := range page.Items {
			if len(scopedList.Routers) == 0 {
				continue
			}

			region := scopeSuffix(scope)
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
						PeerASN:            peer.PeerAsn,
						AdvertisedMode:     peer.AdvertiseMode,
						RoutePriority:      peer.AdvertisedRoutePriority,
						Enabled:            !strings.EqualFold(peer.Enable, "FALSE"),
						AdvertisedGroups:   append([]string{}, peer.AdvertisedGroups...),
						AdvertisedIPRanges: append([]*compute.RouterAdvertisedIpRange{}, peer.AdvertisedIpRanges...),
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

func sameResource(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}

	return normalizeLink(a) == normalizeLink(b)
}

func normalizeLink(link string) string {
	return strings.TrimSuffix(strings.TrimSpace(link), "/")
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
