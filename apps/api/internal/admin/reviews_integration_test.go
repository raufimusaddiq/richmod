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
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
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
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID).Scan(&openItem))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,resolved_by_user_id,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),$3,'{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID, userID).Scan(&resolvedItem))
	// The resolution surface is recorded by the surface's own audit row: the
	// Telegram resolve records actor_type TELEGRAM.
	mustExec(pool.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,'TELEGRAM',$2,'RESOLVE_REVIEW','review_item',$3)`, householdID, userID, resolvedItem))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, openItem, householdID).Scan(&openRequest))
	mustExec(pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,99)`, openRequest, chatID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status,resolved_at) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now()) RETURNING id`, resolvedItem, householdID).Scan(&resolvedRequest))
	// A delivered card's send job should count as a delivery attempt.
	mustExec(pool.Exec(ctx, `INSERT INTO job(type,payload_json,status) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'review_request_id',$2::text),'SUCCEEDED')`, chatID, openRequest))
	mustExec(pool.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id) VALUES($1,'TELEGRAM','STALE_REVIEW_ACTION','source_event',$2)`, householdID, sourceID))

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
		StaleActionAttempts            int      `json:"staleActionAttempts"`
		ResolvedByTelegram             int      `json:"resolvedByTelegram"`
		ResolvedByWeb                  int      `json:"resolvedByWeb"`
		WebEscapeRate                  *float64 `json:"webEscapeRate"`
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
	if summary.StaleActionAttempts < 1 {
		t.Fatalf("stale actions not counted: %+v", summary)
	}
	// The surface split is asserted against this fixture's own items in
	// TestReviewOpsSurfaceComesFromTheSharedResolver, which resolves through the
	// real shared operations. The aggregate totals below are household-wide, so a
	// shared test database can already contain both surfaces.
	//
	// The same user resolves a second review from the Web lane: it must count as
	// WEB even though the user owns a Telegram identity, and must not create a
	// Web escape because that item never had a Telegram projection.
	webResolvedItem := createResolvedReviewForSurface(t, pool, householdID, sourceID, userID, "USER")
	_ = createResolvedReviewForSurface(t, pool, householdID, sourceID, userID, "TELEGRAM")
	_ = webResolvedItem
	resp = call(handler.ReviewOpsSummary, "/api/v1/admin/reviews/summary?range=24h")
	if resp.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", resp.Code, resp.Body.String())
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.ResolvedByWeb < 1 {
		t.Fatalf("a Web resolve must count as Web: %+v", summary)
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

// createResolvedReviewForSurface seeds one resolved review_item plus the audit
// row that records which surface resolved it, which is the canonical source the
// Admin aggregates read.
func createResolvedReviewForSurface(t *testing.T, pool *pgxpool.Pool, householdID, sourceID, userID, surface string) string {
	t.Helper()
	ctx := context.Background()
	var itemID string
	if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,resolved_by_user_id,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),$3,'{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID, userID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4)`, householdID, surface, userID, itemID); err != nil {
		t.Fatal(err)
	}
	return itemID
}

// seedReviewOpsHousehold creates an admin, a household owner with a Telegram
// identity, and the household itself for the review-ops surface tests.
func seedReviewOpsHousehold(t *testing.T, pool *pgxpool.Pool, stamp int64) (adminID, observerID, householdID string, chatID int64) {
	t.Helper()
	ctx := context.Background()
	chatID = stamp
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,is_super_admin) VALUES($1,'Admin','!',true) RETURNING id`, fmt.Sprintf("admin-surface-%d@example.test", stamp)).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Review surface %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','!') RETURNING id`, fmt.Sprintf("owner-surface-%d@example.test", stamp)).Scan(&observerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, observerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, chatID, householdID, observerID); err != nil {
		t.Fatal(err)
	}
	return adminID, observerID, householdID, chatID
}

