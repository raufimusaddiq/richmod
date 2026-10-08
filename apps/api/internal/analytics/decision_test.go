package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func decisionRequest(h *Handler, p auth.Principal, method, query, payload, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/v1/analytics/cycle-decisions"+query, strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", id)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), p))
	w := httptest.NewRecorder()
	switch {
	case id != "":
		h.RevokeDecision(w, r)
	case method == http.MethodPost:
		h.CreateDecision(w, r)
	default:
		h.Decisions(w, r)
	}
	return w
}

func TestDecisionInputFailsBeforeDatabase(t *testing.T) {
	output := captureProductEvents(t)
	h := NewHandler(nil)
	p := auth.Principal{UserID: "u", HouseholdID: "h", HasHousehold: true}
	for _, body := range []string{`{}`, `{"cycleStart":"2026-08-01","body":" "}`, `{"cycleStart":"bad","body":"note"}`, `{"cycleStart":"2026-08-01","body":"note","householdId":"another"}`, `{"cycleStart":"2026-08-01","body":"note","authorUserId":"another"}`, `{"cycleStart":"2026-08-01","body":"note"} {}`, `{"cycleStart":"2026-08-01","body":"note\u0000"}`} {
		if w := decisionRequest(h, p, "POST", "", body, ""); w.Code != 400 {
			t.Fatalf("payload=%s status=%d", body, w.Code)
		}
	}
	oversize, _ := json.Marshal(decisionInput{"2026-08-01", strings.Repeat("字", 2001)})
	if w := decisionRequest(h, p, "POST", "", string(oversize), ""); w.Code != 400 {
		t.Fatalf("unicode overflow status=%d", w.Code)
	}
	oversize, _ = json.Marshal(decisionInput{"2026-08-01", strings.Repeat("a", 17000)})
	if w := decisionRequest(h, p, "POST", "", string(oversize), ""); w.Code != 400 {
		t.Fatalf("byte overflow status=%d", w.Code)
	}
	if w := decisionRequest(h, p, "GET", "?cycle_start=bad", "", ""); w.Code != 400 {
		t.Fatalf("date status=%d", w.Code)
	}
	if w := decisionRequest(h, p, "POST", "", "", "not-a-uuid"); w.Code != 400 {
		t.Fatalf("id status=%d", w.Code)
	}
	for _, method := range []string{"GET", "POST"} {
		if w := decisionRequest(h, auth.Principal{}, method, "?cycle_start=2026-08-01", `{}`, ""); w.Code != 403 {
			t.Fatalf("auth status=%d", w.Code)
		}
	}
	assertProductEvents(t, output, "CYCLE_DECISION_SAVED", 0)
}

