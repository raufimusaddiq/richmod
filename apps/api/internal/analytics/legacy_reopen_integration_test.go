package analytics

import (
	"context"
	"os"
	"strings"
	"testing"
)

const legacyReopenMigration = "00083_reopen_uncategorized_dispute_expense.sql"
const legacyReopenTarget = "6ae4522f-8507-4688-a03e-d6b652279870"

// migrationBody returns the DO block of the Up or Down section of the migration,
// with the production target id rebound to the fixture row. The test therefore
// executes the real migration SQL, not a copy.
func migrationBody(t *testing.T, section, id string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../db/migrations/" + legacyReopenMigration)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	up, down, found := strings.Cut(text, "-- +goose Down")
	if !found {
		t.Fatal("migration has no Down section")
	}
	part := up
	if section == "down" {
		part = down
	}
	start := strings.Index(part, "-- +goose StatementBegin")
	end := strings.Index(part, "-- +goose StatementEnd")
	if start < 0 || end < start {
		t.Fatalf("%s section has no statement block", section)
	}
	body := part[start+len("-- +goose StatementBegin") : end]
	if !strings.Contains(body, legacyReopenTarget) {
		t.Fatal("migration no longer names its target transaction")
	}
	return strings.ReplaceAll(body, legacyReopenTarget, id)
}

type ledgerCycleState struct {
	status                 string
	openReview             bool
	ledgerComplete         bool
	cycleUncategorized     int
	cycleOpenReviews       int
	cycleSaysUnfinished    bool
	disagreementsInHousehd int
}

// surfaces reads one record through both definitions: the ledger's completion
// rule (transaction.status and active review_item, as in ledger/lifecycle.go)
// and the served /analytics cycle review for the current cycle.
func (f reviewFixture) surfaces(t *testing.T, id string) ledgerCycleState {
	t.Helper()
	var s ledgerCycleState
	if err := f.pool.QueryRow(context.Background(), `SELECT t.status,EXISTS(SELECT 1 FROM review_item ri WHERE ri.transaction_id=t.id AND ri.household_id=t.household_id AND ri.status IN ('OPEN','PENDING_SEND')) FROM transaction t WHERE t.id=$1 AND t.household_id=$2`, id, f.household).Scan(&s.status, &s.openReview); err != nil {
		t.Fatal(err)
	}
	s.ledgerComplete = s.status == "CONFIRMED" && !s.openReview
	for _, b := range f.review(t, "").Quality {
		switch b.Kind {
		case "UNCATEGORIZED_EXPENSE":
			s.cycleUncategorized = b.Count
		case "OPEN_REVIEWS":
			s.cycleOpenReviews = b.Count
		}
	}
	s.cycleSaysUnfinished = s.cycleUncategorized+s.cycleOpenReviews > 0
	// Blast-radius query for the legacy class: a review-flow expense that the
	// ledger shows confirmed although the flow requires a category.
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM transaction t WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type='EXPENSE' AND t.category_id IS NULL AND EXISTS(SELECT 1 FROM review_item ri WHERE ri.transaction_id=t.id AND ri.status='RESOLVED' AND ri.resolution_action='CONFIRM_EVIDENCE_DISPUTE')`, f.household).Scan(&s.disagreementsInHousehd); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestLegacyUncategorizedDisputeExpenseAgreesAfterReopenMigration reproduces
// ticket #353 (ledger says complete, cycle says unfinished) and pins that after
// migration 00083 both surfaces agree. It fails without the migration: the
// "after" assertions see a CONFIRMED record that the cycle still flags.
func TestLegacyUncategorizedDisputeExpenseAgreesAfterReopenMigration(t *testing.T) {
	f := cycleReviewFixture(t)
	ctx := context.Background()
	f.anchor(t, "2026-09-01")
	legacy := f.transaction(t, "2026-09-05", "EXPENSE", "CONFIRMED", "2500", false)
	manual := f.transaction(t, "2026-09-06", "EXPENSE", "CONFIRMED", "700", false) // legitimate manual uncategorized entry
	if _, err := f.pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,resolution_action,resolved_at,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','RESOLVED','CONFIRM_EVIDENCE_DISPUTE',now(),jsonb_build_object('reasonCode','POSSIBLE_DUPLICATE','allowedActions',jsonb_build_array('CONFIRM_REVIEW','IGNORE')))`, f.household, legacy); err != nil {
		t.Fatal(err)
	}

	before := f.surfaces(t, legacy)
	if !before.ledgerComplete || before.cycleUncategorized != 2 || before.disagreementsInHousehd != 1 {
		t.Fatalf("reported inconsistency not reproduced: %+v", before)
	}

	apply := func(section string) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, migrationBody(t, section, legacy)); err != nil {
			t.Fatalf("migration %s: %v", section, err)
		}
	}
	apply("up")
	apply("up") // idempotent: the second run must change nothing

	after := f.surfaces(t, legacy)
	if after.ledgerComplete || after.status != "NEEDS_REVIEW" || !after.openReview {
		t.Fatalf("ledger must show the record unfinished: %+v", after)
	}
	if !after.cycleSaysUnfinished || after.cycleOpenReviews != 1 || after.cycleUncategorized != 1 {
		t.Fatalf("cycle must count the record once as an open review (and only the manual entry as uncategorized): %+v", after)
	}
	if after.disagreementsInHousehd != 0 {
		t.Fatalf("disagreement query must be zero: %+v", after)
	}
	var items, audits int
	var confirmedAt *string
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM review_item WHERE transaction_id=$1 AND status='OPEN'),(SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='REOPEN_LEGACY_UNCATEGORIZED'),(SELECT confirmed_at::text FROM transaction WHERE id=$1)`, legacy).Scan(&items, &audits, &confirmedAt); err != nil {
		t.Fatal(err)
	}
	if items != 1 || audits != 1 || confirmedAt != nil {
		t.Fatalf("idempotency/audit: open items=%d audits=%d confirmed_at=%v", items, audits, confirmedAt)
	}
	// Scope: the manual uncategorized entry is untouched.
	var manualStatus string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, manual).Scan(&manualStatus); err != nil || manualStatus != "CONFIRMED" {
		t.Fatalf("migration must not touch other records: status=%q err=%v", manualStatus, err)
	}

	// Rollback restores the original state while the household has not acted.
	apply("down")
	restored := f.surfaces(t, legacy)
	if !restored.ledgerComplete || restored.status != "CONFIRMED" {
		t.Fatalf("down must restore the prior state: %+v", restored)
	}
	apply("down") // no-op once restored
}