// TestReviewOpsSurfaceComesFromTheSharedResolver proves the review-ops surface is
// written by the resolution itself. The earlier fixture seeded audit rows by
// hand, so the metric would have looked correct even if no production path wrote
// the row the aggregate reads. This routes a real financial-email resolution
// through the shared operation and asserts the surface it recorded.
func TestReviewOpsSurfaceComesFromTheSharedResolver(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	handler := NewHandler(pool, false, "responses")
	adminID, observerID, householdID, _ := seedReviewOpsHousehold(t, pool, stamp)
	_ = observerID

	// One review resolved from Web and one from Telegram, both by the same user,
	// through the operation each surface actually calls.
	var webItem, telegramItem string
	for _, resolvedBy := range []struct {
		surface string
		into    *string
	}{{"USER", &webItem}, {"TELEGRAM", &telegramItem}} {
		var source, observation string
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'FINANCIAL_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("surface-%s-%d", resolvedBy.surface, stamp), []byte(fmt.Sprint(stamp))).Scan(&source); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,0,'CASH_MOVEMENT','{}'::jsonb,'REVIEW') RETURNING id`, householdID, source).Scan(&observation); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,financial_email_observation_id,review_type,status,decision) VALUES($1,$2,'FINANCIAL_EMAIL_RESOLUTION','OPEN','{"version":1,"reasonCode":"FINANCIAL_EMAIL_RESOLUTION","missingFacts":["funding_account"],"allowedActions":["SET_FINANCIAL_EMAIL_ENTITIES","IGNORE"]}'::jsonb) RETURNING id`, householdID, observation).Scan(resolvedBy.into); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reviewdomain.ResolveFinancialEmailReview(ctx, tx, reviewdomain.FinancialEmailCommand{
			HouseholdID: householdID, ObservationID: observation, ReviewItemID: *resolvedBy.into,
			ActorUserID: observerID, ActorType: resolvedBy.surface, Ignore: true,
		}); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// The aggregate is household-wide, so assert the row the resolver wrote for
	// each of this fixture's items rather than a delta in a shared database.
	for _, want := range []struct {
		itemID  string
		surface string
	}{{webItem, "USER"}, {telegramItem, "TELEGRAM"}} {
		var surface string
		if err := pool.QueryRow(ctx, `SELECT a.actor_type FROM audit_log a WHERE a.action='RESOLVE_REVIEW' AND a.entity_type='review_item' AND a.entity_id=$1`, want.itemID).Scan(&surface); err != nil {
			t.Fatalf("%s: shared resolver wrote no canonical surface row: %v", want.surface, err)
		}
		if surface != want.surface {
			t.Fatalf("surface=%s want %s", surface, want.surface)
		}
	}
	// The aggregate must still read those rows: the counts it reports for the
	// fixture are included in the household-wide totals.
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews/summary?range=24h", nil)
	req = req.WithContext(auth.ContextWithPrincipal(req.Context(), auth.Principal{UserID: adminID}))
	handler.ReviewOpsSummary(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", resp.Code, resp.Body.String())
	}
	var summary struct {
		ResolvedByTelegram int `json:"resolvedByTelegram"`
		ResolvedByWeb      int `json:"resolvedByWeb"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.ResolvedByWeb < 1 || summary.ResolvedByTelegram < 1 {
		t.Fatalf("shared resolver surface not counted: %+v", summary)
	}
}

// TestReviewOpsTARCAndWebEscapeUseCompletionCapability pins the two review-ops
// metric definitions: a delivered card whose current decision has an action with
// no Telegram lane does not increase TARC, and a fully Telegram-capable review
// voluntarily finished on Web is not a mandatory Web escape.
func TestReviewOpsTARCAndWebEscapeUseCompletionCapability(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	_, observerID, householdID, chatID := seedReviewOpsHousehold(t, pool, stamp)

	cardNumber := 0
	deliveredCard := func(decision string) string {
		t.Helper()
		cardNumber++
		var sourceID, itemID, requestID string
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("tarc-%d-%d", cardNumber, stamp), []byte(fmt.Sprintf("tarc-%d-%d", cardNumber, stamp))).Scan(&sourceID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN',$3::jsonb) RETURNING id`, householdID, sourceID, decision).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN') RETURNING id`, itemID, householdID).Scan(&requestID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,$3)`, requestID, chatID, stamp+int64(cardNumber)); err != nil {
			t.Fatal(err)
		}
		return itemID
	}

	// A card whose ordinary action is Telegram-complete counts; one carrying a
	// Web-only action does not.
	since := time.Now().Add(-time.Hour)
	coverage := func() (eligible, actionable int) {
		var legacy int
		if err := pool.QueryRow(ctx, coverageSQL, since, telegramCompleteActions).Scan(&eligible, &actionable, &legacy); err != nil {
			t.Fatal(err)
		}
		return eligible, actionable
	}
	eligibleBefore, before := coverage()
	deliveredCard(`{"reasonCode":"WEALTH_OBSERVATION_CONFIRMATION","allowedActions":["SET_WEALTH_ACCOUNT","IGNORE"]}`)
	if eligible, got := coverage(); got != before+1 || eligible != eligibleBefore+1 {
		t.Fatalf("Telegram-complete card did not increase actionable coverage: before=%d/%d after=%d/%d", before, eligibleBefore, got, eligible)
	}
	deliveredCard(`{"reasonCode":"WEALTH_OBSERVATION_CONFIRMATION","allowedActions":["PREPARE_SNAPSHOT","IGNORE"]}`)
	if eligible, got := coverage(); got != before+1 || eligible != eligibleBefore+2 {
		t.Fatalf("a Web-only ordinary action inflated actionable coverage: %d/%d", got, eligible)
	}

	escapes := func() (resolved, escaped int) {
		if err := pool.QueryRow(ctx, webEscapeCountsSQL, since, telegramCompleteActions, "WEALTH_OBSERVATION_CONFIRMATION").Scan(&resolved, &escaped); err != nil {
			t.Fatal(err)
		}
		return resolved, escaped
	}
	_, baselineEscapes := escapes()
	// A fully Telegram-capable review resolved on Web is a voluntary switch, so
	// the per-type escape count must not include it.
	voluntaryItem := deliveredCard(`{"reasonCode":"WEALTH_OBSERVATION_CONFIRMATION","allowedActions":["SET_WEALTH_ACCOUNT","IGNORE"]}`)
	if _, err := pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2 WHERE id=$1`, voluntaryItem, observerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,'USER',$2,'RESOLVE_REVIEW','review_item',$3)`, householdID, observerID, voluntaryItem); err != nil {
		t.Fatal(err)
	}
	if _, got := escapes(); got != baselineEscapes {
		t.Fatalf("a voluntary Web switch changed mandatory escapes: before=%d after=%d", baselineEscapes, got)
	}
	// A Web-only review resolved on Web is the mandatory escape this rate means.
	forcedItem := deliveredCard(`{"reasonCode":"WEALTH_OBSERVATION_CONFIRMATION","allowedActions":["PREPARE_SNAPSHOT","IGNORE"]}`)
	if _, err := pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2 WHERE id=$1`, forcedItem, observerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,'USER',$2,'RESOLVE_REVIEW','review_item',$3)`, householdID, observerID, forcedItem); err != nil {
		t.Fatal(err)
	}
	if _, got := escapes(); got != baselineEscapes+1 {
		t.Fatalf("a Web-only review did not add one escape: before=%d after=%d", baselineEscapes, got)
	}
	// Coverage is range-based: resolving reviews must not drop them from TARC,
	// so the rate stays measurable once the inbox is empty.
	if eligible, got := coverage(); got != before+2 || eligible != eligibleBefore+4 {
		t.Fatalf("resolved reviews left coverage: actionable=%d eligible=%d", got, eligible)
	}
}

