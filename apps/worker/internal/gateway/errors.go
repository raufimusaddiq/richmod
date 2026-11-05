package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Gateway failures fall into three classes callers act on differently:
//
//   - unavailable: the gateway could not be reached, is not configured, or said
//     it is overloaded or down (transport error, HTTP 408/429/5xx). Retrying soon
//     may help, but a user-facing flow must not wait long for it, and a
//     deterministic fallback should take over.
//   - rejected: the gateway answered with another 4xx. The same request will be
//     rejected again, so it is permanent.
//   - anything else (a decode error, a context deadline) keeps its own meaning.
//
// The classification keys on typed values, never on error text.

// ErrNotConfigured reports a client without base URL, key, or model.
var ErrNotConfigured = errors.New("LLM gateway is not configured")

// UnavailableError marks a gateway that could not be reached.
type UnavailableError struct{ Err error }

func (e *UnavailableError) Error() string { return "LLM gateway unavailable: " + e.Err.Error() }
func (e *UnavailableError) Unwrap() error { return e.Err }

// StatusError is a non-2xx HTTP answer from the gateway.
type StatusError struct {
	StatusCode int
	Endpoint   string
}

func (e *StatusError) Error() string {
	if e.Endpoint == "chat completion" {
		return fmt.Sprintf("LLM chat completion returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("LLM gateway returned HTTP %d", e.StatusCode)
}

// IsUnavailable reports whether err means the gateway is unreachable, not
// configured, or temporarily refusing work.
func IsUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var unavailable *UnavailableError
	if errors.As(err, &unavailable) {
		return true
	}
	if errors.Is(err, ErrNotConfigured) {
		return true
	}
	var status *StatusError
	if errors.As(err, &status) {
		code := status.StatusCode
		return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
	}
	return false
}

// IsRejected reports whether the gateway refused the request itself (HTTP 4xx
// other than 408/429). Repeating the same request will fail the same way.
func IsRejected(err error) bool {
	var status *StatusError
	if !errors.As(err, &status) {
		return false
	}
	code := status.StatusCode
	return code >= 400 && code < 500 && code != http.StatusRequestTimeout && code != http.StatusTooManyRequests
}

// transportError wraps an http.Client.Do failure. A cancelled or expired
// caller context keeps its own meaning (the caller's budget ran out); every
// other transport failure means the gateway could not be reached.
func transportError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return &UnavailableError{Err: fmt.Errorf("%s: %w", operation, err)}
}
