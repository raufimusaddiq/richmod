package operations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const staleDocMigration = "00099_settle_stale_needs_review_documents.sql"

type staleDocFixture struct {
	t     *testing.T
	pool  *pgxpool.Pool
	stamp int64
	seq   int
}

func (f *staleDocFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *staleDocFixture) exec(sql string, args ...any) {
	f.t.Helper()
	_, err := f.pool.Exec(context.Background(), sql, args...)
	f.must(err)
}

func (f *staleDocFixture) name(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d-%d", prefix, f.stamp, f.seq)
}

func (f *staleDocFixture) household() string {
	var id string
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO household(name) VALUES($1) RETURNING id`, f.name("stale-doc")).Scan(&id))
	return id
}

func (f *staleDocFixture) source(household string) string {
	var id string
	n := f.name("src")
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),decode(md5($2),'hex'),'NEEDS_REVIEW') RETURNING id`, household, n).Scan(&id))
	return id
}

func (f *staleDocFixture) document(household, source, status string) (string, string) {
	var attachment, id string
	n := f.name("att")
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,decode(md5($2),'hex'),'image/jpeg',1,1,1,$2) RETURNING id`, household, n).Scan(&attachment))
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'RECEIPT',$4) RETURNING id`, household, source, attachment, status).Scan(&id))
	return id, attachment
}

func (f *staleDocFixture) transaction(household, status, source string) string {
	var id string
	f.must(f.pool.QueryRow(context.Background(), `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE',$2,34000,'IDR',now(),CASE WHEN $2='CONFIRMED' THEN now() END) RETURNING id`, household, status).Scan(&id))
	f.exec(`INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'RECEIPT_IMAGE')`, id, source)
	return id
}

func (f *staleDocFixture) openItem(household, column, value string) {
	f.exec(fmt.Sprintf(`INSERT INTO review_item(household_id,%s,review_type,status,decision) VALUES($1,$2,'MANUAL_CORRECTION','OPEN',$3::jsonb)`, column), household, value, stuckTestDecision)
}

func (f *staleDocFixture) migrate(section, household string) {
	f.t.Helper()
	raw, err := os.ReadFile("../../../../db/migrations/" + staleDocMigration)
	f.must(err)
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
	f.must(err)
	defer tx.Rollback(ctx)
	// The test database is shared with other tests, so every run is household-scoped.
	_, err = tx.Exec(ctx, `SELECT set_config('richmod.repair_household',$1,true)`, household)
	f.must(err)
	_, err = tx.Exec(ctx, part[start+len("-- +goose StatementBegin"):end])
	f.must(err)
	f.must(tx.Commit(ctx))
}

func (f *staleDocFixture) status(doc string) string {
	var s string
	f.must(f.pool.QueryRow(context.Background(), `SELECT status FROM document WHERE id=$1`, doc).Scan(&s))
	return s
}

