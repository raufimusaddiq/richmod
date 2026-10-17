package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/review"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

const legacyReopenMigration = "00083_reopen_uncategorized_dispute_expense.sql"

// migrationBody returns the DO block of the Up or Down section of the migration.
// The test executes the real migration SQL, not a copy.
func migrationBody(t *testing.T, section string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../db/migrations/" + legacyReopenMigration)
	if err != nil {
		t.Fatal(err)
	}
	up, down, found := strings.Cut(string(raw), "-- +goose Down")
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
	return part[start+len("-- +goose StatementBegin") : end]
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

// seedLegacyDisputeExpense builds the production shape: a CONFIRMED uncategorized
// expense whose POSSIBLE_DUPLICATE review is RESOLVED by CONFIRM_EVIDENCE_DISPUTE,
// and which still carries an ACTIVE evidence_link_dispute row.
func (f reviewFixture) seedLegacyDisputeExpense(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	legacy := f.transaction(t, "2026-09-05", "EXPENSE", "CONFIRMED", "2500", false)
	if _, err := f.pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,resolution_action,resolved_at,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','RESOLVED','CONFIRM_EVIDENCE_DISPUTE',now(),jsonb_build_object('reasonCode','POSSIBLE_DUPLICATE','allowedActions',jsonb_build_array('CONFIRM_REVIEW','IGNORE')))`, f.household, legacy); err != nil {
		t.Fatal(err)
	}
	var event string
	if err := f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',gen_random_uuid()::text,now(),decode(md5(random()::text),'hex'),'PROCESSED') RETURNING id`, f.household).Scan(&event); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('evidence_link_dispute',true))`, legacy, event); err != nil {
		t.Fatal(err)
	}
	return legacy
}

// tryMigrate runs one migration section in a transaction scoped to the fixture
// household through the richmod.repair_household setting, so rows other tests
// left in the shared database cannot change the match count. A failing section
// rolls back, as goose would.
func (f reviewFixture) tryMigrate(t *testing.T, section string) error {
	t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('richmod.repair_household',$1,true)`, f.household); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, migrationBody(t, section)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (f reviewFixture) migrate(t *testing.T, section string) {
	t.Helper()
	if err := f.tryMigrate(t, section); err != nil {
		t.Fatalf("migration %s: %v", section, err)
	}
}

// pointers counts the evidence rows of a transaction that still name migration 00083.
func (f reviewFixture) pointers(t *testing.T, id string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'superseded_by_migration'`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// webConfirm drives the real Web Inbox confirm handler (POST /reviews/{id}/confirm).
func (f reviewFixture) webConfirm(t *testing.T, id, categoryID string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+id+"/confirm", bytes.NewBufferString(fmt.Sprintf(`{"categoryId":%q}`, categoryID)))
	r.SetPathValue("id", id)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: f.user, Memberships: []auth.Membership{{HouseholdID: f.household, Role: "OWNER"}}}))
	w := httptest.NewRecorder()
	review.NewHandler(f.pool).Confirm(w, r)
	return w
}

