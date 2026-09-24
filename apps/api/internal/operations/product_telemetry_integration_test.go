package operations

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// PRD §22.2/§22.3: the append-only telemetry table must capture review turns and
// post-auto-confirm corrections atomically with the canonical write, and the
// aggregate must surface them without persisting any financial value or text.
func TestProductTelemetryCapturesTurnAndAutoConfirmCorrection(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var householdID, userID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Telemetry %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("telemetry-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	var eventID, txID, reviewID, requestID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),decode(md5($2),'hex'),'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("telemetry-%d", stamp)).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,auto_confirmed_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',25000,'IDR',now(),now(),now()) RETURNING id`, householdID).Scan(&txID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN',jsonb_build_object('decisionPolicyVersion','v1','decisionSource','DETERMINISTIC','interactionMode','BOUNDED_CHOICE'))`, householdID, txID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM review_item WHERE household_id=$1`, householdID).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,telegram_chat_id,status) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY',$4,'OPEN') RETURNING id`, reviewID, householdID, txID, stamp).Scan(&requestID); err != nil {
		t.Fatal(err)
	}

	// A saved Telegram detail is one turn while the review is still open; the
	// web review below exercises the resolution-fallback path.
	if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'field','description'))`, txID, eventID, requestID); err != nil {
		t.Fatal(err)
	}
	// A web resolution with no Telegram reply evidence takes the fallback turn path.
	var webTxID, webEventID, webReviewID string
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',15000,'IDR',now(),now()) RETURNING id`, householdID).Scan(&webTxID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),decode(md5($2),'hex'),'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("telemetry-web-%d", stamp)).Scan(&webEventID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'TEST')`, webTxID, webEventID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN',jsonb_build_object('decisionPolicyVersion','v1','decisionSource','DETERMINISTIC','interactionMode','BOUNDED_CHOICE')) RETURNING id`, householdID, webTxID).Scan(&webReviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='CONFIRM_REVIEW',resolution_values=jsonb_build_object('category_id',gen_random_uuid()) WHERE id=$1`, webReviewID); err != nil {
		t.Fatal(err)
	}
	// This web-style review has no bound Telegram reply, so resolution itself is
	// the single recorded turn.
	if _, err := pool.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='CONFIRM_REVIEW',resolution_values=jsonb_build_object('category_id',gen_random_uuid()) WHERE id=$1`, reviewID); err != nil {
		t.Fatal(err)
	}
	// A delayed callback can persist its bound reply after the canonical review
	// has resolved; it is still one review turn and must not be dropped.
	var lateEventID, lateTxID, lateReviewID, lateRequestID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),decode(md5($2),'hex'),'PROCESSED') RETURNING id`, householdID, fmt.Sprintf("telemetry-late-%d", stamp)).Scan(&lateEventID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',7000,'IDR',now(),now()) RETURNING id`, householdID).Scan(&lateTxID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,resolved_at,resolution_action,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),'CONFIRM_REVIEW',jsonb_build_object('interactionMode','BOUNDED_CHOICE')) RETURNING id`, householdID, lateTxID).Scan(&lateReviewID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,telegram_chat_id,status,resolved_at) VALUES($1,$2,$3,'AMBIGUOUS_CATEGORY',$4,'RESOLVED',now()) RETURNING id`, lateReviewID, householdID, lateTxID, stamp).Scan(&lateRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'classification','CATEGORY_SELECTED'))`, lateTxID, lateEventID, lateRequestID); err != nil {
		t.Fatal(err)
	}

	var turns, bounded int
	if err := pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(bounded_choices),0) FROM product_telemetry_event WHERE household_id=$1 AND event_type='REVIEW_TURN'`, householdID).Scan(&turns, &bounded); err != nil {
		t.Fatal(err)
	}
	if turns != 3 {
		t.Fatalf("expected Telegram detail, web resolution, and late Telegram reply turns, got %d", turns)
	}
	// Telegram category and web resolution are bounded choices; detail entry is not.
	if bounded != 2 {
		t.Fatalf("bounded-choice turns must be counted per turn, got %d", bounded)
	}
	// No financial value or free text may be persisted: only field names.
	var leaked int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM product_telemetry_event e CROSS JOIN LATERAL unnest(e.changed_fields) f WHERE e.household_id=$1 AND f NOT IN ('merchant','category','description','note','transaction_at','purpose','wealth_account','account','amount','type','status')`, householdID).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("telemetry changed_fields must be a bounded field-name allow-list, leaked=%d", leaked)
	}

	// A later correction to the auto-confirmed transaction records the correction.
	if _, err := pool.Exec(ctx, `UPDATE transaction SET description='corrected later' WHERE id=$1`, txID); err != nil {
		t.Fatal(err)
	}
	aggregate, err := NewHandler(pool).loadProductAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.ReviewRoundTrips != 3 || aggregate.BoundedChoices != 2 {
		t.Fatalf("aggregate must expose review turns and bounded choices: %+v", aggregate)
	}
	if want := 2.0 / 3.0; aggregate.BoundedChoicesPerEvent != want {
		t.Fatalf("bounded choices per confirmed event must use the canonical cohort: got %v want %v", aggregate.BoundedChoicesPerEvent, want)
	}
	if aggregate.AutoConfirmEvents != 1 || aggregate.AutoConfirmCorrections != 1 || aggregate.AutoConfirmCorrectionRate != 1 {
		t.Fatalf("aggregate must expose the auto-confirm correction rate: %+v", aggregate)
	}
	if aggregate.AutoConfirmCorrectionFields["description"] != 1 {
		t.Fatalf("correction field breakdown missing: %+v", aggregate.AutoConfirmCorrectionFields)
	}
	if aggregate.AutoConfirmCorrectionSource["TELEGRAM_TEXT"] != 1 {
		t.Fatalf("correction source breakdown missing: %+v", aggregate.AutoConfirmCorrectionSource)
	}
}
