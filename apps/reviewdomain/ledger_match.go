// Cross-lane ledger matching. One real purchase can arrive as a bank email, a
// receipt photo and a transaction screenshot; it must stay one canonical
// transaction with many evidence rows (AGENTS.md, ADR-004). Every lane asks the
// same question before it writes a new transaction: does a household
// transaction of the same type and IDR amount already exist close in time with
// a compatible merchant? The answer is deterministic Go; a unique strong match
// is linked as evidence, anything weaker that is still plausible becomes a
// POSSIBLE_DUPLICATE review, and nothing is ever deleted.
package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// LedgerMatchQuery names the canonical event a new source describes.
type LedgerMatchQuery struct {
	HouseholdID string
	Type        string
	AmountIDR   string
	At          time.Time
	Merchant    string
	// ExcludeSourceID never matches transactions this same source already
	// evidences (a reprocess is not a duplicate of itself).
	ExcludeSourceID string
	// ExcludeEvidenceType skips transactions that already carry evidence of this
	// type: two bank emails are two bank events, never one.
	ExcludeEvidenceType string
}

// LedgerMatch is one candidate canonical transaction.
type LedgerMatch struct {
	ID            string
	Status        string
	Merchant      string
	Hours         float64
	MerchantMatch bool
	Score         float64
}

// Match thresholds shared by every lane.
const (
	ledgerMatchWindowHours = 72
	strongMatchScore       = 0.90
	runnerUpCeiling        = 0.80
	ledgerMatchLimit       = 10
)

// FindLedgerMatches returns plausible existing transactions, CONFIRMED or still
// NEEDS_REVIEW, ordered by closeness in time. A candidate is plausible when its
// merchant is compatible or it is within an hour; a different merchant days
// apart is not ambiguity.
func FindLedgerMatches(ctx context.Context, q rowQueryer, query LedgerMatchQuery) ([]LedgerMatch, error) {
	rows, err := q.Query(ctx, `SELECT t.id::text,t.status,COALESCE(m.normalized_name,t.counterparty_name,''),abs(extract(epoch FROM (t.transaction_at-$4::timestamptz)))/3600
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id
		WHERE t.household_id=$1 AND t.status IN ('CONFIRMED','NEEDS_REVIEW') AND t.type=$2 AND t.currency='IDR' AND t.amount=$3::numeric
		  AND t.transaction_at BETWEEN $4::timestamptz-make_interval(hours=>$7) AND $4::timestamptz+make_interval(hours=>$7)
		  AND ($5='' OR NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.transaction_id=t.id AND te.source_event_id::text=$5))
		  AND ($6='' OR NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.transaction_id=t.id AND te.evidence_type=$6 AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE'))
		ORDER BY abs(extract(epoch FROM (t.transaction_at-$4::timestamptz))),t.id`, query.HouseholdID, query.Type, query.AmountIDR, query.At, query.ExcludeSourceID, query.ExcludeEvidenceType, ledgerMatchWindowHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]LedgerMatch, 0)
	for rows.Next() {
		var match LedgerMatch
		if err := rows.Scan(&match.ID, &match.Status, &match.Merchant, &match.Hours); err != nil {
			return nil, err
		}
		match.MerchantMatch = CompatibleMerchant(match.Merchant, query.Merchant)
		if !match.MerchantMatch && match.Hours > 1 {
			continue
		}
		match.Score = LedgerMatchScore(match.Hours, match.MerchantMatch)
		// Every plausible candidate is returned: a strong match must be judged
		// against all of them, never against a time-ordered prefix.
		out = append(out, match)
	}
	return out, rows.Err()
}