// TestLegacyUncategorizedDisputeExpenseAgreesAfterReopenMigration reproduces
// ticket #353 (ledger says complete, cycle says unfinished), pins that after
// migration 00083 both surfaces agree, and then completes the record through the
// Web confirm handler to prove it is not a dead end. It fails without the
// migration: the "after" assertions see a CONFIRMED record the cycle still flags.
func TestLegacyUncategorizedDisputeExpenseAgreesAfterReopenMigration(t *testing.T) {
	f := cycleReviewFixture(t)
	ctx := context.Background()
	f.anchor(t, "2026-09-01")
	legacy := f.seedLegacyDisputeExpense(t)
	manual := f.transaction(t, "2026-09-06", "EXPENSE", "CONFIRMED", "700", false) // legitimate manual uncategorized entry

	before := f.surfaces(t, legacy)
	if !before.ledgerComplete || before.cycleUncategorized != 2 || before.disagreementsInHousehd != 1 {
		t.Fatalf("reported inconsistency not reproduced: %+v", before)
	}

	f.migrate(t, "up")
	f.migrate(t, "up") // idempotent: the second run must change nothing

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
	var items, audits, activeDisputes int
	var confirmedAt, preferred *string
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM review_item WHERE transaction_id=$1 AND status='OPEN'),(SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='REOPEN_LEGACY_UNCATEGORIZED'),(SELECT confirmed_at::text FROM transaction WHERE id=$1),(SELECT preferred_user_id::text FROM review_item WHERE transaction_id=$1 AND status='OPEN'),(SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json->>'evidence_link_dispute'='true' AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE')`, legacy).Scan(&items, &audits, &confirmedAt, &preferred, &activeDisputes); err != nil {
		t.Fatal(err)
	}
	if items != 1 || audits != 1 || confirmedAt != nil || activeDisputes != 0 || preferred == nil || *preferred != f.user {
		t.Fatalf("idempotency/audit/contract: open items=%d audits=%d confirmed_at=%v active disputes=%d preferred=%v", items, audits, confirmedAt, activeDisputes, preferred)
	}
	var manualStatus string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, manual).Scan(&manualStatus); err != nil || manualStatus != "CONFIRMED" {
		t.Fatalf("migration must not touch other records: status=%q err=%v", manualStatus, err)
	}

	// Not a dead end: the household completes it through the real Web confirm.
	if w := f.webConfirm(t, legacy, f.category); w.Code != http.StatusNoContent {
		t.Fatalf("web confirm of the reopened record: status=%d body=%s", w.Code, w.Body.String())
	}
	done := f.surfaces(t, legacy)
	if !done.ledgerComplete || done.openReview || done.cycleOpenReviews != 0 || done.cycleUncategorized != 1 || done.disagreementsInHousehd != 0 {
		t.Fatalf("after confirm both surfaces must agree the record is complete (only the manual entry stays uncategorized): %+v", done)
	}
	var category *string
	if err := f.pool.QueryRow(ctx, `SELECT category_id::text FROM transaction WHERE id=$1`, legacy).Scan(&category); err != nil || category == nil || *category != f.category {
		t.Fatalf("category not recorded: %v err=%v", category, err)
	}
	f.migrate(t, "down") // the household acted, so Down must not undo their work
	if again := f.surfaces(t, legacy); !again.ledgerComplete {
		t.Fatalf("down must not touch a record the household completed: %+v", again)
	}
}

func TestLegacyReopenMigrationRollbackRestoresUntouchedRecord(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-09-01")
	legacy := f.seedLegacyDisputeExpense(t)
	f.migrate(t, "up")
	f.migrate(t, "down")
	restored := f.surfaces(t, legacy)
	if !restored.ledgerComplete || restored.status != "CONFIRMED" || restored.disagreementsInHousehd != 1 {
		t.Fatalf("down must restore the prior state: %+v", restored)
	}
	var active int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json->>'evidence_link_dispute'='true' AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE'`, legacy).Scan(&active); err != nil || active != 1 {
		t.Fatalf("down must reactivate the dispute row: %d err=%v", active, err)
	}
	f.migrate(t, "down") // no-op once restored
}

func TestLegacyReopenMigrationIsNoOpWhenNothingMatches(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-09-01")
	manual := f.transaction(t, "2026-09-06", "EXPENSE", "CONFIRMED", "700", false) // uncategorized, but no dispute review
	if err := f.tryMigrate(t, "up"); err != nil {
		t.Fatalf("zero matches must be a no-op, not an error: %v", err)
	}
	if s := f.surfaces(t, manual); !s.ledgerComplete || s.status != "CONFIRMED" {
		t.Fatalf("a manual uncategorized entry must not be reopened: %+v", s)
	}
}

