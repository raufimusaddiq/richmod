package push

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

var errEndpointBlocked = errors.New("endpoint is not allowed")

func validateEndpoint(ctx context.Context, endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errEndpointBlocked
	}
	_, err = resolveAllowed(ctx, parsed.Hostname())
	return err
}

func resolveAllowed(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, errEndpointBlocked
	}
	if addr, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		addr = addr.Unmap()
		if blockedAddress(addr) {
			return nil, errEndpointBlocked
		}
		return []netip.Addr{addr}, nil
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errEndpointBlocked
	}
	allowed := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if blockedAddress(address) {
			return nil, errEndpointBlocked
		}
		allowed = append(allowed, address)
	}
	return allowed, nil
}

func blockedAddress(address netip.Addr) bool {
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
	value := address.As4()
	switch {
	case value[0] == 0:
		return true
	case value[0] == 100 && value[1] >= 64 && value[1] <= 127:
		return true
	case value[0] == 192 && value[1] == 0 && value[2] == 0:
		return true
	case value[0] == 198 && (value[1] == 18 || value[1] == 19):
		return true
	case value[0] >= 240:
		return true
	default:
		return false
	}
}
