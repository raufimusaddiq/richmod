package uitelemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/platform/httpmw"
)

type call struct{ household, screen, action string }

func post(t *testing.T, body string, principal *auth.Principal) (int, []call) {
	t.Helper()
	var calls []call
	h := &Handler{record: func(_ context.Context, household, screen, action string) error {
		calls = append(calls, call{household, screen, action})
		return nil
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/events", strings.NewReader(body))
	if principal != nil {
		req = req.WithContext(auth.ContextWithPrincipal(req.Context(), *principal))
	}
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	return rec.Code, calls
}

func member(household string) *auth.Principal {
	return &auth.Principal{UserID: "u1", HouseholdID: household, HasHousehold: true}
}

func TestAllowListedEventIsStoredForTheAuthenticatedHouseholdOnly(t *testing.T) {
	code, calls := post(t, `{"screen":"analytics","action":"SCREEN_VIEW"}`, member("house-a"))
	if code != http.StatusNoContent || len(calls) != 1 || calls[0] != (call{"house-a", "analytics", "SCREEN_VIEW"}) {
		t.Fatalf("code=%d calls=%+v", code, calls)
	}
}

func TestEventWithoutHouseholdMembershipIsForbidden(t *testing.T) {
	for _, p := range []*auth.Principal{nil, {UserID: "u1"}} {
		if code, calls := post(t, `{"screen":"home","action":"SCREEN_VIEW"}`, p); code != http.StatusForbidden || len(calls) != 0 {
			t.Fatalf("code=%d calls=%d", code, len(calls))
		}
	}
}

func TestUnknownOrUnboundedPayloadsAreRejected(t *testing.T) {
	for name, body := range map[string]string{
		"unknown screen":         `{"screen":"secrets","action":"SCREEN_VIEW"}`,
		"unknown action":         `{"screen":"home","action":"EXFILTRATE"}`,
		"lowercase action":       `{"screen":"home","action":"screen_view"}`,
		"path instead of key":    `{"screen":"/transactions/123","action":"SCREEN_VIEW"}`,
		"amount field":           `{"screen":"home","action":"SCREEN_VIEW","amount":"9923834"}`,
		"free text field":        `{"screen":"home","action":"SCREEN_VIEW","note":"Orang Tua"}`,
		"household in payload":   `{"screen":"home","action":"SCREEN_VIEW","householdId":"house-b"}`,
		"missing action":         `{"screen":"home"}`,
		"empty body":             ``,
		"not json":               `screen=home`,
		"trailing data":          `{"screen":"home","action":"SCREEN_VIEW"}{"screen":"home","action":"SCREEN_VIEW"}`,
		"oversized":              `{"screen":"home","action":"SCREEN_VIEW","pad":"` + strings.Repeat("x", 2048) + `"}`,
		"number for screen":      `{"screen":1,"action":"SCREEN_VIEW"}`,
		"screen with whitespace": `{"screen":" home","action":"SCREEN_VIEW"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if code, calls := post(t, body, member("house-a")); code != http.StatusBadRequest || len(calls) != 0 {
				t.Fatalf("code=%d calls=%d", code, len(calls))
			}
		})
	}
}

func TestTelemetryRouteIsRateLimitedPerClient(t *testing.T) {
	h := &Handler{record: func(context.Context, string, string, string) error { return nil }}
	limited := httpmw.NewLimiter(RateLimitPerMinute, time.Minute).Handler(http.HandlerFunc(h.Create))
	send := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/events", strings.NewReader(`{"screen":"home","action":"SCREEN_VIEW"}`))
		req.RemoteAddr = "192.0.2.7:4000"
		req = req.WithContext(auth.ContextWithPrincipal(req.Context(), *member("house-a")))
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < RateLimitPerMinute; i++ {
		if code := send(); code != http.StatusNoContent {
			t.Fatalf("request %d: %d", i+1, code)
		}
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("over-limit request got %d", code)
	}
}
