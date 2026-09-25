package operations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestJudgmentAggregateCountsLanesAndAvoidedCalls is the PRD §23 completion
// criterion 13: telemetry must report Jev-only vs mixed vs generative turns and
// the native tool calls the decision plane avoided. The aggregate is derived
// from the two tables the worker already writes, so a household's scoreboard is
// correct without any extra pipeline, and a second household never bleeds in.
func TestJudgmentAggregateCountsLanesAndAvoidedCalls(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()

	seedHousehold := func(label string) string {
		var householdID string
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("%s %d", label, stamp)).Scan(&householdID); err != nil {
			t.Fatal(err)
		}
		return householdID
	}
	householdID := seedHousehold("Judgment Aggregate")
	otherHouseholdID := seedHousehold("Other Household")

	for _, row := range []struct {
		householdID string
		lane        string
		avoided     int
	}{
		{householdID, "JEV_ONLY", 2},
		{householdID, "JEV_ONLY", 0},
		{householdID, "JEV_THEN_GENERATIVE", 1},
		{householdID, "GENERATIVE_ONLY", 0},
		{otherHouseholdID, "JEV_ONLY", 99}, // must not be counted for householdID
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO judgment_turn_telemetry(household_id,lane,decision_tasks,policy_version,model,native_tool_calls_avoided) VALUES($1,$2,'{}','test','jev-test',$3)`, row.householdID, row.lane, row.avoided); err != nil {
			t.Fatal(err)
		}
	}
	// Production-shaped rows: the bounded plane writes protocol 'systemone',
	// call_kind JUDGMENT/DECISION, and the gateway's SUCCEEDED/FAILED status
	// vocabulary. An earlier revision of this test used the bogus 'SUCCESS'/
	// 'ERROR' spellings the code used to emit, which is exactly why the counter
	// read zero against real data.
	for _, row := range []struct {
		status     string
		callKind   string
		durationMs int
	}{
		{"SUCCEEDED", "JUDGMENT", 120},
		{"FAILED", "JUDGMENT", 40},
		{"FAILED", "JUDGMENT", 30},
		{"SUCCEEDED", "DECISION", 0},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO llm_call(household_id,task,protocol,model,status,error_class,duration_ms,call_kind) VALUES($1,'TRANSACTION_SEMANTICS','systemone','jev-test',$2,NULLIF($2,'SUCCEEDED')::text,$3,$4)`, householdID, row.status, row.durationMs, row.callKind); err != nil {
			t.Fatal(err)
		}
	}
	// A generative call in the same window must not be mistaken for a bounded
	// call: the aggregate filters on protocol.
	if _, err := pool.Exec(ctx, `INSERT INTO llm_call(household_id,task,protocol,model,status,duration_ms,call_kind) VALUES($1,'TELEGRAM_NATIVE','responses','gpt-test','FAILED',9000,'NATIVE_TOOL')`, householdID); err != nil {
		t.Fatal(err)
	}

	aggregate, err := NewHandler(pool).loadJudgmentAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if got := aggregate.Turns["JEV_ONLY"]; got != 2 {
		t.Fatalf("JEV_ONLY turns = %d, want 2", got)
	}
	if got := aggregate.Turns["JEV_THEN_GENERATIVE"]; got != 1 {
		t.Fatalf("JEV_THEN_GENERATIVE turns = %d, want 1", got)
	}
	if got := aggregate.Turns["GENERATIVE_ONLY"]; got != 1 {
		t.Fatalf("GENERATIVE_ONLY turns = %d, want 1", got)
	}
	if aggregate.Avoided != 3 {
		t.Fatalf("native tool calls avoided = %d, want 3", aggregate.Avoided)
	}
	if got, _ := aggregate.Decisions["judgmentCalls"].(int); got != 3 {
		t.Fatalf("judgment calls = %v, want 3", aggregate.Decisions["judgmentCalls"])
	}
	if got, _ := aggregate.Decisions["judgmentFailures"].(int); got != 2 {
		t.Fatalf("judgment failures = %v, want 2", aggregate.Decisions["judgmentFailures"])
	}
}

