package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestStatusRequiresAuthentication(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler(nil).Status(response, httptest.NewRequest(http.MethodGet, "/api/v1/operations/status", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d", response.Code)
	}
}

// slowQueryer never answers within the probe budget, standing in for a slow or
// unreachable dependency.
type slowQueryer struct{ delay time.Duration }

func (s slowQueryer) QueryRow(ctx context.Context, sql string, args ...any) rowScanner {
	return slowRow{ctx: ctx, delay: s.delay}
}

func (s slowQueryer) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	return &slowRows{ctx: ctx, delay: s.delay}, nil
}

type slowRow struct {
	ctx   context.Context
	delay time.Duration
}

func (r slowRow) Scan(dest ...any) error {
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	case <-time.After(r.delay):
		return nil
	}
}

type slowRows struct {
	ctx   context.Context
	delay time.Duration
	done  bool
}

func (r *slowRows) Next() bool {
	if r.done {
		return false
	}
	select {
	case <-r.ctx.Done():
		r.done = true
		return false
	case <-time.After(r.delay):
		r.done = true
		return false
	}
}

func (r *slowRows) Scan(dest ...any) error { return nil }
func (r *slowRows) Err() error             { return r.ctx.Err() }
func (r *slowRows) Close()                 {}

// TestStatusDegradesWithinBudgetWhenDependencyIsSlow: a dependency that never
// answers must not hold the response past its budget; the endpoint degrades to
// a status value instead of blocking.
func TestStatusDegradesWithinBudgetWhenDependencyIsSlow(t *testing.T) {
	handler := &Handler{pool: slowQueryer{delay: time.Second}, probeSlots: make(chan struct{}, 32)}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/status", nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{
		UserID:        "00000000-0000-4000-8000-000000000001",
		HasHousehold:  true,
		HouseholdID:   "00000000-0000-4000-8000-000000000002",
		HouseholdRole: "OWNER",
	}))
	response := httptest.NewRecorder()

	start := time.Now()
	handler.Status(response, request)
	elapsed := time.Since(start)

	if elapsed > statusBudget {
		t.Fatalf("status took %v, budget %v", elapsed, statusBudget)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", body.Status)
	}
}
