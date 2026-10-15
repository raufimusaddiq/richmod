package document

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func seedScreenshotIdentityCandidate(t *testing.T, f screenshotFixture, metadata string, at time.Time) string {
	t.Helper()
	ctx := context.Background()
	var transactionID, proposalID string
	if err := f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,counterparty_name,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',7500,'IDR',$2,'Synthetic Store',now()) RETURNING id`, f.householdID, at).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,confidence,proposal_status,metadata_json) VALUES($1,$2,$3,'EXPENSE',7500,'IDR',$4,'Synthetic Store',.95,'ACCEPTED',$5::jsonb) RETURNING id`, f.householdID, f.sourceID, transactionID, at, metadata).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TRANSACTION_SCREENSHOT',.95,jsonb_build_object('proposal_id',$3::uuid))`, transactionID, f.sourceID, proposalID); err != nil {
		t.Fatal(err)
	}
	return transactionID
}

// Baseline regression: only the matcher call requires the old argument shape.
func TestScreenshotIdentityLegacyTimeConflict(t *testing.T) {
	f := seedScreenshotFixture(t, "Synthetic legacy screenshot identity")
	at := screenshotRowTime()
	id := seedScreenshotIdentityCandidate(t, f, `{"date_known":true}`, at.Add(-10*time.Minute))
	row := validatedScreenshotRow{Value: screenshotRow{Amount: ptr("7500"), Merchant: "Synthetic Store"}, Type: "EXPENSE", TransactionAt: at, DateKnown: true}
	matches, err := (&Processor{pool: f.pool}).findScreenshotMatches(context.Background(), f.householdID, row)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].ID != id || matches[0].Score > .80 {
		t.Fatalf("different printed times must retain duplicate review, not merge: %v", matches)
	}
}

func TestScreenshotF02IdentitylessPaymentsStayDistinct(t *testing.T) {
	for _, precision := range []string{"DATE", "TIME", ""} {
		t.Run("precision_"+precision, func(t *testing.T) {
			f := seedScreenshotFixture(t, "Synthetic identityless payments")
			ctx := context.Background()
			at := screenshotRowTime()
			metadata, err := json.Marshal(map[string]any{"date_known": true, "transaction_time_precision": precision})
			if err != nil {
				t.Fatal(err)
			}
			first := seedScreenshotIdentityCandidate(t, f, string(metadata), at)
			// A separate upload is a separate source, never a replay of the first.
			if err := f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',now(),$2,'PROCESSING') RETURNING id`, f.householdID, []byte("synthetic second payment")).Scan(&f.sourceID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(ctx, `UPDATE document SET source_event_id=$2 WHERE id=$1`, f.documentID, f.sourceID); err != nil {
				t.Fatal(err)
			}
			row := screenshotDataRow("EXPENSE", "7500", "Synthetic Store")
			row.Value.Direction, row.Value.TransactionTimePrecision = "OUT", precision
			row.CategoryID, row.CategoryDecided = &f.categoryID, true
			row.Candidates, err = (&Processor{pool: f.pool}).findScreenshotMatches(ctx, f.householdID, row)
			if err != nil {
				t.Fatal(err)
			}
			if len(row.Candidates) != 1 || row.Candidates[0].ID != first || row.Candidates[0].Score >= .90 {
				t.Fatalf("identityless event can auto-link: %v", row.Candidates)
			}
			f.persist(t, rowChoiceProvenance{}, []validatedScreenshotRow{row})
			var confirmed, pending, merged, reviews int
			if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'),(SELECT count(*) FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW'),(SELECT count(*) FROM transaction_proposal WHERE source_event_id=$2 AND proposal_status='MERGED'),(SELECT count(*) FROM review_item WHERE household_id=$1 AND review_type='POSSIBLE_DUPLICATE' AND status IN ('OPEN','PENDING_SEND'))`, f.householdID, f.sourceID).Scan(&confirmed, &pending, &merged, &reviews); err != nil {
				t.Fatal(err)
			}
			if confirmed != 1 || pending != 1 || merged != 0 || reviews != 1 {
				t.Fatalf("confirmed=%d pending=%d merged=%d reviews=%d", confirmed, pending, merged, reviews)
			}
		})
	}
}

func TestScreenshotIdentityDifferentCloseEvents(t *testing.T) {
	f := seedScreenshotFixture(t, "Synthetic distinct screenshot identities")
	at := screenshotRowTime()
	seedScreenshotIdentityCandidate(t, f, `{"transaction_identifiers":[{"kind":"TRANSACTION_ID","value":"SYNTHETIC-A"}],"date_known":true,"transaction_time_precision":"TIME"}`, at.Add(-10*time.Minute))
	row := screenshotDataRow("EXPENSE", "7500", "Synthetic Store")
	row.Value.Direction, row.Value.TransactionIdentifiers = "OUT", []screenshotIdentifier{{"TRANSACTION_ID", "SYNTHETIC-B"}}
	row.Value.TransactionTimePrecision, row.Value.TransactionAt = "TIME", ptr(at.Format(time.RFC3339))
	row.CategoryID, row.CategoryDecided = &f.categoryID, true
	var err error
	row.Candidates, err = (&Processor{pool: f.pool}).findScreenshotMatches(context.Background(), f.householdID, row)
	if err != nil {
		t.Fatal(err)
	}
	if len(row.Candidates) != 0 {
		t.Fatalf("conflicting identifiers must not be duplicate candidates: %v", row.Candidates)
	}
	f.persist(t, rowChoiceProvenance{}, []validatedScreenshotRow{row})
	var count int
	var ids []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM transaction WHERE household_id=$1 AND status='CONFIRMED'`, f.householdID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("distinct events must remain separate: %d", count)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT metadata_json->'transaction_identifiers' FROM transaction_proposal WHERE source_event_id=$1 AND proposal_key='row-001'`, f.sourceID).Scan(&ids); err != nil {
		t.Fatal(err)
	}
	var observed []screenshotIdentifier
	if err := json.Unmarshal(ids, &observed); err != nil || len(observed) != 1 || observed[0].Value != "SYNTHETIC-B" {
		t.Fatalf("identity was not persisted on accepted proposal: %s %v", ids, err)
	}
}

func TestScreenshotIdentityRepeatAndCanonicalBoundaries(t *testing.T) {
	f := seedScreenshotFixture(t, "Synthetic repeat screenshot identity")
	other := seedScreenshotFixture(t, "Synthetic other household identity")
	at := screenshotRowTime()
	metadata := `{"transaction_identifiers":[{"kind":"TRANSACTION_ID","value":"SYNTHETIC-REPEAT"}],"date_known":true,"transaction_time_precision":"TIME"}`
	id := seedScreenshotIdentityCandidate(t, f, metadata, at)
	seedScreenshotIdentityCandidate(t, other, metadata, at)
	row := screenshotDataRow("EXPENSE", "7500", "Synthetic Store")
	row.Value.TransactionIdentifiers = []screenshotIdentifier{{"TRANSACTION_ID", "SYNTHETIC-REPEAT"}}
	row.Value.TransactionTimePrecision = "TIME"
	p := &Processor{pool: f.pool}
	for _, test := range []struct {
		name      string
		amount    string
		kind      string
		dateKnown bool
		at        time.Time
		want      int
	}{
		{"exact repeat", "7500", "EXPENSE", true, at, 1},
		{"identity outside fuzzy window", "7500", "EXPENSE", true, at.Add(96 * time.Hour), 1},
		{"no printed time", "7500", "EXPENSE", false, at.Add(96 * time.Hour), 1},
		{"different amount", "7501", "EXPENSE", true, at, 0},
		{"different type", "7500", "INCOME", true, at, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := row
			current.Value.Amount, current.Type, current.DateKnown, current.TransactionAt = &test.amount, test.kind, test.dateKnown, test.at
			matches, err := p.findScreenshotMatches(context.Background(), f.householdID, current)
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != test.want || (test.want == 1 && (matches[0].ID != id || matches[0].Score != 1)) {
				t.Fatalf("canonical boundaries changed: %v", matches)
			}
		})
	}
	matches, err := p.findScreenshotMatches(context.Background(), f.householdID, row)
	if err != nil {
		t.Fatal(err)
	}
	row.Matched = &matches[0]
	f.persist(t, rowChoiceProvenance{}, []validatedScreenshotRow{row})
	var transactions, linked int
	if err := f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM transaction WHERE household_id=$1),(SELECT count(*) FROM transaction_proposal WHERE household_id=$1 AND proposal_status='MERGED' AND metadata_json->'transaction_identifiers' IS NOT NULL)`, f.householdID).Scan(&transactions, &linked); err != nil {
		t.Fatal(err)
	}
	if transactions != 1 || linked != 1 {
		t.Fatalf("repeat must link without new transaction: transactions=%d linked=%d", transactions, linked)
	}
}

