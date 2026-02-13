package gcp

import (
	"context"
	"net"
	"strings"

	"github.com/kedare/compass/internal/logger"
)

// IPLookupStrategy defines how to balance speed vs completeness in IP lookups
type IPLookupStrategy int

const (
	// StrategyFastOnly uses only cache-optimized search (fastest, may miss results if cache stale)
	StrategyFastOnly IPLookupStrategy = iota

	// StrategySmartFallback uses fast search first, falls back to full scan if nothing found (recommended)
	StrategySmartFallback

	// StrategyAlwaysComplete always does full scan (slowest, most thorough)
	StrategyAlwaysComplete
)

// LookupIPAddressWithStrategy performs IP lookup using the specified strategy
func (c *Client) LookupIPAddressWithStrategy(ctx context.Context, ip string, strategy IPLookupStrategy) ([]IPAssociation, error) {
	target := strings.TrimSpace(ip)
	if target == "" {
		return nil, nil
	}

	targetIP := net.ParseIP(target)
	if targetIP == nil {
		return nil, nil
	}

	switch strategy {
	case StrategyFastOnly:
		return c.lookupFastOnly(ctx, ip, targetIP)

	case StrategySmartFallback:
		return c.lookupSmartFallback(ctx, ip, targetIP)

	case StrategyAlwaysComplete:
		return c.LookupIPAddress(ctx, ip)

	default:
		return c.lookupSmartFallback(ctx, ip, targetIP)
	}
}

// lookupFastOnly uses only cache-optimized search
func (c *Client) lookupFastOnly(ctx context.Context, ip string, targetIP net.IP) ([]IPAssociation, error) {
	// For private IPs, try cache-optimized search
	if targetIP.IsPrivate() && c.cache != nil {
		hints := c.getSubnetHintsFromCache(targetIP)
		if len(hints) > 0 {
			logger.Log.Debugf("[%s] Fast-only strategy: found %d subnet hints", c.project, len(hints))
			return c.lookupWithHints(ctx, targetIP, targetIP.String(), hints)
		}
		logger.Log.Debugf("[%s] Fast-only strategy: no cache hints, returning empty", c.project)
		return nil, nil
	}

	// For public IPs, must do full scan
	logger.Log.Debugf("[%s] Public IP, using full scan", c.project)
	return c.LookupIPAddress(ctx, ip)
}

// lookupSmartFallback tries fast first, falls back to full scan if nothing found
func (c *Client) lookupSmartFallback(ctx context.Context, ip string, targetIP net.IP) ([]IPAssociation, error) {
	// For private IPs with cache hints, try optimized search first
	if targetIP.IsPrivate() && c.cache != nil {
		hints := c.getSubnetHintsFromCache(targetIP)
		if len(hints) > 0 {
			logger.Log.Debugf("[%s] Smart fallback: trying fast search with %d hints", c.project, len(hints))
			results, err := c.lookupWithHints(ctx, targetIP, targetIP.String(), hints)
			if err != nil {
				return nil, err
			}

			if len(results) > 0 {
				logger.Log.Debugf("[%s] Smart fallback: found %d results, skipping full scan", c.project, len(results))
				return results, nil
			}

			// No results from fast search - fall back to full scan
			logger.Log.Debugf("[%s] Smart fallback: no results from fast search, trying full scan", c.project)
			return c.LookupIPAddress(ctx, ip)
		}
	}

	// No hints available or public IP - use standard lookup
	logger.Log.Debugf("[%s] No cache hints available, using full scan", c.project)
	return c.LookupIPAddress(ctx, ip)
}

// DefaultStrategy returns the recommended strategy for most use cases
func DefaultStrategy() IPLookupStrategy {
	return StrategySmartFallback
}
