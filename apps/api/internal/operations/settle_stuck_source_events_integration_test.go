package operations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

const settleMigration = "00084_settle_stuck_source_events.sql"

// settleFixture seeds the five stuck groups of ticket #356 in one household.
type settleFixture struct {
	t          *testing.T
	pool       *pgxpool.Pool
	household  string
	stamp      int64
	seq        int
	groups     map[string][]string // group letter -> source event ids
	matched    string              // the confirmed transaction group E matched
	receivedAt time.Time           // inside the inspected window unless a test moves it
}

func (f *settleFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *settleFixture) next(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d-%d", prefix, f.stamp, f.seq)
}

func (f *settleFixture) event(sourceType, status string, parser *string) string {
	f.t.Helper()
	var id string
	name := f.next("settle")
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,parser_name) VALUES($1,$2,$3,$6,decode(md5($3),'hex'),$4,$5) RETURNING id`, f.household, sourceType, name, status, parser, f.receivedAt).Scan(&id))
	return id
}

func (f *settleFixture) exec(sql string, args ...any) {
	f.t.Helper()
	_, err := f.pool.Exec(context.Background(), sql, args...)
	f.must(err)
}

func (f *settleFixture) document(source, docType, status string) string {
	f.t.Helper()
	var attachment, id string
	name := f.next("att")
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,decode(md5($2),'hex'),'image/jpeg',1,1,1,$2) RETURNING id`, f.household, name).Scan(&attachment))
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,$4,$5) RETURNING id`, f.household, source, attachment, docType, status).Scan(&id))
	return id
}

func (f *settleFixture) item(column, value, reviewType, action string) {
	f.t.Helper()
	f.exec(fmt.Sprintf(`INSERT INTO review_item(household_id,%s,review_type,status,resolution_action,resolved_at,decision) VALUES($1,$2,$3,'RESOLVED',$4,now(),$5::jsonb)`, column), f.household, value, reviewType, action, stuckTestDecision)
}

func seedSettleFixture(t *testing.T, sizes map[string]int) *settleFixture {
	t.Helper()
	pool := integrationPool(t)
	ctx := context.Background()
	f := &settleFixture{t: t, pool: pool, stamp: time.Now().UnixNano(), groups: map[string][]string{}, receivedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
	f.must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Settle %d", f.stamp)).Scan(&f.household))
	var user, listener string
	f.must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Settle','unused') RETURNING id`, fmt.Sprintf("settle-%d@example.test", f.stamp)).Scan(&user))
	f.must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Fixture bank',$2,$3) RETURNING id`, f.household, fmt.Sprintf("bank-%d@example.test", f.stamp), user).Scan(&listener))

	for i := 0; i < sizes["A"]; i++ {
		f.groups["A"] = append(f.groups["A"], f.event("TELEGRAM_TEXT", "NEEDS_REVIEW", nil))
	}
	for i := 0; i < sizes["B"]; i++ {
		id := f.event("BANK_EMAIL", "NEEDS_REVIEW", ptr("bank-email-generic"))
		f.exec(`INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status,policy_result) VALUES($1,$2,'native_tool','v1','{}','INVALID','NEEDS_REVIEW')`, id, listener)
		f.groups["B"] = append(f.groups["B"], id)
	}
	for i := 0; i < sizes["C"]; i++ {
		id := f.event("TELEGRAM_IMAGE", "NEEDS_REVIEW", ptr("cloud-llm-gateway"))
		doc := f.document(id, "WEALTH_OBSERVATION", "EXTRACTED")
		var obs string
		f.must(pool.QueryRow(ctx, `INSERT INTO wealth_observation(household_id,document_id,institution,account_hint,observed_value_idr,status) VALUES($1,$2,'Fixture','x',1000,'APPLIED') RETURNING id`, f.household, doc).Scan(&obs))
		f.item("wealth_observation_id", obs, "MANUAL_CORRECTION", "SNAPSHOT_CREATED")
		f.groups["C"] = append(f.groups["C"], id)
	}
	for i := 0; i < sizes["D"]; i++ {
		id := f.event("TELEGRAM_IMAGE", "NEEDS_REVIEW", ptr("cloud-llm-gateway"))
		f.item("document_id", f.document(id, "EWALLET_SCREENSHOT", "EXTRACTED"), "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", "IGNORE")
		f.groups["D"] = append(f.groups["D"], id)
	}
	if sizes["E"] > 0 {
		f.must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',5000,'IDR',now(),now()) RETURNING id`, f.household).Scan(&f.matched))
		original := f.event("TELEGRAM_IMAGE", "PROCESSED", ptr("cloud-llm-gateway"))
		f.exec(`INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'TRANSACTION_SCREENSHOT')`, f.matched, original)
		var ids []string
		for i := 0; i < sizes["E"]; i++ {
			id := f.event("TELEGRAM_IMAGE", "NEEDS_REVIEW", ptr("cloud-llm-gateway"))
			doc := f.document(id, "TRANSFER_PROOF", "EXTRACTED")
			f.exec(`INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,confidence,proposal_status,metadata_json) VALUES($1,$2,'row-001','EXPENSE',5000,'IDR',now(),1,'NEEDS_REVIEW',jsonb_build_object('document_id',$3::text,'matched_transaction_id',$4::text))`, f.household, id, doc, f.matched)
			ids = append(ids, id)
			f.groups["E"] = append(f.groups["E"], id)
		}
		f.exec(`INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'SYSTEM','MANUAL_UNLINK_EVIDENCE','transaction',$2,jsonb_build_object('source_events',$3::text[]))`, f.household, f.matched, ids)
	}
	return f
}

