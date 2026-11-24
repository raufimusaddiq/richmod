package pushsupport

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
)

var (
	ErrEndpointBlocked = errors.New("endpoint is not allowed")

	blockedIPv4Prefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
)

func ResolveAllowed(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, ErrEndpointBlocked
	}
	if addr, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		addr = addr.Unmap()
		if BlockedAddress(addr) {
			return nil, ErrEndpointBlocked
		}
		return []netip.Addr{addr}, nil
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrEndpointBlocked
	}
	allowed := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if BlockedAddress(address) {
			return nil, ErrEndpointBlocked
		}
		allowed = append(allowed, address)
	}
	return allowed, nil
}

func BlockedAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return true
	}
	if address.IsLoopback() || address.IsPrivate() || address.IsUnspecified() ||
		address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return true
	}
	if !address.Is4() {
		return false
	}
	for _, prefix := range blockedIPv4Prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