// TestJudgmentAggregateMeasuresPhaseOrderWithoutConfusingTransportForPolicy is
// the IR-09 exit gate: model-order counts, latency, double-pass shape, and
// rescue outcome must come from the rows that actually prove them. Transport
// success is never reported as policy acceptance.
func TestJudgmentAggregateMeasuresPhaseOrder(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Phase Telemetry %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),decode('00','hex'),'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("phase-%d", stamp)).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	phase := func(capability, purpose string, dimensions []string, outcome string) {
		if _, err := pool.Exec(ctx, `INSERT INTO intelligence_phase_telemetry(household_id,source_event_id,capability,purpose,semantic_dimensions,answered_dimensions,policy_version,model,latency_ms,outcome) VALUES($1,$2,$3,$4,$5,$5,'test-policy','test-model',10,$6)`, householdID, eventID, capability, purpose, dimensions, outcome); err != nil {
			t.Fatal(err)
		}
	}
	// Valid order: generative extraction then a Jev residual category ruling.
	phase("GENERATIVE", "EXTRACTION", []string{"amount", "merchant"}, "SUCCEEDED")
	phase("JEV", "RESIDUAL_CATEGORY", []string{"category"}, "SUCCEEDED")
	// Go accepted that rescue: the decision row is what makes it a success.
	if _, err := pool.Exec(ctx, `INSERT INTO judgment_decision(household_id,source_event_id,task,model,policy_version,question_keys,outcome) VALUES($1,$2,'RECEIPT_CATEGORY','test-model','test-policy',ARRAY['category'],'AUTO_CONFIRM')`, householdID, eventID); err != nil {
		t.Fatal(err)
	}
	// Redundant order on a second event: generative accepted a category and a
	// later non-residual Jev pass repeated the same semantic dimension.
	var redundantEvent string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),decode('01','hex'),'RECEIVED') RETURNING id`, householdID, fmt.Sprintf("phase-redundant-%d", stamp)).Scan(&redundantEvent); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct {
		capability string
		purpose    string
	}{
		{"GENERATIVE", "EXTRACTION"},
		{"JEV", "TRANSACTION_BOUNDED"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO intelligence_phase_telemetry(household_id,source_event_id,capability,purpose,semantic_dimensions,answered_dimensions,policy_version,latency_ms,outcome) VALUES($1,$2,$3,$4,ARRAY['category'],ARRAY['category'],'test-policy',10,'SUCCEEDED')`, householdID, redundantEvent, spec.capability, spec.purpose); err != nil {
			t.Fatal(err)
		}
	}

	aggregate, err := NewHandler(pool).loadJudgmentAggregate(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := aggregate.Phases["jevCalls"].(int); got != 2 {
		t.Fatalf("jevCalls = %v, want 2", aggregate.Phases["jevCalls"])
	}
	if got, _ := aggregate.Phases["generativeCalls"].(int); got != 2 {
		t.Fatalf("generativeCalls = %v, want 2", aggregate.Phases["generativeCalls"])
	}
	if got, _ := aggregate.Phases["events"].(int); got != 2 {
		t.Fatalf("events = %v, want 2", aggregate.Phases["events"])
	}
	if got, _ := aggregate.Phases["residualCategoryPasses"].(int); got != 1 {
		t.Fatalf("residualCategoryPasses = %v, want 1", aggregate.Phases["residualCategoryPasses"])
	}
	if got, _ := aggregate.Phases["residualRescueSuccesses"].(int); got != 1 {
		t.Fatalf("residualRescueSuccesses = %v, want 1", aggregate.Phases["residualRescueSuccesses"])
	}
	if got, _ := aggregate.Phases["categoryDoublePassCandidates"].(int); got != 1 {
		t.Fatalf("categoryDoublePassCandidates = %v, want 1", aggregate.Phases["categoryDoublePassCandidates"])
	}
	if got, _ := aggregate.Phases["latencyP50Ms"].(float64); got != 10 {
		t.Fatalf("latencyP50Ms = %v, want 10", got)
	}
}
