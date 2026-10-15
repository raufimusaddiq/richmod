package document

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

const screenshotIdentifierPattern = `^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`

var screenshotIdentifierSyntax = regexp.MustCompile(screenshotIdentifierPattern)

type screenshotIdentifier struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func validScreenshotIdentity(row screenshotRow) bool {
	if len(row.TransactionIdentifiers) > 3 || (row.TransactionTimePrecision != "" && row.TransactionTimePrecision != "DATE" && row.TransactionTimePrecision != "TIME") || (row.TransactionTimePrecision != "" && row.TransactionAt == nil) {
		return false
	}
	seen := make(map[string]bool, len(row.TransactionIdentifiers))
	for _, id := range row.TransactionIdentifiers {
		if (id.Kind != "TRANSACTION_ID" && id.Kind != "REFERENCE_NUMBER" && id.Kind != "ORDER_ID") || !screenshotIdentifierSyntax.MatchString(id.Value) || seen[id.Kind] {
			return false
		}
		seen[id.Kind] = true
	}
	return true
}

// Identity remains observed evidence, not authority to change amount, type or tenant.
func screenshotIdentityRelation(left, right []screenshotIdentifier) (equal, conflict bool) {
	for _, a := range left {
		for _, b := range right {
			if a.Kind == b.Kind {
				if a.Value != b.Value {
					conflict = true
				} else {
					equal = true
				}
			}
		}
	}
	return equal, conflict
}

func screenshotPreciseTime(at time.Time, known bool, precision string) bool {
	return known && (precision == "TIME" || (precision == "" && (at.In(jakarta()).Hour() != 0 || at.In(jakarta()).Minute() != 0 || at.Second() != 0 || at.Nanosecond() != 0)))
}

func screenshotIdentityMetadata(row validatedScreenshotRow) string {
	metadata, _ := json.Marshal(map[string]any{
		"transaction_identifiers":    row.Value.TransactionIdentifiers,
		"transaction_time_precision": row.Value.TransactionTimePrecision,
		"date_known":                 row.DateKnown,
	})
	return string(metadata)
}

type screenshotCandidateEvidence struct {
	TransactionAt time.Time       `json:"transaction_at"`
	Metadata      json.RawMessage `json:"metadata"`
}

func reconcileScreenshotCandidate(candidate matchCandidate, row validatedScreenshotRow, evidence []screenshotCandidateEvidence) (matchCandidate, bool) {
	equal, timeConflict, invalid := false, false, false
	for _, prior := range evidence {
		var facts struct {
			Identifiers []screenshotIdentifier `json:"transaction_identifiers"`
			Precision   string                 `json:"transaction_time_precision"`
			DateKnown   *bool                  `json:"date_known"`
		}
		at := prior.TransactionAt.Format(time.RFC3339)
		if err := json.Unmarshal(prior.Metadata, &facts); err != nil || !validScreenshotIdentity(screenshotRow{TransactionIdentifiers: facts.Identifiers, TransactionTimePrecision: facts.Precision, TransactionAt: &at}) {
			invalid = true
			continue
		}
		same, conflict := screenshotIdentityRelation(row.Value.TransactionIdentifiers, facts.Identifiers)
		if conflict {
			return candidate, false
		}
		equal = equal || same
		known := facts.DateKnown == nil || *facts.DateKnown
		if screenshotPreciseTime(row.TransactionAt, row.DateKnown, row.Value.TransactionTimePrecision) && screenshotPreciseTime(prior.TransactionAt, known, facts.Precision) && !row.TransactionAt.Equal(prior.TransactionAt) {
			timeConflict = true
		}
	}
	if equal && !invalid {
		candidate.Score = 1
		return candidate, true
	}
	if timeConflict || invalid {
		candidate.Score = min(candidate.Score, .80)
		return candidate, true // Keep plausible duplicates visible; never auto-link uncertain identity.
	}
	// Time/amount/merchant cannot prove event identity, even at an identical timestamp.
	candidate.Score = min(candidate.Score, .80)
	// Preserve the prior guard for receipts/bank candidates without screenshot facts.
	if sameMerchant(candidate.Merchant, row.Value.Merchant) && candidate.Hours >= 1 {
		return candidate, false
	}
	return candidate, true
}

func (p *Processor) findScreenshotMatches(ctx context.Context, householdID string, row validatedScreenshotRow) ([]matchCandidate, error) {
	if row.Value.Amount == nil || (!row.DateKnown && len(row.Value.TransactionIdentifiers) == 0) {
		return nil, nil
	}
	ids, err := json.Marshal(row.Value.TransactionIdentifiers)
	if err != nil {
		return nil, err
	}
	// Both fuzzy and identity candidates retain the canonical household/type/amount
	// boundary. Only proposals actually bound to this transaction's screenshot
	// evidence may contribute identity; stray proposals cannot authorise a link.
	rows, err := p.pool.Query(ctx, `SELECT t.id,COALESCE(m.normalized_name,t.counterparty_name,''),abs(extract(epoch FROM (t.transaction_at-$4::timestamptz)))/3600,COALESCE(e.facts,'[]'::jsonb)
FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id
LEFT JOIN LATERAL (
 SELECT jsonb_agg(jsonb_build_object('transaction_at',p.transaction_at,'metadata',p.metadata_json)) AS facts,
 bool_or(EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(NULLIF($5::jsonb,'null'::jsonb),'[]'::jsonb)) incoming WHERE p.metadata_json->'transaction_identifiers' @> jsonb_build_array(incoming))) AS identity_hit
 FROM transaction_evidence te
 JOIN transaction_proposal p ON p.id::text=te.metadata_json->>'proposal_id' AND p.household_id=t.household_id AND p.source_event_id=te.source_event_id
 JOIN source_event s ON s.id=te.source_event_id AND s.household_id=t.household_id
 WHERE te.transaction_id=t.id AND te.evidence_type='TRANSACTION_SCREENSHOT' AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE'
) e ON true
WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type=$2 AND t.currency='IDR' AND t.amount=$3::numeric
AND (($6::boolean AND t.transaction_at BETWEEN $4::timestamptz-interval '72 hours' AND $4::timestamptz+interval '72 hours') OR e.identity_hit)
ORDER BY e.identity_hit DESC NULLS LAST,abs(extract(epoch FROM (t.transaction_at-$4::timestamptz))),t.id`, householdID, row.Type, *row.Value.Amount, row.TransactionAt, string(ids), row.DateKnown)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []matchCandidate
	for rows.Next() {
		var candidate matchCandidate
		var raw []byte
		if err := rows.Scan(&candidate.ID, &candidate.Merchant, &candidate.Hours, &raw); err != nil {
			return nil, err
		}
		var evidence []screenshotCandidateEvidence
		if err := json.Unmarshal(raw, &evidence); err != nil {
			return nil, fmt.Errorf("decode screenshot candidate evidence: %w", err)
		}
		merchantMatch := sameMerchant(candidate.Merchant, row.Value.Merchant)
		candidate.Score = documentMatchScore(candidate.Hours, merchantMatch)
		candidate, keep := reconcileScreenshotCandidate(candidate, row, evidence)
		if keep && (candidate.Score == 1 || (row.DateKnown && (merchantMatch || candidate.Hours <= 1))) {
			result = append(result, candidate)
		}
	}
	return result, rows.Err()
}
