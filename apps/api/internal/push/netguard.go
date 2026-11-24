package push

import (
	"context"
	"net/url"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/pushsupport"
)

func validateEndpoint(ctx context.Context, endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return pushsupport.ErrEndpointBlocked
	}
	_, err = pushsupport.ResolveAllowed(ctx, parsed.Hostname())
	return err
}
