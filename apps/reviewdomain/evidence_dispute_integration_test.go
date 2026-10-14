package reviewdomain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type disputeFixture struct {
	household, user, document, source, proposal, evidence, original, category string
}

func seedMergedScreenshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) disputeFixture {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := fmt.Sprintf("%s-%d", label, time.Now().UnixNano())
	var f disputeFixture
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id::text`, "Dispute "+stamp).Scan(&f.household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Dispute Owner','unused') RETURNING id::text`, stamp+"@example.test").Scan(&f.user))
	_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, f.household, f.user)
	must(err)
	var attachment string
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Makanan','food') RETURNING id::text`, f.household).Scan(&f.category))
	must(pool.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',10,1,1,$3) RETURNING id::text`, f.household, []byte(stamp), stamp+".png").Scan(&attachment))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',7500,'IDR',$2,now()) RETURNING id::text`, f.household, time.Date(2026, 10, 12, 2, 48, 0, 0, time.UTC)).Scan(&f.original))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSED') RETURNING id::text`, f.household, "img-"+stamp, []byte("img-"+stamp)).Scan(&f.source))
	must(pool.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'EWALLET_SCREENSHOT','EXTRACTED') RETURNING id::text`, f.household, f.source, attachment).Scan(&f.document))
	must(pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,confidence,proposal_status,metadata_json)
		VALUES($1,$2,'row-001','EXPENSE',7500,'IDR',$3,'Test Merchant',.99,'MERGED',jsonb_build_object('document_id',$4::uuid,'row_index',0,'date_known',true,'matched_transaction_id',$5::uuid,'match_score',.95)) RETURNING id::text`,
		f.household, f.source, time.Date(2026, 10, 12, 2, 38, 0, 0, time.UTC), f.document, f.original).Scan(&f.proposal))
	must(pool.QueryRow(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TRANSACTION_SCREENSHOT',.99,jsonb_build_object('proposal_id',$3::uuid,'document_id',$4::uuid,'row_index',0,'match_score',.95)) RETURNING id::text`, f.original, f.source, f.proposal, f.document).Scan(&f.evidence))
	return f
}

func stageDisputeForTest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f disputeFixture) (EvidenceDisputeResult, string, string) {
	t.Helper()
	var chatEvent string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'PROCESSING') RETURNING id::text`, f.household, fmt.Sprintf("dispute-%s-%d", f.source, time.Now().UnixNano()), []byte(fmt.Sprintf("dispute-%d", time.Now().UnixNano()))).Scan(&chatEvent); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	staged, err := StageEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: f.household, ActorUserID: f.user, DocumentID: f.document, SourceEventID: chatEvent, CategoryID: f.category})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	var item, request string
	if staged.Created {
		decision := `{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","decisionClass":"DUPLICATE_AMBIGUITY","missingFacts":["duplicate_relationship"],"allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"],"decisionProvenance":{"evidence_link_dispute":true}}`
		if err = tx.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb) RETURNING id::text`, f.household, staged.TransactionID, decision).Scan(&item); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRow(ctx, `INSERT INTO review_request(household_id,review_item_id,transaction_id,review_type,status) VALUES($1,$2,$3,'POSSIBLE_DUPLICATE','OPEN') RETURNING id::text`, f.household, item, staged.TransactionID).Scan(&request); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO review_conversation(review_request_id,state) VALUES($1,'AWAITING_DETAIL')`, request); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return staged, item, request
}

