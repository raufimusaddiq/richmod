package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestReviewOpsAdminAggregatesAndRedaction(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	mustExec := func(_ pgconn.CommandTag, err error) { must(err) }
	stamp := time.Now().UnixNano()
	chatID := stamp
	var adminID, userID, householdID, sourceID, openItem, resolvedItem, openRequest, resolvedRequest string
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,is_super_admin) VALUES($1,'Admin','!',true) RETURNING id`, fmt.Sprintf("admin-review-ops-%d@example.test", stamp)).Scan(&adminID))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Review ops %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','!') RETURNING id`, fmt.Sprintf("owner-review-ops-%d@example.test", stamp)).Scan(&userID))
	mustExec(pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID))
	mustExec(pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("ro-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, householdID, sourceID).Scan(&openItem))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,resolved_by_user_id) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),$3) RETURNING id`, householdID, sourceID, userID).Scan(&resolvedItem))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, openItem, householdID).Scan(&openRequest))
	mustExec(pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,99)`, openRequest, chatID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status,resolved_at) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now()) RETURNING id`, resolvedItem, householdID).Scan(&resolvedRequest))
	// A delivered card's send job should count as a delivery attempt.
	mustExec(pool.Exec(ctx, `INSERT INTO job(type,payload_json,status) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'review_request_id',$2::text),'SUCCEEDED')`, chatID, openRequest))

	handler := NewHandler(pool, false, "responses")
	call := func(fn http.HandlerFunc, target string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: adminID, IsSuperAdmin: true}))
		response := httptest.NewRecorder()
		fn(response, request)
		return response
	}

	var summary struct {
		OpenReviews                    int      `json:"openReviews"`
		EligibleTelegramReviews        int      `json:"eligibleTelegramReviews"`
		ActionableTelegramProjections  int      `json:"actionableTelegramProjections"`
		TelegramActionableCoverageRate *float64 `json:"telegramActionableCoverageRate"`
		DeliveryAttempts               int      `json:"deliveryAttempts"`
		DeliverySucceeded              int      `json:"deliverySucceeded"`
		ResolvedByTelegram             int      `json:"resolvedByTelegram"`
		ResolutionLatencyP95Ms         *float64 `json:"resolutionLatencyP95Ms"`
	}
	resp := call(handler.ReviewOpsSummary, "/api/v1/admin/reviews/summary?range=24h")
	if resp.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", resp.Code, resp.Body.String())
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.OpenReviews < 1 || summary.EligibleTelegramReviews < 1 || summary.ActionableTelegramProjections < 1 {
		t.Fatalf("summary counts = %+v", summary)
	}
	if summary.DeliveryAttempts < 1 || summary.DeliverySucceeded < 1 {
		t.Fatalf("delivery counts = %+v", summary)
	}
	if summary.ResolvedByTelegram < 1 {
		t.Fatalf("resolvedByTelegram = %d", summary.ResolvedByTelegram)
	}
	if summary.ResolutionLatencyP95Ms == nil {
		t.Fatal("resolution latency p95 missing")
	}

	resp = call(handler.ReviewOpsBreakdown, "/api/v1/admin/reviews/breakdown?range=24h")
	if resp.Code != http.StatusOK {
		t.Fatalf("breakdown status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "AMBIGUOUS_CATEGORY") || !strings.Contains(resp.Body.String(), "coverageRate") {
		t.Fatalf("breakdown body=%s", resp.Body.String())
	}

	resp = call(handler.ReviewOpsProjections, "/api/v1/admin/reviews/projections?range=24h")
	if resp.Code != http.StatusOK {
		t.Fatalf("projections status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, "deliveryStatus") || !strings.Contains(body, "ageMs") {
		t.Fatalf("projections body=%s", body)
	}
	// Redaction: no financial evidence field may appear in any Admin review body.
	for _, forbidden := range []string{"amount", "merchant", "counterparty", "funding_account_hint", "provider_account_hint", "description", "body\""} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("projections leaked %q: %s", forbidden, body)
		}
	}
}

func TestHouseholdOverviewReviewDiagnostics(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	mustExec := func(_ pgconn.CommandTag, err error) { must(err) }
	stamp := time.Now().UnixNano()
	chatID := stamp
	var adminID, userID, householdID, sourceID, itemID, requestID string
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,is_super_admin) VALUES($1,'Admin','!',true) RETURNING id`, fmt.Sprintf("admin-hh-ops-%d@example.test", stamp)).Scan(&adminID))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("HH ops %d", stamp)).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','!') RETURNING id`, fmt.Sprintf("owner-hh-ops-%d@example.test", stamp)).Scan(&userID))
	mustExec(pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID))
	mustExec(pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, userID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("hh-ro-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, householdID, sourceID).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, itemID, householdID).Scan(&requestID))
	mustExec(pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,7)`, requestID, chatID))

	handler := NewHandler(pool, false, "responses")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/households/"+householdID+"/overview", nil)
	request.SetPathValue("householdId", householdID)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: adminID, IsSuperAdmin: true}))
	response := httptest.NewRecorder()
	handler.Require(http.HandlerFunc(handler.HouseholdOverview)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("household overview status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{"reviewDiagnostics", "actionableProjections", "latestDeliveryFailureErrorClass"} {
		if !strings.Contains(body, want) {
			t.Fatalf("household overview missing %q: %s", want, body)
		}
	}
	var parsed struct {
		ReviewDiagnostics struct {
			EligibleTelegram      int `json:"eligibleTelegram"`
			ActionableProjections int `json:"actionableProjections"`
		} `json:"reviewDiagnostics"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.ReviewDiagnostics.EligibleTelegram < 1 || parsed.ReviewDiagnostics.ActionableProjections < 1 {
		t.Fatalf("review diagnostics = %+v", parsed.ReviewDiagnostics)
	}
}

func TestReviewOpsRequiresSuperAdmin(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://invalid:invalid@127.0.0.1:1/none")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	handler := NewHandler(pool, false, "responses")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews/summary", nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: "user", IsSuperAdmin: false}))
	response := httptest.NewRecorder()
	handler.Require(http.HandlerFunc(handler.ReviewOpsSummary)).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-super-admin status=%d", response.Code)
	}
}
