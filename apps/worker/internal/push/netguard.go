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

var errEndpointBlocked = errors.New("endpoint is not allowed")

// checkEndpoint pins HTTPS and pre-resolves the host. The dialer below repeats
// the check on the address it actually connects to, so a DNS answer that
// changes after this call cannot reach a private service.
func checkEndpoint(ctx context.Context, endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errEndpointBlocked
	}
	_, err = resolveAllowed(ctx, parsed.Hostname())
	return err
}

// guardedDialContext resolves the name, rejects any blocked answer, and dials
// the already-checked IP rather than letting the transport resolve again.
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
	value := address.As4()
	switch {
	case value[0] == 0: // "this network" 0.0.0.0/8
		return true
	case value[0] == 100 && value[1] >= 64 && value[1] <= 127: // CGNAT 100.64.0.0/10
		return true
	case value[0] == 192 && value[1] == 0 && value[2] == 0: // IETF protocol assignments
		return true
	case value[0] == 198 && (value[1] == 18 || value[1] == 19): // benchmarking 198.18.0.0/15
		return true
	case value[0] >= 240: // reserved, including broadcast
		return true
	default:
		return false
	}
}
