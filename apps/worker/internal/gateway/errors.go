package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var ErrNotConfigured = errors.New("LLM gateway is not configured")

type UnavailableError struct{ Err error }

func (e *UnavailableError) Error() string { return "LLM gateway unavailable: " + e.Err.Error() }
func (e *UnavailableError) Unwrap() error { return e.Err }

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

func IsRejected(err error) bool {
	var status *StatusError
	if !errors.As(err, &status) {
		return false
	}
	code := status.StatusCode
	return code >= 400 && code < 500 && code != http.StatusRequestTimeout && code != http.StatusTooManyRequests
}

func transportError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return &UnavailableError{Err: fmt.Errorf("%s: %w", operation, err)}
}