// TestReviewOpsSurfaceFromResolvingTransaction pins attribution for resolvers
// whose audit row uses their own action name (CONFIRM_REVIEW,
// COMPLETE_BANK_FACTS_REQUESTED, ...) on another entity: the row written in the
// resolving transaction decides the surface. A resolver that wrote no audit row
// falls back to its resolution_action.
func TestReviewOpsSurfaceFromResolvingTransaction(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	_, observerID, householdID, _ := seedReviewOpsHousehold(t, pool, stamp)
	var sourceID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("surface-tx-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	resolve := func(resolutionAction, actorType, auditAction string) string {
		t.Helper()
		var itemID string
		if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action=$2 WHERE id=$1`, itemID, resolutionAction); err != nil {
			t.Fatal(err)
		}
		if auditAction != "" {
			// Entity is unrelated to the item: only the shared transaction ties them.
			if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,$2,$3,$4,'transaction',gen_random_uuid())`, householdID, actorType, observerID, auditAction); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return itemID
	}
	for _, c := range []struct {
		name, resolutionAction, actorType, auditAction, want string
	}{
		{"web confirm", "CONFIRM_REVIEW", "USER", "CONFIRM_REVIEW", "USER"},
		{"telegram detail", "TELEGRAM_CONFIRMED", "TELEGRAM", "UPDATE_REVIEW_DETAIL", "TELEGRAM"},
		{"worker fallback", "EMAIL_RECEIVED_AT_FALLBACK", "WORKER", "CREATE_FROM_BANK_EMAIL", "SYSTEM"},
		{"telegram without audit", "TELEGRAM_MERCHANT_DECISION", "", "", "TELEGRAM"},
		{"migration backfill", "LEGACY_TRANSACTION_RESOLVED", "", "", "SYSTEM"},
	} {
		itemID := resolve(c.resolutionAction, c.actorType, c.auditAction)
		var surface *string
		if err := pool.QueryRow(ctx, `SELECT s.surface FROM review_item ri `+resolutionSurfaceSQL+` WHERE ri.id=$1`, itemID).Scan(&surface); err != nil {
			t.Fatal(err)
		}
		if surface == nil || *surface != c.want {
			t.Fatalf("%s: surface=%v want %s", c.name, surface, c.want)
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
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID).Scan(&itemID))
	must(pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,review_type,status) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN') RETURNING id`, itemID, householdID).Scan(&requestID))
	mustExec(pool.Exec(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id) VALUES($1,$2,7)`, requestID, chatID))
	mustExec(pool.Exec(ctx, `INSERT INTO job(type,payload_json,status,last_error) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('review_request_id',$1::text),'FAILED','TELEGRAM_SEND_FAILED')`, requestID))

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
			LatestFailureAt       *time.Time `json:"latestDeliveryFailureAt"`
			LatestFailureError    *string `json:"latestDeliveryFailureErrorClass"`
		} `json:"reviewDiagnostics"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.ReviewDiagnostics.EligibleTelegram < 1 || parsed.ReviewDiagnostics.ActionableProjections < 1 {
		t.Fatalf("review diagnostics = %+v", parsed.ReviewDiagnostics)
	}
	if parsed.ReviewDiagnostics.LatestFailureAt == nil || parsed.ReviewDiagnostics.LatestFailureError == nil || *parsed.ReviewDiagnostics.LatestFailureError != "TELEGRAM_SEND_FAILED" {
		t.Fatalf("household failure diagnosis = %+v", parsed.ReviewDiagnostics)
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
