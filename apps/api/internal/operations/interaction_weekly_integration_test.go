package operations

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

type weeklyFixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	household string
	stamp     int64
	seq       int
}

func newWeeklyFixture(t *testing.T, pool *pgxpool.Pool) *weeklyFixture {
	t.Helper()
	f := &weeklyFixture{t: t, pool: pool, stamp: testsupport.UniqueStamp()}
	if err := pool.QueryRow(context.Background(), `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Weekly %d", f.stamp)).Scan(&f.household); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *weeklyFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *weeklyFixture) card(reviewType, outcome string, sentAgo, finishAfter time.Duration) {
	f.t.Helper()
	f.seq++
	ctx := context.Background()
	var txID, itemID, requestID, recipientID string
	if err := f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',1000,'IDR',now(),now()) RETURNING id`, f.household).Scan(&txID); err != nil {
		f.t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,$3,'OPEN',jsonb_build_object('reasonCode',$3::text,'allowedActions',jsonb_build_array('CONFIRM_REVIEW','IGNORE'),'decisionPolicyVersion','v1','decisionSource','DETERMINISTIC','interactionMode','BOUNDED_CHOICE')) RETURNING id`, f.household, txID, reviewType).Scan(&itemID); err != nil {
		f.t.Fatal(err)
	}
	chat := f.stamp*1000 + int64(f.seq)
	if err := f.pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,$4,$5,'OPEN') RETURNING id`, itemID, f.household, txID, reviewType, chat).Scan(&requestID); err != nil {
		f.t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO review_request_recipient(review_request_id,telegram_chat_id) VALUES($1,$2) RETURNING id`, requestID, chat).Scan(&recipientID); err != nil {
		f.t.Fatal(err)
	}
	f.exec(`INSERT INTO review_request_message(household_id,review_request_id,recipient_id,telegram_chat_id,telegram_message_id,created_at) VALUES($1,$2,$3,$4,$5,now()-make_interval(secs => $6))`,
		f.household, requestID, recipientID, chat, f.seq, sentAgo.Seconds())
	if outcome == "OPEN" {
		return
	}
	if outcome == "TELEGRAM" {
		f.exec(`INSERT INTO product_telemetry_event(household_id,review_item_id,event_type,source_type,action,occurred_at) VALUES($1,$2,'INTERACTION','TAP','ANSWERED',now()-make_interval(secs => $3))`, f.household, itemID, (sentAgo - finishAfter).Seconds())
	}
	f.exec(`UPDATE review_item SET status='RESOLVED',resolved_at=now()-make_interval(secs => $2),resolution_action='CONFIRM_REVIEW' WHERE id=$1`, itemID, (sentAgo - finishAfter).Seconds())
	if outcome == "WEB" {
		f.exec(`INSERT INTO product_telemetry_event(household_id,review_item_id,event_type,action,occurred_at) VALUES($1,$2,'INTERACTION','FINISHED_ON_WEB',now()-make_interval(secs => $3))`, f.household, itemID, (sentAgo - finishAfter).Seconds())
	}
}

func (f *weeklyFixture) event(action, source, decision string, ago time.Duration) {
	f.t.Helper()
	f.exec(`INSERT INTO product_telemetry_event(household_id,event_type,source_type,action,decision_source,occurred_at) VALUES($1,'INTERACTION',NULLIF($2,''),$3,NULLIF($4,''),now()-make_interval(secs => $5))`,
		f.household, source, action, decision, ago.Seconds())
}

const day = 24 * time.Hour

func TestInteractionWeeklyMeasuresAndWindows(t *testing.T) {
	t.Parallel()
	pool := integrationPool(t)
	ctx := context.Background()
	f := newWeeklyFixture(t, pool)
	other := newWeeklyFixture(t, pool)

	f.card("UNKNOWN_MERCHANT", "TELEGRAM", 2*day, 30*time.Second)
	f.card("UNKNOWN_MERCHANT", "TELEGRAM", 2*day, 90*time.Second)
	f.card("UNKNOWN_MERCHANT", "TELEGRAM", 2*day, 60*time.Second)
	f.card("UNKNOWN_MERCHANT", "WEB", 2*day, time.Hour)
	f.card("TRANSFER_CLASSIFICATION", "WEB", 3*day, time.Hour)
	f.card("TRANSFER_CLASSIFICATION", "OPEN", 3*day, 0)
	f.card("AMBIGUOUS_CATEGORY", "TELEGRAM", 10*day, time.Minute)
	f.card("AMBIGUOUS_CATEGORY", "OPEN", 10*day, 0)
	f.card("AMBIGUOUS_CATEGORY", "TELEGRAM", 30*day, time.Minute)
	other.card("UNKNOWN_MERCHANT", "TELEGRAM", 2*day, time.Minute)

	f.event("ANSWERED", "TAP", "", day)
	f.event("ANSWERED", "REPLY", "", day)
	f.event("ANSWERED", "PLAIN", "", day)
	f.event("UNMATCHED", "", "", day)
	f.event("CLOSED_CARD_REPLY", "", "", day)
	f.event("REPEATED_INPUT", "", "", day)
	f.event("REPEATED_INPUT", "", "", 10*day)
	f.event("ANSWERED", "TAP", "", 10*day)
	f.event("ANSWERED", "TAP", "", 30*day)
	other.event("UNMATCHED", "", "", day)

	handler := aggregateHandler(pool)
	summary, err := handler.loadInteractionWeekly(ctx, f.household, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cur := summary.Current
	if cur.CardsSent != 6 || cur.FinishedTelegram != 3 || cur.FinishedWeb != 2 || cur.StillOpen != 1 {
		t.Fatalf("current cards = %+v", cur)
	}
	if cur.FinishedRate == nil || *cur.FinishedRate != 0.5 {
		t.Fatalf("finished rate = %v", cur.FinishedRate)
	}
	if cur.TypesSent != 2 || cur.TypesMostlyTelegram != 1 {
		t.Fatalf("types = %d of %d, want 1 of 2 (3 of 4 clears 70%%, 0 of 2 does not)", cur.TypesMostlyTelegram, cur.TypesSent)
	}
	if cur.MedianFinishMs == nil || *cur.MedianFinishMs < 59000 || *cur.MedianFinishMs > 61000 {
		t.Fatalf("median = %v, want about 60000", cur.MedianFinishMs)
	}
	if cur.Answers != 6 || cur.UnusableAnswers != 2 || cur.UnusableRate == nil || *cur.UnusableRate != 0.25 {
		t.Fatalf("answers = %d unusable = %d rate = %v", cur.Answers, cur.UnusableAnswers, cur.UnusableRate)
	}
	if cur.AnswerSources["TAP"] != 4 || cur.AnswerSources["REPLY"] != 1 || cur.AnswerSources["PLAIN"] != 1 || cur.AnswerSources["VIA_ASSISTANT"] != 0 {
		t.Fatalf("sources = %v", cur.AnswerSources)
	}
	if cur.RepeatedInput != 1 {
		t.Fatalf("repeated = %d", cur.RepeatedInput)
	}
	prev := summary.Previous
	if prev.CardsSent != 2 || prev.FinishedTelegram != 1 || prev.TypesSent != 1 || prev.RepeatedInput != 1 || prev.Answers != 2 {
		t.Fatalf("previous = %+v", prev)
	}
	if prev.UnusableRate == nil || *prev.UnusableRate != 0 {
		t.Fatalf("previous unusable rate = %v", prev.UnusableRate)
	}
	wantMissing := map[string]bool{"broken_or_empty_replies": true, "telegram_mechanics_replies": true, "answers_via_assistant": true}
	if len(summary.NotYetMeasurable) != len(wantMissing) {
		t.Fatalf("notYetMeasurable = %v", summary.NotYetMeasurable)
	}
	for _, key := range summary.NotYetMeasurable {
		if !wantMissing[key] {
			t.Fatalf("unexpected coverage gap %q", key)
		}
	}

	empty, err := handler.loadInteractionWeekly(ctx, other.household, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if empty.Current.CardsSent != 1 || empty.Current.UnusableAnswers != 1 || empty.Current.Answers != 1 {
		t.Fatalf("other household saw %+v", empty.Current)
	}
	quiet := newWeeklyFixture(t, pool)
	none, err := handler.loadInteractionWeekly(ctx, quiet.household, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if none.Current.FinishedRate != nil || none.Current.UnusableRate != nil || none.Current.MedianFinishMs != nil {
		t.Fatalf("empty household returned rates: %+v", none.Current)
	}
}

func TestInteractionWeeklyWindowEdgesFollowJakartaDays(t *testing.T) {
	t.Parallel()
	pool := integrationPool(t)
	ctx := context.Background()
	f := newWeeklyFixture(t, pool)
	now := time.Now()
	previous, current, err := interactionWindows(now)
	if err != nil {
		t.Fatal(err)
	}
	var wantStart time.Time
	if err := pool.QueryRow(ctx, `SELECT (date_trunc('day', $1::timestamptz AT TIME ZONE 'Asia/Jakarta') - interval '6 days') AT TIME ZONE 'Asia/Jakarta'`, now).Scan(&wantStart); err != nil {
		t.Fatal(err)
	}
	if !current.Start.Equal(wantStart) || !previous.End.Equal(wantStart) || !previous.Start.Equal(wantStart.AddDate(0, 0, -7)) {
		t.Fatalf("windows %v %v do not start at Jakarta midnight %v", previous, current, wantStart)
	}
	insertAt := func(at time.Time) {
		f.exec(`INSERT INTO product_telemetry_event(household_id,event_type,action,occurred_at) VALUES($1,'INTERACTION','REPEATED_INPUT',$2)`, f.household, at)
	}
	insertAt(current.Start)
	insertAt(current.Start.Add(-time.Second))
	insertAt(previous.Start.Add(-time.Second))
	summary, err := aggregateHandler(pool).loadInteractionWeekly(ctx, f.household, now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Current.RepeatedInput != 1 || summary.Previous.RepeatedInput != 1 {
		t.Fatalf("edges: current %d previous %d", summary.Current.RepeatedInput, summary.Previous.RepeatedInput)
	}
}

func TestInteractionWeeklyReplyQualityCountsOnceRecorded(t *testing.T) {
	t.Parallel()
	pool := integrationPool(t)
	ctx := context.Background()
	f := newWeeklyFixture(t, pool)
	f.exec(`ALTER TABLE product_telemetry_event DROP CONSTRAINT IF EXISTS product_telemetry_event_interaction_check`)
	f.event("REPLY_REPAIRED", "", "EMPTY", day)
	f.event("REPLY_REPAIRED", "", "BROKEN_TEXT", day)
	f.event("REPLY_REPAIRED", "", "TELEGRAM_MECHANICS", day)
	f.event("REPLY_REPAIRED", "", "TELEGRAM_MECHANICS", 10*day)
	summary, err := aggregateHandler(pool).loadInteractionWeekly(ctx, f.household, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Current.BrokenReplies != 2 || summary.Current.MechanicsReplies != 1 || summary.Previous.MechanicsReplies != 1 {
		t.Fatalf("reply quality = %+v / %+v", summary.Current, summary.Previous)
	}
	for _, key := range summary.NotYetMeasurable {
		if key != "answers_via_assistant" {
			t.Fatalf("still reported as not measurable: %q", key)
		}
	}
}

func TestInteractionWeeklyEndpointIsOwnerOnly(t *testing.T) {
	t.Parallel()
	handler := &Handler{pool: poolQueryer{}, probeSlots: make(chan struct{}, 1)}
	for name, tc := range map[string]struct {
		principal auth.Principal
		want      int
	}{
		"no household": {auth.Principal{}, http.StatusUnauthorized},
		"member":       {auth.Principal{HasHousehold: true, HouseholdID: "h", HouseholdRole: "MEMBER"}, http.StatusForbidden},
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/interaction-weekly", nil)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), tc.principal))
		response := httptest.NewRecorder()
		handler.InteractionWeekly(response, request)
		if response.Code != tc.want {
			t.Fatalf("%s: status = %d, want %d", name, response.Code, tc.want)
		}
	}
}