func TestLegacyReopenMigrationRefusesMultipleMatches(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-09-01")
	first, second := f.seedLegacyDisputeExpense(t), f.seedLegacyDisputeExpense(t)
	err := f.tryMigrate(t, "up")
	if err == nil || !strings.Contains(err.Error(), "expected at most 1") {
		t.Fatalf("two matches must abort the migration, got %v", err)
	}
	for _, id := range []string{first, second} {
		if s := f.surfaces(t, id); !s.ledgerComplete || s.status != "CONFIRMED" || f.pointers(t, id) != 0 {
			t.Fatalf("a refused migration must leave %s untouched: %+v", id, s)
		}
	}
}

// TestLegacyReopenMigrationUsesSharedAmbiguousCategoryContract pins the SQL
// decision to reviewdec.Preset so a reopened item cannot advertise a different
// contract from a freshly created AMBIGUOUS_CATEGORY review.
func TestLegacyReopenMigrationUsesSharedAmbiguousCategoryContract(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-09-01")
	legacy := f.seedLegacyDisputeExpense(t)
	f.migrate(t, "up")
	var stored []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT decision FROM review_item WHERE transaction_id=$1 AND status='OPEN'`, legacy).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	preset, ok := reviewdec.Preset("AMBIGUOUS_CATEGORY", "transaction", legacy)
	if !ok {
		t.Fatal("no AMBIGUOUS_CATEGORY preset")
	}
	want, err := preset.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var got, expected map[string]any
	if err := json.Unmarshal(stored, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &expected); err != nil {
		t.Fatal(err)
	}
	if got["decisionProvenance"] == nil {
		t.Fatal("migration provenance missing")
	}
	delete(got, "decisionProvenance") // the only field the preset does not own
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("migration decision drifted from reviewdec.Preset:\n got  %v\n want %v", got, expected)
	}
}

// TestLegacyReopenMigrationCycleLeavesNoDanglingPointer covers Up -> Down -> Up
// both for an untouched record and for one the household completed in between.
func TestLegacyReopenMigrationCycleLeavesNoDanglingPointer(t *testing.T) {
	t.Run("untouched", func(t *testing.T) {
		f := cycleReviewFixture(t)
		f.anchor(t, "2026-09-01")
		legacy := f.seedLegacyDisputeExpense(t)
		f.migrate(t, "up")
		if f.pointers(t, legacy) != 1 {
			t.Fatal("up must stamp the pointer")
		}
		f.migrate(t, "down")
		if f.pointers(t, legacy) != 0 {
			t.Fatal("down must clear the pointer")
		}
		f.migrate(t, "up")
		if s := f.surfaces(t, legacy); s.status != "NEEDS_REVIEW" || !s.openReview || f.pointers(t, legacy) != 1 {
			t.Fatalf("second up must reopen once: %+v pointers=%d", s, f.pointers(t, legacy))
		}
	})
	t.Run("household confirmed in between", func(t *testing.T) {
		f := cycleReviewFixture(t)
		f.anchor(t, "2026-09-01")
		legacy := f.seedLegacyDisputeExpense(t)
		f.migrate(t, "up")
		if w := f.webConfirm(t, legacy, f.category); w.Code != http.StatusNoContent {
			t.Fatalf("web confirm: status=%d body=%s", w.Code, w.Body.String())
		}
		f.migrate(t, "down")
		if f.pointers(t, legacy) != 0 {
			t.Fatal("down must clear the pointer even when the household already acted")
		}
		f.migrate(t, "up") // nothing matches any more: must not resurrect the record
		f.migrate(t, "down")
		if s := f.surfaces(t, legacy); !s.ledgerComplete || s.disagreementsInHousehd != 0 || f.pointers(t, legacy) != 0 {
			t.Fatalf("the completed record must stay completed with no pointer: %+v", s)
		}
		var audits int
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='REOPEN_LEGACY_UNCATEGORIZED'`, legacy).Scan(&audits); err != nil || audits != 1 {
			t.Fatalf("up must not have reopened twice: audits=%d err=%v", audits, err)
		}
	})
}
