package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResidualCatchUpIsNotBlockedByAnotherHouseholdsSettledCycle(t *testing.T) {
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
	seedCycle := func(label string) (household, endEvent string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("%s %d", label, stamp)).Scan(&household); err != nil {
			t.Fatal(err)
		}
		var source string
		if err := pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,employer,normalized_employer,is_primary) VALUES($1,'PT Contoh','pt contoh',true) RETURNING id`, household).Scan(&source); err != nil {
			t.Fatal(err)
		}
		for i, payDate := range []string{"2026-08-25", "2026-09-25"} {
			var sourceEvent, transaction, event string
			if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),'\x01','PROCESSED') RETURNING id`, household, fmt.Sprintf("residual-catchup-%d-%s-%d", stamp, label, i)).Scan(&sourceEvent); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,'IDR',$2::date,now()) RETURNING id`, household, payDate).Scan(&transaction); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,date_trunc('month',$3::date),$3::date,1000000,$4,'CONFIRMED',$5) RETURNING id`, source, household, payDate, transaction, sourceEvent).Scan(&event); err != nil {
				t.Fatal(err)
			}
			endEvent = event
		}
		return household, endEvent
	}
	householdA, endA := seedCycle("Residual A")
	householdB, endB := seedCycle("Residual B")
	if _, err := pool.Exec(ctx, `INSERT INTO job(type,household_id,status,payload_json,max_attempts,finished_at) VALUES('GENERATE_CYCLE_RESIDUAL_REVIEW',$1,'SUCCEEDED',jsonb_build_object('household_id',$1::uuid,'end_salary_event_id',$2::uuid),5,now())`, householdA, endA); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, residualCatchUpSQL, 10000); err != nil {
		t.Fatalf("catch-up failed: %v", err)
	}
	var jobsA, jobsB int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM job WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'end_salary_event_id'=$1),
		(SELECT count(*) FROM job WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'end_salary_event_id'=$2 AND status='PENDING' AND household_id=$3)`, endA, endB, householdB).Scan(&jobsA, &jobsB); err != nil {
		t.Fatal(err)
	}
	if jobsA != 1 || jobsB != 1 {
		t.Fatalf("jobs: household A=%d (want its one settled job), household B=%d (want one pending)", jobsA, jobsB)
	}
	if _, err := pool.Exec(ctx, residualCatchUpSQL, 10000); err != nil {
		t.Fatalf("second catch-up failed: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'end_salary_event_id'=$1`, endB).Scan(&jobsB); err != nil || jobsB != 1 {
		t.Fatalf("household B jobs after second pass = %d (%v)", jobsB, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE job SET status='FAILED',finished_at=now() WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'end_salary_event_id'=$1`, endB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, residualCatchUpSQL, 10000); err != nil {
		t.Fatalf("catch-up after permanent failure failed: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND payload_json->>'end_salary_event_id'=$1`, endB).Scan(&jobsB); err != nil || jobsB != 1 {
		t.Fatalf("household B jobs after a FAILED residual review = %d (%v), want no re-enqueue", jobsB, err)
	}
}

func TestPruneTerminalJobsKeepsResidualReviewsAndPrunesTheirDependents(t *testing.T) {
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
	var household string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Prune %d", time.Now().UnixNano())).Scan(&household); err != nil {
		t.Fatal(err)
	}
	var terminalJob, residualJob string
	if err := pool.QueryRow(ctx, `INSERT INTO job(type,household_id,status,payload_json,finished_at) VALUES('PROCESS_DOCUMENT',$1,'SUCCEEDED','{}',now()-interval '40 days') RETURNING id`, household).Scan(&terminalJob); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO job(type,household_id,status,payload_json,finished_at) VALUES('GENERATE_CYCLE_RESIDUAL_REVIEW',$1,'SUCCEEDED','{}',now()-interval '40 days') RETURNING id`, household).Scan(&residualJob); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO job_retry_log(job_id,attempt,lane,job_type,error_class) VALUES($1,1,'BACKGROUND','PROCESS_DOCUMENT','OTHER'),($2,1,'DETERMINISTIC','GENERATE_CYCLE_RESIDUAL_REVIEW','OTHER')`, terminalJob, residualJob); err != nil {
		t.Fatal(err)
	}
	var oldPhase, freshPhase string
	if err := pool.QueryRow(ctx, `INSERT INTO intelligence_phase_telemetry(capability,purpose,latency_ms,outcome,created_at) VALUES('GENERATIVE','OTHER_BOUNDED',1,'SUCCEEDED',now()-interval '40 days') RETURNING id`).Scan(&oldPhase); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO intelligence_phase_telemetry(capability,purpose,latency_ms,outcome,created_at) VALUES('GENERATIVE','OTHER_BOUNDED',1,'SUCCEEDED',now()) RETURNING id`).Scan(&freshPhase); err != nil {
		t.Fatal(err)
	}
	deleted, err := pruneTerminalJobs(ctx, pool, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if deleted < 1 {
		t.Fatalf("pruned %d jobs, want the terminal job", deleted)
	}
	var terminalJobs, residualJobs, terminalLogs, residualLogs, oldPhases, freshPhases int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM job WHERE id=$1),
		(SELECT count(*) FROM job WHERE id=$2),
		(SELECT count(*) FROM job_retry_log WHERE job_id=$1),
		(SELECT count(*) FROM job_retry_log WHERE job_id=$2),
		(SELECT count(*) FROM intelligence_phase_telemetry WHERE id=$3),
		(SELECT count(*) FROM intelligence_phase_telemetry WHERE id=$4)`, terminalJob, residualJob, oldPhase, freshPhase).Scan(&terminalJobs, &residualJobs, &terminalLogs, &residualLogs, &oldPhases, &freshPhases); err != nil {
		t.Fatal(err)
	}
	if terminalJobs != 0 || terminalLogs != 0 {
		t.Fatalf("terminal job=%d retry logs=%d, want both pruned", terminalJobs, terminalLogs)
	}
	if residualJobs != 1 || residualLogs != 1 {
		t.Fatalf("residual job=%d retry logs=%d, want both retained", residualJobs, residualLogs)
	}
	if oldPhases != 0 || freshPhases != 1 {
		t.Fatalf("telemetry old=%d fresh=%d, want old pruned and fresh retained", oldPhases, freshPhases)
	}
}