func (f *staleDocFixture) count(sql string, args ...any) int {
	var n int
	f.must(f.pool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

func TestSettleStaleNeedsReviewDocuments(t *testing.T) {
	f := &staleDocFixture{t: t, pool: integrationPool(t), stamp: time.Now().UnixNano()}
	hh, other := f.household(), f.household()

	stuck := map[string]string{}
	pending := map[string]string{}
	newDoc := func(household, status string) (string, string) {
		s := f.source(household)
		d, _ := f.document(household, s, status)
		return s, d
	}

	_, d := newDoc(hh, "NEEDS_REVIEW")
	stuck["bare"] = d
	s, d := newDoc(hh, "NEEDS_REVIEW")
	f.transaction(hh, "CONFIRMED", s)
	stuck["confirmed transaction (N5)"] = d
	_, d = newDoc(hh, "NEEDS_REVIEW")
	f.openItem(hh, "document_id", d)
	f.exec(`UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='IGNORE' WHERE document_id=$1`, d)
	stuck["resolved review"] = d

	_, d = newDoc(hh, "NEEDS_REVIEW")
	f.openItem(hh, "document_id", d)
	pending["open review on document"] = d
	s, d = newDoc(hh, "NEEDS_REVIEW")
	f.openItem(hh, "source_event_id", s)
	pending["open review on source"] = d
	s, d = newDoc(hh, "NEEDS_REVIEW")
	f.openItem(hh, "transaction_id", f.transaction(hh, "CONFIRMED", s))
	pending["open review on transaction"] = d
	s, d = newDoc(hh, "NEEDS_REVIEW")
	f.transaction(hh, "NEEDS_REVIEW", s)
	pending["NEEDS_REVIEW transaction"] = d
	s, d = newDoc(hh, "NEEDS_REVIEW")
	f.exec(`INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,confidence,proposal_status) VALUES($1,$2,'row-001','EXPENSE',5000,'IDR',now(),1,'NEEDS_REVIEW')`, hh, s)
	pending["NEEDS_REVIEW proposal"] = d
	_, d = newDoc(hh, "NEEDS_REVIEW")
	f.exec(`INSERT INTO wealth_observation(household_id,document_id,institution,account_hint,observed_value_idr,status) VALUES($1,$2,'Fixture','x',1000,'PENDING')`, hh, d)
	pending["PENDING wealth observation"] = d
	// Album: the open review is bound to a secondary page source, not the document's own.
	s = f.source(hh)
	d, attachment := f.document(hh, s, "NEEDS_REVIEW")
	secondary := f.source(hh)
	f.exec(`INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,0)`, d, secondary, attachment)
	f.openItem(hh, "source_event_id", secondary)
	pending["open review on album page source"] = d
	_, d = newDoc(hh, "FAILED")
	pending["FAILED document is not NEEDS_REVIEW and stays"] = d

	_, otherStuck := newDoc(other, "NEEDS_REVIEW")

	txBefore := f.count(`SELECT count(*) FROM transaction WHERE household_id=$1`, hh)
	evBefore := f.count(`SELECT count(*) FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id WHERE t.household_id=$1`, hh)
	riBefore := f.count(`SELECT count(*) FROM review_item WHERE household_id=$1`, hh)
	check := func(when, wantStuck string) {
		t.Helper()
		for name, id := range stuck {
			if got := f.status(id); got != wantStuck {
				t.Fatalf("%s: stuck %q is %s, want %s", when, name, got, wantStuck)
			}
		}
		for name, id := range pending {
			want := "NEEDS_REVIEW"
			if strings.HasPrefix(name, "FAILED") {
				want = "FAILED"
			}
			if got := f.status(id); got != want {
				t.Fatalf("%s: non-stuck %q is %s, want %s", when, name, got, want)
			}
		}
	}
	audits := func() int {
		return f.count(`SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='REPAIR_STALE_DOCUMENT_REVIEW'`, hh)
	}

	f.migrate("up", hh)
	check("after Up", "EXTRACTED")
	if audits() != len(stuck) {
		t.Fatalf("want %d audit rows, got %d", len(stuck), audits())
	}
	if got := f.status(otherStuck); got != "NEEDS_REVIEW" {
		t.Fatalf("scoped run touched another household: %s", got)
	}
	if f.count(`SELECT count(*) FROM transaction WHERE household_id=$1`, hh) != txBefore ||
		f.count(`SELECT count(*) FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id WHERE t.household_id=$1`, hh) != evBefore ||
		f.count(`SELECT count(*) FROM review_item WHERE household_id=$1`, hh) != riBefore {
		t.Fatal("repair changed canonical state")
	}

	f.migrate("up", hh)
	if audits() != len(stuck) {
		t.Fatal("second Up wrote more audit rows")
	}
	f.migrate("down", hh)
	check("after Down", "NEEDS_REVIEW")
	if got := f.status(otherStuck); got != "NEEDS_REVIEW" {
		t.Fatal("Down touched another household")
	}
	f.migrate("down", hh)
	check("after second Down", "NEEDS_REVIEW")
	f.migrate("up", hh)
	check("after Up again", "EXTRACTED")

	f.migrate("up", other)
	if got := f.status(otherStuck); got != "EXTRACTED" {
		t.Fatalf("scoped run for the other household: %s", got)
	}
}
