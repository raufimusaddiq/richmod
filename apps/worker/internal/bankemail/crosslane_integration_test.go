package bankemail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Bank email checks transactions other lanes already wrote (receipt,
// screenshot, manual) before writing its own. The SPENDING_ONLY extraction is
// untouched; dedup only links evidence. Two bank emails stay two events.
func TestBankEmailAfterAnotherLaneLinksOrAsks(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	at := time.Date(2026, 10, 19, 6, 30, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, status, evidence, merchant string
		offset                           time.Duration
		want                             string // LINK, ASK, NEW
	}{
		{"receipt first, still in review", "NEEDS_REVIEW", "RECEIPT_IMAGE", "Alfamart", 15 * time.Minute, "LINK"},
		{"receipt first, confirmed", "CONFIRMED", "RECEIPT_IMAGE", "Alfamart", 2 * time.Hour, "LINK"},
		{"screenshot first, still in review", "NEEDS_REVIEW", "TRANSACTION_SCREENSHOT", "alfamart", 5 * time.Minute, "LINK"},
		{"unnamed nearby transaction asks", "CONFIRMED", "RECEIPT_IMAGE", "", 20 * time.Minute, "ASK"},
		{"another bank email is another event", "CONFIRMED", "BANK_EMAIL", "QRIS ALFAMART", 5 * time.Minute, "NEW"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			stamp := time.Now().UnixNano()
			var household, owner, account, category, source, otherSource, existing string
			must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Cross-lane bank %d", stamp)).Scan(&household))
			must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("crosslane-%d@test.invalid", stamp)).Scan(&owner))
			must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
			must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&category))
			listener := Listener{HouseholdID: household, AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
			must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listener.ID))
			must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&source))
			_, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, source, listener.ID)
			must(err)
			// The transaction the earlier lane wrote.
			var merchantID *string
			if tt.merchant != "" {
				var id string
				must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, household, tt.merchant).Scan(&id))
				merchantID = &id
			}
			must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSED') RETURNING id`, household, fmt.Sprint(stamp+1), []byte(fmt.Sprint(stamp+1))).Scan(&otherSource))
			must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,confirmed_at) VALUES($1,'EXPENSE',$2,54000,'IDR',$3,$4,$5,CASE WHEN $2='CONFIRMED' THEN now() END) RETURNING id`, household, tt.status, at.Add(-tt.offset), merchantID, category).Scan(&existing))
			_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,$3,.9)`, existing, otherSource, tt.evidence)
			must(err)

			amount, merchant, description := "54000", "QRIS ALFAMART SUDIRMAN", "Belanja"
			extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Merchant: &merchant, Description: &description, TransactionAt: &at, Confidence: .95}
			result := PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", AutoConfirm: true, CategoryID: category}
			must((&Processor{pool: pool}).persist(ctx, listener, source, extraction, result, ""))

			var transactions int
			var linked bool
			var sourceStatus string
			must(pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1),EXISTS(SELECT 1 FROM transaction_evidence WHERE transaction_id=$2 AND source_event_id=$3 AND evidence_type='BANK_EMAIL'),(SELECT processing_status FROM source_event WHERE id=$3)`, household, existing, source).Scan(&transactions, &linked, &sourceStatus))
			switch tt.want {
			case "LINK":
				var accountID *string
				var proposalStatus string
				must(pool.QueryRow(ctx, `SELECT t.account_id::text,(SELECT proposal_status FROM transaction_proposal WHERE source_event_id=$2) FROM transaction t WHERE t.id=$1`, existing, source).Scan(&accountID, &proposalStatus))
				if transactions != 1 || !linked || proposalStatus != "MERGED" {
					t.Fatalf("bank email must link to the existing event: transactions=%d linked=%v proposal=%s", transactions, linked, proposalStatus)
				}
				if accountID == nil || *accountID != account {
					t.Fatalf("linked bank evidence must enrich the missing account: %v", accountID)
				}
				want := "PROCESSED"
				if tt.status == "NEEDS_REVIEW" {
					want = "NEEDS_REVIEW"
				}
				if sourceStatus != want {
					t.Fatalf("bank source=%s want %s", sourceStatus, want)
				}
			case "ASK":
				var reviewType string
				var named bool
				must(pool.QueryRow(ctx, `SELECT ri.review_type,ri.decision->'knownFacts'->'prior_match_transaction_ids' ? $2 FROM review_item ri JOIN transaction t ON t.id=ri.transaction_id WHERE ri.household_id=$1 AND t.id<>$2::uuid AND ri.status IN ('OPEN','PENDING_SEND')`, household, existing).Scan(&reviewType, &named))
				if transactions != 2 || linked || reviewType != "POSSIBLE_DUPLICATE" || !named || sourceStatus != "NEEDS_REVIEW" {
					t.Fatalf("ambiguity must ask, never guess: transactions=%d linked=%v review=%s named=%v source=%s", transactions, linked, reviewType, named, sourceStatus)
				}
			case "NEW":
				if transactions != 2 || linked || sourceStatus != "PROCESSED" {
					t.Fatalf("a second bank email is a second event: transactions=%d linked=%v source=%s", transactions, linked, sourceStatus)
				}
			}
		})
	}
}

// A bank-fact completion whose persist (here: a cross-lane link) committed
// before the completion transaction failed is retried; the retry resolves the
// review and must also settle the source instead of leaving it NEEDS_REVIEW.
func TestCompleteRetryAfterCommittedLinkSettlesTheSource(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().UnixNano()
	var household, owner, account, listenerID, source, otherSource, existing, merchantID, item string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Complete retry %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("retry-%d@test.invalid", stamp)).Scan(&owner))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&source))
	_, err = pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{"kind":"TRANSACTION","amount_idr":"54000","transaction_at":"2026-10-19T06:30:00Z","merchant":"QRIS ALFAMART","confidence":0.9}','VALID')`, source, listenerID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN','{"version":1,"reasonCode":"UNKNOWN_BANK_TEMPLATE","allowedActions":["COMPLETE_BANK_FACTS","IGNORE"]}'::jsonb) RETURNING id`, household, source).Scan(&item))
	// The earlier attempt's persist committed: a link to the receipt transaction.
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Alfamart') RETURNING id`, household).Scan(&merchantID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'PROCESSED') RETURNING id`, household, fmt.Sprint(stamp+1), []byte(fmt.Sprint(stamp+1))).Scan(&otherSource))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',54000,'IDR','2026-10-19T06:20:00Z',$2,now()) RETURNING id`, household, merchantID).Scan(&existing))
	_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence) VALUES($1,$2,'RECEIPT_IMAGE',.9),($1,$3,'BANK_EMAIL',.9)`, existing, otherSource, source)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,confidence,proposal_status) VALUES($1,$2,'EXPENSE',54000,'IDR','2026-10-19T06:30:00Z',.9,'MERGED')`, household, source)
	must(err)
	amount, at := "54000", "2026-10-19T13:30:00+07:00"
	must((&Processor{pool: pool}).Complete(ctx, Payload{SourceEventID: source, ReviewID: item, AmountIDR: &amount, TransactionAt: &at, UserID: owner}))
	var itemStatus, sourceStatus string
	must(pool.QueryRow(ctx, `SELECT (SELECT status FROM review_item WHERE id=$1),(SELECT processing_status FROM source_event WHERE id=$2)`, item, source).Scan(&itemStatus, &sourceStatus))
	if itemStatus != "RESOLVED" || sourceStatus != "PROCESSED" {
		t.Fatalf("retry left review=%s source=%s", itemStatus, sourceStatus)
	}
}

// An auto-confirmed bank expense reopens a cycle waiting on a missing
// transaction, with the recomputed residual.
func TestAutoConfirmedBankEmailReopensAWaitingResidual(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().UnixNano()
	var household, owner, category, source, salarySource, salaryEventSource, start, end, caseID string
	listener := Listener{TrackingPolicy: "SPENDING_ONLY", Active: true}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank residual %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-residual-%d@test.invalid", stamp)).Scan(&owner))
	listener.HouseholdID = household
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id) VALUES($1,'Bank','bank@test.invalid',$2) RETURNING id`, household, owner).Scan(&listener.ID))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&category))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&source))
	_, err = pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, source, listener.ID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'PROCESSED') RETURNING id`, household, fmt.Sprint(stamp+1), []byte(fmt.Sprint(stamp+1))).Scan(&salaryEventSource))
	must(pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test','test',true) RETURNING id`, household, owner).Scan(&salarySource))
	for _, row := range []struct {
		date string
		into *string
	}{{"2026-10-01", &start}, {"2026-11-01", &end}} {
		var transaction string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,$2::date,'GENERAL',now()) RETURNING id`, household, row.date).Scan(&transaction))
		must(pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,1000000,$4,'CONFIRMED',$5) RETURNING id`, salarySource, household, row.date, transaction, salaryEventSource).Scan(row.into))
	}
	must(pool.QueryRow(ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-10-01','2026-11-01',1000000,0,0,1000000) RETURNING id`, household, start, end).Scan(&caseID))
	_, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status,decision,resolved_at,resolution_action) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','RESOLVED','{"version":1,"reasonCode":"CYCLE_RESIDUAL_ALLOCATION","allowedActions":["ALLOCATE_RETAINED_BALANCE","TRANSACTION_MISSING","LEAVE_UNALLOCATED"]}'::jsonb,now(),'TRANSACTION_MISSING')`, household, caseID)
	must(err)
	amount, merchant, description := "54000", "Warung", "Makan"
	at := time.Date(2026, 10, 19, 6, 30, 0, 0, time.UTC)
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Merchant: &merchant, Description: &description, TransactionAt: &at, Confidence: .95}
	must((&Processor{pool: pool}).persist(ctx, listener, source, extraction, PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", AutoConfirm: true, CategoryID: category}, ""))
	var residual string
	must(pool.QueryRow(ctx, `SELECT decision->'knownFacts'->>'residual_idr' FROM review_item WHERE cycle_residual_case_id=$1 AND status='OPEN'`, caseID).Scan(&residual))
	if residual != "946000" {
		t.Fatalf("residual=%s", residual)
	}
}