func ptr(s string) *string { return &s }

var fullSizes = map[string]int{"A": 6, "B": 3, "C": 1, "D": 5, "E": 2}

func (f *settleFixture) migrate(section string) error {
	f.t.Helper()
	raw, err := os.ReadFile("../../../../db/migrations/" + settleMigration)
	if err != nil {
		f.t.Fatal(err)
	}
	up, down, _ := strings.Cut(string(raw), "-- +goose Down")
	part := up
	if section == "down" {
		part = down
	}
	start, end := strings.Index(part, "-- +goose StatementBegin"), strings.Index(part, "-- +goose StatementEnd")
	if start < 0 || end < start {
		f.t.Fatalf("%s has no statement block", section)
	}
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('richmod.repair_household',$1,true)`, f.household); err != nil {
		f.t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, part[start+len("-- +goose StatementBegin"):end]); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (f *settleFixture) mustMigrate(section string) {
	f.t.Helper()
	if err := f.migrate(section); err != nil {
		f.t.Fatalf("migration %s: %v", section, err)
	}
}

func (f *settleFixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	f.must(f.pool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

func (f *settleFixture) statuses() map[string]string {
	f.t.Helper()
	out := map[string]string{}
	rows, err := f.pool.Query(context.Background(), `SELECT id::text,processing_status FROM source_event WHERE household_id=$1`, f.household)
	f.must(err)
	defer rows.Close()
	for rows.Next() {
		var id, status string
		f.must(rows.Scan(&id, &status))
		out[id] = status
	}
	f.must(rows.Err())
	return out
}

func (f *settleFixture) audits(action string) int {
	return f.count(`SELECT count(*) FROM audit_log WHERE household_id=$1 AND action=$2 AND entity_type='source_event'`, f.household, action)
}

func (f *settleFixture) jobs() int {
	return f.count(`SELECT count(*) FROM job WHERE type='PROCESS_TRANSACTION_SCREENSHOT' AND payload_json->>'document_id' IN (SELECT d.id::text FROM document d WHERE d.household_id=$1)`, f.household)
}

func TestSettleStuckSourceEventsRepairsEveryGroupAndLeavesNothingStuck(t *testing.T) {
	f := seedSettleFixture(t, fullSizes)
	ctx := context.Background()
	if stuck, err := StuckSourceEvents(ctx, f.pool, f.household); err != nil || len(stuck) != 17 {
		t.Fatalf("fixture must reproduce 17 stuck events, got %d err=%v", len(stuck), err)
	}
	transactionsBefore := f.count(`SELECT count(*) FROM transaction WHERE household_id=$1`, f.household)
	evidenceBefore := f.count(`SELECT count(*) FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id WHERE t.household_id=$1`, f.household)
	reviewsBefore := f.count(`SELECT count(*) FROM review_item WHERE household_id=$1`, f.household)

	f.mustMigrate("up")

	status := f.statuses()
	for _, id := range f.groups["A"] {
		if status[id] != "PROCESSED" {
			t.Fatalf("group A %s: %s", id, status[id])
		}
	}
	for _, id := range f.groups["C"] {
		if status[id] != "PROCESSED" {
			t.Fatalf("group C %s: %s", id, status[id])
		}
	}
	for _, id := range f.groups["B"] {
		if status[id] != "FAILED" {
			t.Fatalf("group B %s: %s", id, status[id])
		}
	}
	for _, id := range f.groups["D"] {
		if status[id] != "IGNORED" || f.count(`SELECT count(*) FROM document WHERE source_event_id=$1 AND status='EXTRACTED'`, id) != 1 {
			t.Fatalf("group D %s: %s", id, status[id])
		}
	}
	for _, id := range f.groups["E"] {
		if status[id] != "PROCESSING" ||
			f.count(`SELECT count(*) FROM document WHERE source_event_id=$1 AND status='CLASSIFIED'`, id) != 1 ||
			f.count(`SELECT count(*) FROM transaction_proposal WHERE source_event_id=$1 AND proposal_status='REJECTED' AND proposal_key='superseded-row-001'`, id) != 1 {
			t.Fatalf("group E %s not staged for reprocess: %s", id, status[id])
		}
	}
	if got := f.jobs(); got != 2 {
		t.Fatalf("group E must queue exactly one job per document, got %d", got)
	}
	// Canonical ledger state is untouched: the repair writes no transaction, evidence or review row.
	if f.count(`SELECT count(*) FROM transaction WHERE household_id=$1`, f.household) != transactionsBefore ||
		f.count(`SELECT count(*) FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id WHERE t.household_id=$1`, f.household) != evidenceBefore ||
		f.count(`SELECT count(*) FROM review_item WHERE household_id=$1`, f.household) != reviewsBefore ||
		f.count(`SELECT count(*) FROM transaction WHERE id=$1 AND status='CONFIRMED'`, f.matched) != 1 {
		t.Fatal("repair changed canonical state")
	}
	if got := f.audits("REPAIR_STUCK_SOURCE_EVENT"); got != 17 {
		t.Fatalf("want one audit row per event (17), got %d", got)
	}

	// Group B's Tindakan item is exactly what the worker writes for a BANK_EMAIL / INVALID failure.
	title, description := reviewdomain.FailedSourceCopy("BANK_EMAIL", "INVALID")
	for _, id := range f.groups["B"] {
		if f.count(`SELECT count(*) FROM integration_action WHERE household_id=$1 AND integration_type=$2 AND action_type=$3 AND status='OPEN' AND dedupe_key=$4 AND title=$5 AND description=$6 AND metadata_json->>'source_event_id'=$4 AND metadata_json->>'source_type'='BANK_EMAIL' AND metadata_json->>'reason'='INVALID'`,
			f.household, reviewdomain.FailedSourceIntegrationType, reviewdomain.FailedSourceActionType, id, title, description) != 1 {
			t.Fatalf("group B %s lacks the failed-source item", id)
		}
	}

	// Group E is waiting on its reprocess job, which is not "stuck"; everything else is settled.
	if stuck, err := StuckSourceEvents(ctx, f.pool, f.household); err != nil || len(stuck) != 0 {
		t.Fatalf("detection query must return zero rows after the repair, got %+v err=%v", stuck, err)
	}
}

func TestSettleStuckSourceEventsRerunIsANoOp(t *testing.T) {
	f := seedSettleFixture(t, fullSizes)
	f.mustMigrate("up")
	before := f.statuses()
	f.mustMigrate("up")
	f.mustMigrate("up")
	after := f.statuses()
	for id, status := range before {
		if after[id] != status {
			t.Fatalf("%s changed on re-run: %s -> %s", id, status, after[id])
		}
	}
	if f.audits("REPAIR_STUCK_SOURCE_EVENT") != 17 || f.jobs() != 2 ||
		f.count(`SELECT count(*) FROM integration_action WHERE household_id=$1`, f.household) != 3 {
		t.Fatal("re-run must not add audit rows, jobs or Tindakan items")
	}
}

func TestSettleStuckSourceEventsRefusesAGroupOfTheWrongSize(t *testing.T) {
	for name, tc := range map[string]struct {
		group string
		size  int
		want  string
	}{
		"group A short":  {"A", 5, "group A: 5 events match, expected 6"},
		"group A excess": {"A", 7, "group A: 7 events match, expected 6"},
		"group B short":  {"B", 2, "group B: 2 events match, expected 3"},
		"group D excess": {"D", 6, "group D: 6 events match, expected 5"},
		"group E short":  {"E", 1, "group E: 1 events match, expected 2"},
	} {
		t.Run(name, func(t *testing.T) {
			sizes := map[string]int{tc.group: tc.size}
			f := seedSettleFixture(t, sizes)
			err := f.migrate("up")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			for _, ids := range f.groups {
				for _, id := range ids {
					if status := f.statuses()[id]; status != "NEEDS_REVIEW" {
						t.Fatalf("a refused migration must roll back; %s is %s", id, status)
					}
				}
			}
			if f.audits("REPAIR_STUCK_SOURCE_EVENT") != 0 || f.jobs() != 0 {
				t.Fatal("a refused migration must leave no audit rows or jobs")
			}
		})
	}
}

func TestSettleStuckSourceEventsIsANoOpWhenNothingMatches(t *testing.T) {
	f := seedSettleFixture(t, map[string]int{})
	if err := f.migrate("up"); err != nil {
		t.Fatalf("empty groups must be a no-op: %v", err)
	}
	if err := f.migrate("down"); err != nil {
		t.Fatalf("down on an empty database must be a no-op: %v", err)
	}
}

func TestSettleStuckSourceEventsDownRestoresAndUpRunsAgain(t *testing.T) {
	f := seedSettleFixture(t, fullSizes)
	original := f.statuses()
	f.mustMigrate("up")
	f.mustMigrate("down")
	for id, status := range f.statuses() {
		if original[id] != status {
			t.Fatalf("%s not restored: %s -> %s", id, original[id], status)
		}
	}
	if f.jobs() != 0 || f.count(`SELECT count(*) FROM integration_action WHERE household_id=$1`, f.household) != 0 ||
		f.count(`SELECT count(*) FROM transaction_proposal WHERE household_id=$1 AND proposal_status='NEEDS_REVIEW' AND proposal_key='row-001'`, f.household) != 2 ||
		f.count(`SELECT count(*) FROM document WHERE household_id=$1 AND status='EXTRACTED'`, f.household) != 8 {
		t.Fatal("down must restore proposals, documents and remove the queued jobs and Tindakan items")
	}
	if f.audits("ROLLBACK_REPAIR_STUCK_SOURCE_EVENT") != 17 {
		t.Fatalf("one rollback audit row per event, got %d", f.audits("ROLLBACK_REPAIR_STUCK_SOURCE_EVENT"))
	}
	f.mustMigrate("down") // second down: nothing left to roll back
	if f.audits("ROLLBACK_REPAIR_STUCK_SOURCE_EVENT") != 17 {
		t.Fatal("second down must be a no-op")
	}
	f.mustMigrate("up") // Up -> Down -> Up settles everything again
	if stuck, err := StuckSourceEvents(context.Background(), f.pool, f.household); err != nil || len(stuck) != 0 || f.jobs() != 2 {
		t.Fatalf("second up must settle again: stuck=%d jobs=%d err=%v", len(stuck), f.jobs(), err)
	}
}

func TestSettleStuckSourceEventsDownKeepsAReprocessThatAlreadyRan(t *testing.T) {
	f := seedSettleFixture(t, fullSizes)
	f.mustMigrate("up")
	// The worker picked the jobs up: their outcome is real and must survive a rollback.
	f.exec(`UPDATE job SET status='SUCCEEDED' WHERE type='PROCESS_TRANSACTION_SCREENSHOT' AND payload_json->>'document_id' IN (SELECT d.id::text FROM document d WHERE d.household_id=$1)`, f.household)
	f.mustMigrate("down")
	status := f.statuses()
	for _, id := range f.groups["E"] {
		if status[id] != "PROCESSING" {
			t.Fatalf("down must not undo a reprocess that already ran: %s is %s", id, status[id])
		}
	}
	for _, id := range f.groups["A"] {
		if status[id] != "NEEDS_REVIEW" {
			t.Fatalf("other groups still roll back: %s is %s", id, status[id])
		}
	}
}

func TestSettleStuckSourceEventsLeavesLaterEventsAlone(t *testing.T) {
	f := seedSettleFixture(t, fullSizes)
	// Group A's cause is still live in code: a chat reply received after the inspected
	// batch must neither block the migration nor be touched by it.
	f.receivedAt = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	late := f.event("TELEGRAM_TEXT", "NEEDS_REVIEW", nil)
	f.mustMigrate("up")
	if got := f.statuses()[late]; got != "NEEDS_REVIEW" {
		t.Fatalf("an event after the cutoff must be left alone, got %s", got)
	}
	stuck, err := StuckSourceEvents(context.Background(), f.pool, f.household)
	if err != nil || len(stuck) != 1 || stuck[0].SourceEventID != late {
		t.Fatalf("the late event stays visible to the detection query: %+v err=%v", stuck, err)
	}
}

func TestSettleStuckSourceEventsSettlesAlbumPageMembers(t *testing.T) {
	sizes := map[string]int{"A": 6, "B": 3, "C": 1, "D": 4, "E": 2}
	f := seedSettleFixture(t, sizes)
	// A fifth group D event is an album member: only the primary owns the document.
	member := f.event("TELEGRAM_IMAGE", "NEEDS_REVIEW", ptr("cloud-llm-gateway"))
	var doc, attachment string
	f.must(f.pool.QueryRow(context.Background(), `SELECT d.id::text,d.attachment_id::text FROM document d WHERE d.source_event_id=$1`, f.groups["D"][0]).Scan(&doc, &attachment))
	f.exec(`INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,1)`, doc, member, attachment)
	f.groups["D"] = append(f.groups["D"], member)
	f.mustMigrate("up")
	if got := f.statuses()[member]; got != "IGNORED" {
		t.Fatalf("album member must settle with its document: %s", got)
	}
	if stuck, err := StuckSourceEvents(context.Background(), f.pool, f.household); err != nil || len(stuck) != 0 {
		t.Fatalf("detection must be zero: %+v err=%v", stuck, err)
	}
}

func TestSettleStuckSourceEventsDownRestoresBankEmailWithAPreexistingItem(t *testing.T) {
	f := seedSettleFixture(t, fullSizes)
	existing := f.groups["B"][0]
	// A failed-source item for this event already exists, so Up's insert is a no-op.
	f.exec(`INSERT INTO integration_action(household_id,integration_type,action_type,status,title,dedupe_key,metadata_json) VALUES($1,'SOURCE_PROCESSING','SOURCE_FAILED','OPEN','existing',$2,'{}')`, f.household, existing)
	f.mustMigrate("up")
	f.mustMigrate("down")
	if got := f.statuses()[existing]; got != "NEEDS_REVIEW" {
		t.Fatalf("rollback must restore the event even when it did not write the item: %s", got)
	}
	if f.count(`SELECT count(*) FROM integration_action WHERE household_id=$1 AND dedupe_key=$2 AND title='existing'`, f.household, existing) != 1 {
		t.Fatal("rollback must keep an item the migration did not write")
	}
}
