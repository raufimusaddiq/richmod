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

type LedgerMatchQuery struct {
	HouseholdID         string
	Type                string
	AmountIDR           string
	At                  time.Time
	Merchant            string
	ExcludeSourceID     string
	ExcludeEvidenceType string
}

type LedgerMatch struct {
	ID            string
	Status        string
	Merchant      string
	Hours         float64
	MerchantMatch bool
	Score         float64
}

const (
	ledgerMatchWindowHours = 72
	strongMatchScore       = 0.90
	runnerUpCeiling        = 0.80
	ledgerMatchLimit       = 10
	ledgerMatchScanLimit   = 200
)

func FindLedgerMatches(ctx context.Context, q rowQueryer, query LedgerMatchQuery) ([]LedgerMatch, error) {
	rows, err := q.Query(ctx, `SELECT t.id::text,t.status,COALESCE(m.normalized_name,t.counterparty_name,''),abs(extract(epoch FROM (t.transaction_at-$4::timestamptz)))/3600
		FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id
		WHERE t.household_id=$1 AND t.status IN ('CONFIRMED','NEEDS_REVIEW') AND t.type=$2 AND t.currency='IDR' AND t.amount=$3::numeric
		  AND t.transaction_at BETWEEN $4::timestamptz-make_interval(hours=>$7) AND $4::timestamptz+make_interval(hours=>$7)
		  AND ($5='' OR NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.transaction_id=t.id AND te.source_event_id::text=$5))
		  AND ($6='' OR NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.transaction_id=t.id AND te.evidence_type=$6 AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE'))
		ORDER BY abs(extract(epoch FROM (t.transaction_at-$4::timestamptz))),t.id LIMIT $8`, query.HouseholdID, query.Type, query.AmountIDR, query.At, query.ExcludeSourceID, query.ExcludeEvidenceType, ledgerMatchWindowHours, ledgerMatchScanLimit)
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
		out = append(out, match)
	}
	return out, rows.Err()
}

func LockLedgerEvent(ctx context.Context, tx pgx.Tx, householdID, amountIDR string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('ledger-event:'||$1::text||':'||$2::numeric::text,0))`, householdID, amountIDR)
	return err
}

func SameLedgerCandidates(decided map[string]float64, now []LedgerMatch) bool {
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

var ErrLedgerRaced = errors.New("reviewdomain: ledger candidates changed concurrently; retry")

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

func StrongLedgerMatch(matches []LedgerMatch) *LedgerMatch {
	if len(matches) > ledgerMatchLimit {
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

func NormalizeMerchant(value string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)), " ")
}

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

type EvidenceEnrichment struct {
	MerchantName string
	AccountID    string
	Reference    string
	Counterparty string
}

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