func TestEvidenceDisputeStagesThenHumanConfirmationSupersedesLinkWithoutLosingEvidence(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	f := seedMergedScreenshot(t, ctx, pool, "confirm")
	staged, item, request := stageDisputeForTest(t, ctx, pool, f)
	if !staged.Created || staged.Status != "NEEDS_REVIEW" {
		t.Fatalf("staged=%#v", staged)
	}
	var originalStatus, proposalStatus string
	if err = pool.QueryRow(ctx, `SELECT t.status,p.proposal_status FROM transaction t,transaction_proposal p WHERE t.id=$1 AND p.id=$2`, f.original, f.proposal).Scan(&originalStatus, &proposalStatus); err != nil || originalStatus != "CONFIRMED" || proposalStatus != "MERGED" {
		t.Fatalf("staging mutated original: %v %s %s", err, originalStatus, proposalStatus)
	}
	var confirmedCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.household).Scan(&confirmedCount); err != nil || confirmedCount != 1 {
		t.Fatalf("staging must not confirm a second transaction: %v %d", err, confirmedCount)
	}

	// Retry is idempotent: no second staged row.
	again, _, _ := stageDisputeForTest(t, ctx, pool, f)
	if again.Created || again.TransactionID != staged.TransactionID {
		t.Fatalf("retry staged again: %#v", again)
	}

	// Generic confirm / merge cannot bypass the dedicated confirmation.
	generic := func(run func(tx pgx.Tx) error) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		return run(tx)
	}
	if err = generic(func(tx pgx.Tx) error {
		_, e := ConfirmTransactionReview(ctx, tx, ConfirmCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: staged.TransactionID, Action: "CONFIRM_REVIEW"})
		return e
	}); !errors.Is(err, ErrEvidenceDisputeConfirmationRequired) {
		t.Fatalf("generic confirm err=%v", err)
	}
	if err = generic(func(tx pgx.Tx) error {
		_, e := MergeDuplicateReview(ctx, tx, DuplicateCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: staged.TransactionID, TargetTransactionID: f.original})
		return e
	}); !errors.Is(err, ErrEvidenceDisputeConfirmationRequired) {
		t.Fatalf("generic merge err=%v", err)
	}

	// Another household cannot stage or resolve against this evidence.
	other := seedMergedScreenshot(t, ctx, pool, "other")
	if err = generic(func(tx pgx.Tx) error {
		_, e := StageEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: other.household, ActorUserID: other.user, DocumentID: f.document, SourceEventID: f.source})
		return e
	}); !errors.Is(err, ErrEvidenceDisputeInvalid) {
		t.Fatalf("cross-household stage err=%v", err)
	}
	if err = generic(func(tx pgx.Tx) error {
		return ResolveEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: other.household, ActorUserID: other.user, TransactionID: staged.TransactionID, ReviewItemID: item, RequestID: request}, true)
	}); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("cross-household resolve err=%v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = ResolveEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: staged.TransactionID, ReviewItemID: item, RequestID: request}, true); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("confirm dispute: %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var newStatus, linkStatus, origProposal string
	var activeScreenshotLinks, totalScreenshotLinks, confirmed int
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, staged.TransactionID).Scan(&newStatus))
	must(pool.QueryRow(ctx, `SELECT metadata_json->>'link_status' FROM transaction_evidence WHERE id=$1`, f.evidence).Scan(&linkStatus))
	must(pool.QueryRow(ctx, `SELECT proposal_status FROM transaction_proposal WHERE id=$1`, f.proposal).Scan(&origProposal))
	must(pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE'),count(*) FROM transaction_evidence WHERE source_event_id=$1 AND evidence_type='TRANSACTION_SCREENSHOT'`, f.source).Scan(&activeScreenshotLinks, &totalScreenshotLinks))
	must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.household).Scan(&confirmed))
	if newStatus != "CONFIRMED" || linkStatus != "SUPERSEDED" || origProposal != "MERGED" || activeScreenshotLinks != 1 || totalScreenshotLinks != 2 || confirmed != 2 {
		t.Fatalf("new=%s link=%s proposal=%s active=%d total=%d confirmed=%d", newStatus, linkStatus, origProposal, activeScreenshotLinks, totalScreenshotLinks, confirmed)
	}
	// A repeated confirmation callback is stale and mutates nothing.
	if err = generic(func(tx pgx.Tx) error {
		return ResolveEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: staged.TransactionID, ReviewItemID: item, RequestID: request}, true)
	}); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("repeat confirm err=%v", err)
	}
}

