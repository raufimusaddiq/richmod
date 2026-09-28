package review

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

func TestPayslipFinalizerLinksExactDuplicateAndRejectsConflict(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	household, user, _ := seedTransferReviewOwner(t, pool, stamp)
	if _, err := pool.Exec(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Existing','existing',true)`, household, user); err != nil {
		t.Fatal(err)
	}
	seed := func(suffix string, amount int, date string) (source, proposal string) {
		t.Helper()
		external := fmt.Sprintf("payslip-%s-%d", suffix, stamp)
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, external, []byte(external)).Scan(&source); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,counterparty_raw,description,confidence,proposal_status,metadata_json) VALUES($1,$2,'INCOME',$3,'IDR',$4,'Employer','Penghasilan dari slip gaji',.96,'NEEDS_REVIEW','{"period":"2026-08"}'::jsonb) RETURNING id`, household, source, amount, date).Scan(&proposal); err != nil {
			t.Fatal(err)
		}
		return
	}
	finalize := func(source, proposal string) error {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		_, err = reviewdomain.FinalizePayslip(ctx, tx, reviewdomain.PayslipFinalization{HouseholdID: household, ProposalID: proposal, SourceEventID: source, Choice: "HOUSEHOLD_POLICY", Auto: true})
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	first, firstProposal := seed("first", 16000000, "2026-09-25")
	if err := finalize(first, firstProposal); err != nil {
		t.Fatal(err)
	}
	duplicate, duplicateProposal := seed("duplicate", 16000000, "2026-09-25")
	if err := finalize(duplicate, duplicateProposal); err != nil {
		t.Fatal(err)
	}
	conflict, conflictProposal := seed("conflict", 17000000, "2026-09-25")
	if err := finalize(conflict, conflictProposal); err != reviewdomain.ErrPayslipReviewInvalid {
		t.Fatalf("conflicting salary must fail closed: %v", err)
	}
	var incomes, events, evidence, conflictEvidence int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM transaction WHERE household_id=$1 AND type='INCOME'),
		(SELECT count(*) FROM salary_event WHERE household_id=$1),
		(SELECT count(*) FROM transaction_evidence WHERE source_event_id IN ($2,$3)),
		(SELECT count(*) FROM transaction_evidence WHERE source_event_id=$4)`, household, first, duplicate, conflict).Scan(&incomes, &events, &evidence, &conflictEvidence); err != nil {
		t.Fatal(err)
	}
	if incomes != 1 || events != 1 || evidence != 2 || conflictEvidence != 0 {
		t.Fatalf("salary state incomes=%d events=%d evidence=%d conflict=%d", incomes, events, evidence, conflictEvidence)
	}
}
