package push

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var (
	errEndpointBlocked = errors.New("endpoint is not allowed")

	thisNetworkPrefix             = netip.MustParsePrefix("0.0.0.0/8")
	carrierGradeNATPrefix         = netip.MustParsePrefix("100.64.0.0/10")
	ietfProtocolAssignmentsPrefix = netip.MustParsePrefix("192.0.0.0/24")
	benchmarkingPrefix            = netip.MustParsePrefix("198.18.0.0/15")
	reservedIPv4Prefix            = netip.MustParsePrefix("240.0.0.0/4")

	blockedIPv4Prefixes = []netip.Prefix{
		thisNetworkPrefix,
		carrierGradeNATPrefix,
		ietfProtocolAssignmentsPrefix,
		benchmarkingPrefix,
		reservedIPv4Prefix,
	}
)

func checkEndpoint(ctx context.Context, endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errEndpointBlocked
	}
	_, err = resolveAllowed(ctx, parsed.Hostname())
	return err
}

func guardedDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errEndpointBlocked
	}
	addresses, err := resolveAllowed(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var lastError error
	for _, address := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if err == nil {
			return conn, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = errEndpointBlocked
	}
	return nil, lastError
}

func resolveAllowed(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, errEndpointBlocked
	}
	if address, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		address = address.Unmap()
		if blockedAddress(address) {
			return nil, errEndpointBlocked
		}
		return []netip.Addr{address}, nil
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
	for _, prefix := range blockedIPv4Prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
