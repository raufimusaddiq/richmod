package document

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const screenshotIdentifierPattern = `^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`

var screenshotIdentifierSyntax = regexp.MustCompile(screenshotIdentifierPattern)

type screenshotIdentifier struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

const screenshotIdentifiersDescription = "Printed transaction identifiers. Each kind (TRANSACTION_ID, REFERENCE_NUMBER, ORDER_ID) appears at most once per row and a row has at most 3 identifiers. If several numbers of one kind are printed, keep only the most specific one (for REFERENCE_NUMBER, the number explicitly labelled reference or RRN). Use null when none is printed."

const maxScreenshotIdentifiers = 3

func knownScreenshotIdentifierKind(kind string) bool {
	return kind == "TRANSACTION_ID" || kind == "REFERENCE_NUMBER" || kind == "ORDER_ID"
}

type screenshotIdentityNormalization struct {
	Row                   int      `json:"row"`
	DuplicateKindsDropped []string `json:"duplicate_kinds_dropped,omitempty"`
	OverCapDropped        int      `json:"over_cap_dropped,omitempty"`
}

func normalizeScreenshotIdentifiers(value *screenshotExtraction) []screenshotIdentityNormalization {
	var changes []screenshotIdentityNormalization
	for index := range value.Transactions {
		row := &value.Transactions[index]
		ids := row.TransactionIdentifiers
		valid := true
		for _, id := range ids {
			if !knownScreenshotIdentifierKind(id.Kind) || !screenshotIdentifierSyntax.MatchString(id.Value) {
				valid = false
				break
			}
		}
		if !valid || len(ids) == 0 {
			continue
		}
		kept := make([]screenshotIdentifier, 0, len(ids))
		seen := make(map[string]bool, len(ids))
		change := screenshotIdentityNormalization{Row: index}
		for _, id := range ids {
			if seen[id.Kind] {
				if !slices.Contains(change.DuplicateKindsDropped, id.Kind) {
					change.DuplicateKindsDropped = append(change.DuplicateKindsDropped, id.Kind)
				}
				continue
			}
			seen[id.Kind] = true
			kept = append(kept, id)
		}
		if len(kept) > maxScreenshotIdentifiers {
			change.OverCapDropped = len(kept) - maxScreenshotIdentifiers
			kept = kept[:maxScreenshotIdentifiers]
		}
		if len(change.DuplicateKindsDropped) == 0 && change.OverCapDropped == 0 {
			continue
		}
		row.TransactionIdentifiers = kept
		changes = append(changes, change)
	}
	return changes
}

func screenshotIdentityIssue(row screenshotRow) string {
	var rules []string
	add := func(rule string) {
		if !slices.Contains(rules, rule) {
			rules = append(rules, rule)
		}
	}
	if row.TransactionTimePrecision != "" && row.TransactionTimePrecision != "DATE" && row.TransactionTimePrecision != "TIME" {
		add("transaction_time_precision must be DATE, TIME or null")
	}
	if row.TransactionTimePrecision != "" && row.TransactionAt == nil {
		add("transaction_time_precision requires transaction_at")
	}
	if len(row.TransactionIdentifiers) > maxScreenshotIdentifiers {
		add(fmt.Sprintf("more than %d transaction identifiers", maxScreenshotIdentifiers))
	}
	seen := make(map[string]bool, len(row.TransactionIdentifiers))
	for _, id := range row.TransactionIdentifiers {
		switch {
		case !knownScreenshotIdentifierKind(id.Kind):
			add("identifier kind must be TRANSACTION_ID, REFERENCE_NUMBER or ORDER_ID")
		case seen[id.Kind]:
			add("duplicate identifier kind " + id.Kind + ": keep one per kind")
		}
		if !screenshotIdentifierSyntax.MatchString(id.Value) {
			add("identifier value must be 1-128 characters of letters, digits and . _ : / -")
		}
		seen[id.Kind] = true
	}
	return strings.Join(rules, "; ")
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
	if len(evidence) == 0 {
		candidate.Score = min(candidate.Score, .80)
		return candidate, true
	}
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
		return candidate, true
	}
	candidate.Score = min(candidate.Score, .80)
	if sameMerchant(candidate.Merchant, row.Value.Merchant) && candidate.Hours >= 1 {
		return candidate, false
	}
	return candidate, true
}

func (p *Processor) findScreenshotMatches(ctx context.Context, householdID string, row validatedScreenshotRow) ([]matchCandidate, error) {
	return findScreenshotMatchesWith(ctx, p.pool, householdID, row)
}

type screenshotQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func findScreenshotMatchesWith(ctx context.Context, q screenshotQueryer, householdID string, row validatedScreenshotRow) ([]matchCandidate, error) {
	if row.Value.Amount == nil || (!row.DateKnown && len(row.Value.TransactionIdentifiers) == 0) {
		return nil, nil
	}
	ids, err := json.Marshal(row.Value.TransactionIdentifiers)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT t.id,COALESCE(m.normalized_name,t.counterparty_name,''),abs(extract(epoch FROM (t.transaction_at-$4::timestamptz)))/3600,COALESCE(e.facts,'[]'::jsonb),t.status
FROM transaction t LEFT JOIN merchant m ON m.id=t.merchant_id
LEFT JOIN LATERAL (
 SELECT jsonb_agg(jsonb_build_object('transaction_at',p.transaction_at,'metadata',p.metadata_json)) AS facts,
 bool_or(EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(NULLIF($5::jsonb,'null'::jsonb),'[]'::jsonb)) incoming WHERE p.metadata_json->'transaction_identifiers' @> jsonb_build_array(incoming))) AS identity_hit
 FROM transaction_evidence te
 JOIN transaction_proposal p ON p.id::text=te.metadata_json->>'proposal_id' AND p.household_id=t.household_id AND p.source_event_id=te.source_event_id
 JOIN source_event s ON s.id=te.source_event_id AND s.household_id=t.household_id
 WHERE te.transaction_id=t.id AND te.evidence_type='TRANSACTION_SCREENSHOT' AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE'
) e ON true
WHERE t.household_id=$1 AND t.status IN ('CONFIRMED','NEEDS_REVIEW') AND t.type=$2 AND t.currency='IDR' AND t.amount=$3::numeric
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
		if err := rows.Scan(&candidate.ID, &candidate.Merchant, &candidate.Hours, &raw, &candidate.Status); err != nil {
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

func (p *Processor) recordIdentifierNormalization(ctx context.Context, householdID, sourceID, documentID, stage string, changes []screenshotIdentityNormalization) error {
	encoded, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json)
SELECT $1::uuid,'WORKER','NORMALIZE_SCREENSHOT_IDENTIFIERS','source_event',$2::uuid,jsonb_build_object('document_id',$3::uuid,'stage',$4::text,'rows',$5::jsonb)
WHERE NOT EXISTS (SELECT 1 FROM audit_log WHERE household_id=$1::uuid AND action='NORMALIZE_SCREENSHOT_IDENTIFIERS' AND entity_id=$2::uuid AND after_json->>'document_id'=$3::text AND after_json->>'stage'=$4::text)`, householdID, sourceID, documentID, stage, string(encoded))
	return err
}
