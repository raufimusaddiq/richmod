package push

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/pushsupport"
)

func checkEndpoint(ctx context.Context, endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return pushsupport.ErrEndpointBlocked
	}
	_, err = pushsupport.ResolveAllowed(ctx, parsed.Hostname())
	return err
}

func guardedDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, pushsupport.ErrEndpointBlocked
	}
	addresses, err := pushsupport.ResolveAllowed(ctx, host)
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
		lastError = pushsupport.ErrEndpointBlocked
	}
	return nil, lastError
}