func TestEvidenceDisputeRejectKeepsOriginalLinkAndRecordsNothing(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	f := seedMergedScreenshot(t, ctx, pool, "reject")
	staged, item, request := stageDisputeForTest(t, ctx, pool, f)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = ResolveEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: staged.TransactionID, ReviewItemID: item, RequestID: request}, false); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("reject: %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var stagedStatus string
	var activeLinks, confirmed int
	if err = pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, staged.TransactionID).Scan(&stagedStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE' AND transaction_id=$2) FROM transaction_evidence WHERE source_event_id=$1 AND evidence_type='TRANSACTION_SCREENSHOT'`, f.source, f.original).Scan(&activeLinks); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.household).Scan(&confirmed); err != nil {
		t.Fatal(err)
	}
	if stagedStatus != "VOIDED" || activeLinks != 1 || confirmed != 1 {
		t.Fatalf("staged=%s activeOriginalLinks=%d confirmed=%d", stagedStatus, activeLinks, confirmed)
	}
}

func TestEvidenceDisputeFailsClosedForMultiRowOrUnknownDate(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	f := seedMergedScreenshot(t, ctx, pool, "multirow")
	if _, err = pool.Exec(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,confidence,proposal_status,metadata_json) VALUES($1,$2,'row-002','EXPENSE',9000,'IDR',now(),.9,'NEEDS_REVIEW',jsonb_build_object('document_id',$3::uuid,'row_index',1))`, f.household, f.source, f.document); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = StageEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: f.household, ActorUserID: f.user, DocumentID: f.document, SourceEventID: f.source}); !errors.Is(err, ErrEvidenceDisputeInvalid) {
		t.Fatalf("multi-row dispute err=%v", err)
	}
}

// Only expense evidence may be disputed: an incoming screenshot row may be an
// own-account transfer and never confirms without transfer classification (ADR-042).
func TestEvidenceDisputeRefusesIncomeRows(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	f := seedMergedScreenshot(t, ctx, pool, "income")
	if _, err = pool.Exec(ctx, `UPDATE transaction_proposal SET proposed_type='INCOME' WHERE id=$1`, f.proposal); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = StageEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: f.household, ActorUserID: f.user, DocumentID: f.document, SourceEventID: f.source}); !errors.Is(err, ErrEvidenceDisputeInvalid) {
		t.Fatalf("INCOME dispute err=%v, want ErrEvidenceDisputeInvalid", err)
	}
}

// An expense dispute never confirms without a category: unresolved staging
// needs the household's explicit, household-valid choice, and a refused attempt
// mutates nothing.
func TestEvidenceDisputeConfirmRequiresAValidHouseholdCategory(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	f := seedMergedScreenshot(t, ctx, pool, "needs-category")
	unresolved := f
	unresolved.category = ""
	staged, item, request := stageDisputeForTest(t, ctx, pool, unresolved)
	foreign := seedMergedScreenshot(t, ctx, pool, "foreign-category")

	resolve := func(category string) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = ResolveEvidenceDispute(ctx, tx, EvidenceDisputeCommand{HouseholdID: f.household, ActorUserID: f.user, TransactionID: staged.TransactionID, ReviewItemID: item, RequestID: request, CategoryID: category}, true); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	for name, category := range map[string]string{"no category": "", "another household's category": foreign.category} {
		err := resolve(category)
		if name == "no category" && !errors.Is(err, ErrMissingCategory) || name != "no category" && !errors.Is(err, ErrInvalidCandidate) {
			t.Fatalf("%s: err=%v", name, err)
		}
		var status, link string
		if err = pool.QueryRow(ctx, `SELECT t.status,COALESCE(e.metadata_json->>'link_status','ACTIVE') FROM transaction t,transaction_evidence e WHERE t.id=$1 AND e.id=$2`, staged.TransactionID, f.evidence).Scan(&status, &link); err != nil {
			t.Fatal(err)
		}
		if status != "NEEDS_REVIEW" || link != "ACTIVE" {
			t.Fatalf("%s mutated state: %s/%s", name, status, link)
		}
	}
	if err = resolve(f.category); err != nil {
		t.Fatalf("household-chosen category: %v", err)
	}
	var category string
	var classification *float64
	var merchant *string
	if err = pool.QueryRow(ctx, `SELECT category_id::text,classification_confidence::float8,merchant_id::text FROM transaction WHERE id=$1`, staged.TransactionID).Scan(&category, &classification, &merchant); err != nil {
		t.Fatal(err)
	}
	if category != f.category || classification == nil || *classification != 1 || merchant == nil {
		t.Fatalf("category=%s classification=%v merchant=%v", category, classification, merchant)
	}
}