func TestCycleDecisionsExplicitSaveAuditAndHouseholdIsolation(t *testing.T) {
	f, other := cycleReviewFixture(t), cycleReviewFixture(t)
	output := captureProductEvents(t)
	for _, fixture := range []reviewFixture{f, other} {
		for _, start := range []string{"2026-07-01", "2026-08-01", "2026-09-01"} {
			fixture.anchor(t, start)
		}
	}
	h := NewCycleHandler(f.pool, func() time.Time { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) })
	p := auth.Principal{UserID: f.user, HouseholdID: f.household, HasHousehold: true, HouseholdRole: "OWNER"}
	foreign := auth.Principal{UserID: other.user, HouseholdID: other.household, HasHousehold: true, HouseholdRole: "OWNER"}
	ctx := context.Background()
	var ledgerBefore, wealthBefore int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.household).Scan(&ledgerBefore); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM wealth_snapshot WHERE household_id=$1`, f.household).Scan(&wealthBefore); err != nil {
		t.Fatal(err)
	}
	create := func(start, body string) cycleDecision {
		t.Helper()
		payload, _ := json.Marshal(decisionInput{start, body})
		w := decisionRequest(h, p, "POST", "", string(payload), "")
		if w.Code != 201 {
			t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
		}
		var d cycleDecision
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		if d.AuthorID != f.user || d.CycleStart != start || d.CreatedAt.IsZero() {
			t.Fatalf("author/binding=%+v", d)
		}
		return d
	}
	prior := create("2026-07-01", "Keep more cash liquid.")
	current := create("2026-08-01", "  Catatan keluarga\nBukan transaksi.  ")
	if current.Body != "Catatan keluarga\nBukan transaksi." {
		t.Fatalf("text=%q", current.Body)
	}
	var audit, ledgerAfter, wealthAfter int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_id=$2 AND actor_id=$3 AND action='CYCLE_DECISION_CREATE'`, f.household, current.ID, f.user).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if audit != 1 {
		t.Fatalf("audit=%d", audit)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, f.household).Scan(&ledgerAfter); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM wealth_snapshot WHERE household_id=$1`, f.household).Scan(&wealthAfter); err != nil {
		t.Fatal(err)
	}
	if ledgerBefore != ledgerAfter || wealthBefore != wealthAfter {
		t.Fatal("decision changed financial row counts")
	}
	get := func(principal auth.Principal, start string) decisionList {
		t.Helper()
		w := decisionRequest(h, principal, "GET", "?cycle_start="+start, "", "")
		if w.Code != 200 {
			t.Fatalf("read status=%d body=%s", w.Code, w.Body.String())
		}
		var result decisionList
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	listed := get(p, "2026-08-01")
	if len(listed.Items) != 1 || listed.Items[0].ID != current.ID || len(listed.Previous) != 1 || listed.Previous[0].ID != prior.ID {
		t.Fatalf("list=%+v", listed)
	}
	if listed.PreviousCycleStart == nil || *listed.PreviousCycleStart != "2026-07-01" {
		t.Fatalf("previous=%+v", listed)
	}
	if list := get(foreign, "2026-08-01"); len(list.Items)+len(list.Previous) != 0 {
		t.Fatalf("cross-household read=%+v", list)
	}
	if w := decisionRequest(h, foreign, "POST", "", "", current.ID); w.Code != 404 {
		t.Fatalf("cross-household revoke=%d", w.Code)
	}
	for _, item := range []struct {
		start  string
		status int
	}{{"2026-09-01", 409}, {"2026-08-02", 404}} {
		payload, _ := json.Marshal(decisionInput{item.start, "Must not be saved"})
		if w := decisionRequest(h, p, "POST", "", string(payload), ""); w.Code != item.status {
			t.Fatalf("cycle %s status=%d body=%s", item.start, w.Code, w.Body.String())
		}
	}
	if w := decisionRequest(h, p, "POST", "", "", current.ID); w.Code != 200 {
		t.Fatalf("revoke=%d body=%s", w.Code, w.Body.String())
	}
	if w := decisionRequest(h, p, "POST", "", "", current.ID); w.Code != 404 {
		t.Fatalf("repeat revoke=%d", w.Code)
	}
	if list := get(p, "2026-08-01"); len(list.Items) != 0 || len(list.Previous) != 1 {
		t.Fatalf("revoked list=%+v", list)
	}
	var retained bool
	if err := f.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL AND body=$2 FROM cycle_decision WHERE id=$1`, current.ID, current.Body).Scan(&retained); err != nil || !retained {
		t.Fatalf("retention=%t err=%v", retained, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='CYCLE_DECISION_REVOKE'`, current.ID).Scan(&audit); err != nil || audit != 1 {
		t.Fatalf("revoke audit=%d err=%v", audit, err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE household_member SET active=false WHERE household_id=$1 AND user_id=$2`, f.household, f.user); err != nil {
		t.Fatal(err)
	}
	if w := decisionRequest(h, p, "POST", "", `{"cycleStart":"2026-08-01","body":"Stale membership"}`, ""); w.Code != 404 {
		t.Fatalf("inactive membership=%d body=%s", w.Code, w.Body.String())
	}
	assertProductEvents(t, output, "CYCLE_DECISION_SAVED", 2)
}