// LockLedgerEvent serializes every lane that may write or link a transaction of
// this household and IDR amount, for the rest of the caller's transaction. A
// lane takes it before it matches and inserts, so two lanes ingesting the same
// purchase concurrently cannot both see "no match" and write two transactions.
func LockLedgerEvent(ctx context.Context, tx pgx.Tx, householdID, amountIDR string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('ledger-event:'||$1::text||':'||$2::numeric::text,0))`, householdID, amountIDR)
	return err
}

// SameLedgerCandidates reports whether a match result taken under the lock is
// the one the lane decided on: no new or vanished candidate and none whose score
// changed (another lane may have enriched its merchant meanwhile). Any change
// means the caller must re-run its decision.
func SameLedgerCandidates(decided map[string]float64, now []LedgerMatch) bool {
	// Both directions: a candidate that vanished (voided or merged by another
	// lane) is a change too, so a stale strong match is never linked.
	if len(decided) != len(now) {
		return false
	}
	for _, match := range now {
		score, ok := decided[match.ID]
		if !ok || score != match.Score {
			return false
		}
	}
	return true
}

// ErrLedgerRaced reports that another lane changed the candidate set between a
// lane's decision and its write; the job retries and decides again.
var ErrLedgerRaced = errors.New("reviewdomain: ledger candidates changed concurrently; retry")

// LedgerMatchScore is the shared deterministic score (amount and type already
// equal): closeness in time plus merchant compatibility.
func LedgerMatchScore(hours float64, merchantMatch bool) float64 {
	score := 0.45
	switch {
	case hours <= 1:
		score += 0.25
	case hours <= 24:
		score += 0.20
	default:
		score += 0.10
	}
	if merchantMatch {
		score += 0.25
	}
	return math.Round(score*100) / 100
}

// StrongLedgerMatch returns the single candidate that is safe to link without
// asking: a compatible merchant within a day, and no runner-up above 0.80.
// Otherwise it returns nil and the caller asks (POSSIBLE_DUPLICATE) when any
// candidate exists.
func StrongLedgerMatch(matches []LedgerMatch) *LedgerMatch {
	if len(matches) > ledgerMatchLimit {
		// Too many plausible events to call any one of them the same purchase.
		return nil
	}
	var strong *LedgerMatch
	for i := range matches {
		if matches[i].Score >= strongMatchScore {
			if strong != nil {
				return nil
			}
			strong = &matches[i]
		}
	}
	if strong == nil {
		return nil
	}
	for i := range matches {
		if matches[i].ID != strong.ID && matches[i].Score > runnerUpCeiling {
			return nil
		}
	}
	return strong
}

// NormalizeMerchant folds a merchant name to lower-case letter/digit tokens.
func NormalizeMerchant(value string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)), " ")
}

// CompatibleMerchant reports whether two observed merchant names describe the
// same merchant: equal after folding, or every token of the shorter name (at
// least five letters in total) appears in the longer one, as a bank descriptor
// such as "QRIS ALFAMART SUDIRMAN" does for a receipt's "Alfamart". An empty
// name is never compatible.
func CompatibleMerchant(left, right string) bool {
	a, b := NormalizeMerchant(left), NormalizeMerchant(right)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	short, long := strings.Fields(a), strings.Fields(b)
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(strings.Join(short, "")) < 5 {
		return false
	}
	present := map[string]bool{}
	for _, token := range long {
		present[token] = true
	}
	for _, token := range short {
		if !present[token] {
			return false
		}
	}
	return true
}

// EvidenceEnrichment carries the facts a newly linked source adds to an
// existing transaction. Only facts the transaction does not have yet are
// written; an existing fact is never overwritten by a later source.
type EvidenceEnrichment struct {
	MerchantName string
	AccountID    string
	Reference    string
	Counterparty string
}

// EnrichLinkedTransaction fills facts the existing transaction is missing from
// newly linked evidence, and keeps an open review's decision in step so it does
// not keep asking for a fact the evidence now supplies. It returns the fields it
// filled, for the caller's audit row.
func EnrichLinkedTransaction(ctx context.Context, tx pgx.Tx, householdID, transactionID string, facts EvidenceEnrichment) ([]string, error) {
	var filled []string
	if name := strings.TrimSpace(facts.MerchantName); name != "" {
		var missing bool
		if err := tx.QueryRow(ctx, `SELECT merchant_id IS NULL FROM transaction WHERE id=$1 AND household_id=$2 FOR UPDATE`, transactionID, householdID).Scan(&missing); err != nil {
			return nil, err
		}
		if missing {
			var merchantID string
			if err := tx.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,regexp_replace(trim($2), '[[:space:]]+', ' ', 'g')) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=now() RETURNING id::text`, householdID, name).Scan(&merchantID); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE transaction SET merchant_id=$3,updated_at=now() WHERE id=$1 AND household_id=$2 AND merchant_id IS NULL`, transactionID, householdID, merchantID); err != nil {
				return nil, err
			}
			filled = append(filled, "merchant")
			known, _ := json.Marshal(name)
			if _, err := tx.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(jsonb_set(decision,'{knownFacts,merchant}',$3::jsonb,true),'{missingFacts}',COALESCE((SELECT jsonb_agg(f) FROM jsonb_array_elements(decision->'missingFacts') f WHERE f<>'"merchant"'::jsonb),'[]'::jsonb)),updated_at=now()
				WHERE transaction_id=$1 AND household_id=$2 AND status IN ('OPEN','PENDING_SEND') AND jsonb_typeof(decision->'missingFacts')='array' AND jsonb_typeof(decision->'knownFacts')='object'`, transactionID, householdID, string(known)); err != nil {
				return nil, err
			}
		}
	}
	for _, column := range []struct{ name, value, field string }{{"account_id", facts.AccountID, "account"}, {"external_reference", facts.Reference, "reference"}, {"counterparty_name", facts.Counterparty, "counterparty"}} {
		value := strings.TrimSpace(column.value)
		if value == "" {
			continue
		}
		cast := ""
		if column.name == "account_id" {
			cast = "::uuid"
		}
		// column.name is one of the fixed names above, never input.
		result, err := tx.Exec(ctx, `UPDATE transaction SET `+column.name+`=$3`+cast+`,updated_at=now() WHERE id=$1 AND household_id=$2 AND `+column.name+` IS NULL`, transactionID, householdID, value)
		if err != nil {
			return nil, err
		}
		if result.RowsAffected() == 1 {
			filled = append(filled, column.field)
		}
	}
	return filled, nil
}