func TestScreenshotIdentityEvidenceBinding(t *testing.T) {
	for _, mode := range []string{"superseded", "foreign proposal", "foreign source", "unbound proposal"} {
		t.Run(mode, func(t *testing.T) {
			f := seedScreenshotFixture(t, fmt.Sprintf("Synthetic %s identity", mode))
			other := seedScreenshotFixture(t, "Synthetic foreign identity facts")
			at := screenshotRowTime()
			metadata := `{"transaction_identifiers":[{"kind":"TRANSACTION_ID","value":"SYNTHETIC-BOUND"}],"date_known":true}`
			id := seedScreenshotIdentityCandidate(t, f, metadata, at)
			foreign := seedScreenshotIdentityCandidate(t, other, metadata, at)
			ctx := context.Background()
			var err error
			switch mode {
			case "superseded":
				_, err = f.pool.Exec(ctx, `UPDATE transaction_evidence SET metadata_json=metadata_json || '{"link_status":"SUPERSEDED"}'::jsonb WHERE transaction_id=$1`, id)
			case "foreign proposal":
				_, err = f.pool.Exec(ctx, `UPDATE transaction_evidence SET metadata_json=jsonb_build_object('proposal_id',(SELECT metadata_json->>'proposal_id' FROM transaction_evidence WHERE transaction_id=$2)) WHERE transaction_id=$1`, id, foreign)
			case "foreign source":
				_, err = f.pool.Exec(ctx, `UPDATE transaction_evidence SET source_event_id=$2 WHERE transaction_id=$1`, id, other.sourceID)
			case "unbound proposal":
				_, err = f.pool.Exec(ctx, `UPDATE transaction_evidence SET metadata_json='{}'::jsonb WHERE transaction_id=$1`, id)
			}
			if err != nil {
				t.Fatal(err)
			}
			row := screenshotDataRow("EXPENSE", "7500", "Synthetic Store")
			row.TransactionAt, row.DateKnown = at.Add(96*time.Hour), false
			row.Value.TransactionIdentifiers = []screenshotIdentifier{{"TRANSACTION_ID", "SYNTHETIC-BOUND"}}
			matches, err := (&Processor{pool: f.pool}).findScreenshotMatches(ctx, f.householdID, row)
			if err != nil || len(matches) != 0 {
				t.Fatalf("unbound identity must never authorize linking: %v %v", matches, err)
			}
		})
	}
}
