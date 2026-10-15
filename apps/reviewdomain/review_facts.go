package reviewdomain

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

type ReviewFacts struct {
	HouseholdID, TransactionID, CategoryID, Relationship string
	TransactionAt                                        *time.Time
}

// ApplyTransactionReviewFacts persists accepted facts and the remaining contract
// together. A relationship answer never substitutes for a category or date.
func ApplyTransactionReviewFacts(ctx context.Context, tx pgx.Tx, facts ReviewFacts) ([]string, error) {
	var kind string
	var category *string
	var categoryValid, wealthValid bool
	if err := tx.QueryRow(ctx, `SELECT type,category_id,
		category_id IS NULL OR EXISTS(SELECT 1 FROM category c WHERE c.id=t.category_id AND c.household_id=t.household_id AND c.active),
		type<>'TRANSFER' OR transfer_wealth_compatible(purpose,related_wealth_account_id,household_id)
		FROM transaction t WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE`, facts.TransactionID, facts.HouseholdID).Scan(&kind, &category, &categoryValid, &wealthValid); err != nil {
		return nil, err
	}
	if slices.Contains([]string{"OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT", "INVESTMENT_ACCOUNT", "ASSET_PURCHASE"}, facts.Relationship) {
		kind = "TRANSFER"
		// The classifier already validated the replacement; it writes it after this call.
		categoryValid, wealthValid = true, true
	}
	var categorySlug string
	if facts.CategoryID != "" {
		if err := ValidateCategoryForHousehold(ctx, tx, facts.HouseholdID, facts.CategoryID); err != nil {
			return nil, err
		}
		if err := tx.QueryRow(ctx, `SELECT slug FROM category WHERE id=$1 AND household_id=$2`, facts.CategoryID, facts.HouseholdID).Scan(&categorySlug); err != nil {
			return nil, err
		}
		category = &facts.CategoryID
		categoryValid = true
	}
	if facts.TransactionAt != nil && facts.TransactionAt.IsZero() {
		return nil, &ErrMissingFacts{Facts: []string{"transaction_at"}}
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction SET transaction_at=COALESCE($3,transaction_at),category_id=COALESCE(NULLIF($4,'')::uuid,category_id),updated_at=now() WHERE id=$1 AND household_id=$2`, facts.TransactionID, facts.HouseholdID, facts.TransactionAt, facts.CategoryID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction_proposal p SET transaction_at=COALESCE($3,p.transaction_at),category_candidate_id=COALESCE(NULLIF($4,'')::uuid,p.category_candidate_id),metadata_json=p.metadata_json||CASE WHEN $3::timestamptz IS NULL THEN '{}'::jsonb ELSE '{"date_known":true,"date_source":"USER"}'::jsonb END,updated_at=now()
		WHERE p.household_id=$2 AND p.id IN(SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE')`, facts.TransactionID, facts.HouseholdID, facts.TransactionAt, facts.CategoryID); err != nil {
		return nil, err
	}
	var fallback bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction_proposal p JOIN transaction_evidence te ON p.id=NULLIF(te.metadata_json->>'proposal_id','')::uuid WHERE te.transaction_id=$1 AND p.household_id=$2 AND p.metadata_json->>'date_known'='false' AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE')`, facts.TransactionID, facts.HouseholdID).Scan(&fallback); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text,decision FROM review_item WHERE transaction_id=$1 AND household_id=$2 AND status IN ('OPEN','PENDING_SEND') ORDER BY id FOR UPDATE`, facts.TransactionID, facts.HouseholdID)
	if err != nil {
		return nil, err
	}
	type item struct {
		id       string
		decision reviewdec.Decision
	}
	var items []item
	for rows.Next() {
		var value item
		var raw []byte
		if err := rows.Scan(&value.id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(raw, &value.decision); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	var remaining []string
	add := func(fact string) {
		if !slices.Contains(remaining, fact) {
			remaining = append(remaining, fact)
		}
	}
	needsCategory := kind == "EXPENSE" && category == nil || !categoryValid
	if needsCategory {
		add("category")
	}
	if !wealthValid {
		add("wealth_account")
	}
	if fallback {
		add("transaction_at")
	}
	for _, value := range items {
		missing := []string{}
		for _, fact := range value.decision.MissingFacts {
			if fact == "category" && (facts.CategoryID != "" || kind == "TRANSFER" && categoryValid) || fact == "wealth_account" && wealthValid || fact == "transaction_at" && facts.TransactionAt != nil || fact == "duplicate_relationship" && (facts.Relationship == "DISTINCT" || facts.Relationship == "INCOME") || fact == "transfer_relationship" && facts.Relationship != "" && facts.Relationship != "DISTINCT" {
				continue
			}
			missing = append(missing, fact)
		}
		if needsCategory && !slices.Contains(missing, "category") {
			missing = append(missing, "category")
		}
		if !wealthValid && !slices.Contains(missing, "wealth_account") {
			missing = append(missing, "wealth_account")
		}
		if fallback && !slices.Contains(missing, "transaction_at") {
			missing = append(missing, "transaction_at")
		}
		for _, fact := range missing {
			if slices.Contains([]string{"category", "transaction_at", "duplicate_relationship", "transfer_relationship", "wealth_account"}, fact) {
				add(fact)
			}
		}
		known := map[string]any{}
		if facts.TransactionAt != nil {
			known["transaction_at"] = facts.TransactionAt.Format(time.RFC3339)
			known["transaction_time_source"] = "USER"
		}
		if facts.CategoryID != "" {
			known["category"] = categorySlug
		}
		if facts.Relationship != "" {
			known["relationship_choice"] = facts.Relationship
		}
		patch := map[string]any{"missingFacts": missing}
		// Once relationship ambiguity is answered, render the residual contract.
		if (facts.Relationship != "" || !categoryValid || !wealthValid) && len(missing) > 0 {
			reason := "TRANSACTION_FACTS_MISSING"
			switch {
			case slices.Contains(missing, "transfer_relationship") || slices.Contains(missing, "wealth_account"):
				reason = "TRANSFER_CLASSIFICATION"
			case len(missing) == 1 && missing[0] == "category":
				reason = "AMBIGUOUS_CATEGORY"
			case len(missing) == 1 && missing[0] == "transaction_at":
				reason = "MISSING_TRANSACTION_DATE"
			}
			if preset, ok := reviewdec.Preset(reason, "transaction", facts.TransactionID); ok {
				patch["reasonCode"] = reason
				patch["decisionClass"] = preset.DecisionClass
				patch["interactionMode"] = preset.InteractionMode
				patch["allowedActions"] = preset.AllowedActions
			}
		}
		encoded, err := json.Marshal(patch)
		if err != nil {
			return nil, err
		}
		knownJSON, err := json.Marshal(known)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_item SET decision=decision||$2::jsonb||jsonb_build_object('knownFacts',COALESCE(decision->'knownFacts','{}'::jsonb)||$3::jsonb),review_type=COALESCE($2::jsonb->>'reasonCode',review_type),updated_at=now() WHERE id=$1`, value.id, encoded, knownJSON); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_request SET review_type=(SELECT review_type FROM review_item WHERE id=$1) WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND')`, value.id); err != nil {
			return nil, err
		}
	}
	return remaining, nil
}
