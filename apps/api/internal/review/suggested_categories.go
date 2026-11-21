package review

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
)

const (
	suggestionMerchantHistory = "MERCHANT_HISTORY"
	suggestionHouseholdRecent = "HOUSEHOLD_RECENT"
	maxSuggestedCategories    = 3
	householdRecentWindowDays = 90
)

type suggestedCategory struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Reason string `json:"reason"`
}

func needsCategorySuggestions(value item) bool {
	if value.Type != "EXPENSE" && value.Type != "INCOME" && value.Type != "REFUND" {
		return false
	}
	return value.CategoryID == nil || containsString(value.MissingFacts, "category")
}

func normalizedMerchantKey(values ...*string) string {
	for _, value := range values {
		if value != nil {
			if key := strings.ToLower(strings.Join(strings.Fields(*value), " ")); key != "" {
				return key
			}
		}
	}
	return ""
}

func (h *Handler) suggestCategories(ctx context.Context, household string, items []item) (map[string][]suggestedCategory, error) {
	result := map[string][]suggestedCategory{}
	var ids, keys, kinds []string
	var txIDs, merchantIDs []pgtype.UUID
	kindSet := map[string]bool{}
	for _, value := range items {
		if !needsCategorySuggestions(value) {
			continue
		}
		ids = append(ids, value.ID)
		txIDs = append(txIDs, toUUID(value.ID))
		merchantIDs = append(merchantIDs, toUUID(stringValue(value.MerchantID)))
		keys = append(keys, normalizedMerchantKey(value.MerchantName, value.Counterparty, value.Description))
		kinds = append(kinds, value.Type)
		kindSet[value.Type] = true
	}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := h.pool.Query(ctx, `
		WITH q AS (
			SELECT * FROM unnest($2::uuid[], $3::uuid[], $4::text[], $5::text[]) AS q(tx_id, merchant_id, key, kind)
		), history AS (
			SELECT q.tx_id, t.category_id, c.name, count(*)::int AS uses, max(t.transaction_at) AS last_at
			FROM q
			JOIN transaction t ON t.household_id=$1 AND t.status='CONFIRMED' AND t.type=q.kind AND t.category_id IS NOT NULL AND t.id<>q.tx_id
			JOIN category c ON c.id=t.category_id AND c.household_id=$1 AND c.active
			WHERE (q.merchant_id IS NOT NULL AND t.merchant_id=q.merchant_id)
			   OR (q.key<>'' AND lower(btrim(regexp_replace(COALESCE(t.counterparty_name,''),'[[:space:]]+',' ','g')))=q.key)
			   OR (q.key<>'' AND lower(btrim(regexp_replace(COALESCE(t.description,''),'[[:space:]]+',' ','g')))=q.key)
			GROUP BY q.tx_id, t.category_id, c.name
		)
		SELECT tx_id::text, category_id::text, name, uses
		FROM (
			SELECT h.*, row_number() OVER (PARTITION BY tx_id ORDER BY uses DESC, last_at DESC, name, category_id) AS rank FROM history h
		) ranked WHERE rank<=$6 ORDER BY tx_id, rank`, household, txIDs, merchantIDs, keys, kinds, maxSuggestedCategories)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var txID string
		value := suggestedCategory{Reason: suggestionMerchantHistory}
		if err := rows.Scan(&txID, &value.ID, &value.Name, &value.Count); err != nil {
			return nil, err
		}
		result[txID] = append(result[txID], value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	recent := map[string][]suggestedCategory{}
	for kind := range kindSet {
		fill, err := h.pool.Query(ctx, `
			SELECT t.category_id::text, c.name, count(*)::int
			FROM transaction t JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id AND c.active
			WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type=$2 AND t.category_id IS NOT NULL
			  AND t.transaction_at >= now() - make_interval(days => $3)
			GROUP BY t.category_id, c.name ORDER BY count(*) DESC, max(t.transaction_at) DESC, c.name, t.category_id LIMIT $4`, household, kind, householdRecentWindowDays, maxSuggestedCategories)
		if err != nil {
			return nil, err
		}
		for fill.Next() {
			value := suggestedCategory{Reason: suggestionHouseholdRecent}
			if err := fill.Scan(&value.ID, &value.Name, &value.Count); err != nil {
				fill.Close()
				return nil, err
			}
			recent[kind] = append(recent[kind], value)
		}
		if err := fill.Err(); err != nil {
			return nil, err
		}
	}
	for index, id := range ids {
		picked := result[id]
		seen := map[string]bool{}
		for _, value := range picked {
			seen[value.ID] = true
		}
		for _, value := range recent[kinds[index]] {
			if len(picked) >= maxSuggestedCategories {
				break
			}
			if !seen[value.ID] {
				picked = append(picked, value)
				seen[value.ID] = true
			}
		}
		result[id] = picked
	}
	return result, nil
}

func toUUID(value string) pgtype.UUID {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}
}
