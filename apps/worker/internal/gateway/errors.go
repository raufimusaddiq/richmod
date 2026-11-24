package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("LLM gateway is not configured")

type UnavailableError struct{ Err error }

func (e *UnavailableError) Error() string { return "LLM gateway unavailable: " + e.Err.Error() }
func (e *UnavailableError) Unwrap() error { return e.Err }

type StatusError struct {
	StatusCode int
	Endpoint   string
	Backoff    time.Duration
}

func (e *StatusError) RetryAfter() time.Duration { return e.Backoff }

func ParseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(min(seconds, maxRetryAfterSeconds)) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if wait := at.Sub(now); wait > 0 {
			return min(wait, maxRetryAfterSeconds*time.Second)
		}
	}
	return 0
}

const maxRetryAfterSeconds = 24 * 60 * 60

const errorBodyDrainLimit = 4 << 10

func statusFailure(resp *http.Response, endpoint string) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errorBodyDrainLimit))
	return &StatusError{StatusCode: resp.StatusCode, Endpoint: endpoint, Backoff: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
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
