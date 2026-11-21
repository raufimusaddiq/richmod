package ledger

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

const (
	quickScanLimit       = 300
	quickFrequentLimit   = 5
	quickCategoryLimit   = 6
	quickDescriptionKeys = 50
)

type quickRow struct {
	Description string
	Amount      string
	Type        string
	CategoryID  string
	AccountID   string
}

type quickFrequent struct {
	Description string  `json:"description"`
	Amount      string  `json:"amount"`
	Type        string  `json:"type"`
	CategoryID  *string `json:"categoryId"`
	AccountID   *string `json:"accountId"`
}

type quickSuggestions struct {
	Frequent         []quickFrequent   `json:"frequent"`
	RecentCategories []string          `json:"recentCategories"`
	DescriptionMap   map[string]string `json:"descriptionMap"`
}

func collapseSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeDescription(value string) string {
	return strings.ToLower(collapseSpaces(value))
}

func normalizeQuickAmount(value string) string {
	trimmed := strings.TrimSpace(value)
	if dot := strings.IndexByte(trimmed, '.'); dot >= 0 {
		trimmed = trimmed[:dot]
	}
	return trimmed
}

func nonEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func buildQuickSuggestions(frequentRows, historyRows []quickRow) quickSuggestions {
	result := quickSuggestions{Frequent: []quickFrequent{}, RecentCategories: []string{}, DescriptionMap: map[string]string{}}

	type group struct {
		first, count int
		row          quickRow
		amount       string
	}
	groups := map[string]*group{}
	var groupOrder []string

	for index, row := range frequentRows {
		key := normalizeDescription(row.Description)
		amount := normalizeQuickAmount(row.Amount)
		if key == "" || amount == "" {
			continue
		}
		groupKey := row.Type + "|" + amount + "|" + key
		if existing, ok := groups[groupKey]; ok {
			existing.count++
		} else {
			groups[groupKey] = &group{first: index, count: 1, row: row, amount: amount}
			groupOrder = append(groupOrder, groupKey)
		}
	}

	sort.SliceStable(groupOrder, func(i, j int) bool {
		a, b := groups[groupOrder[i]], groups[groupOrder[j]]
		if a.count != b.count {
			return a.count > b.count
		}
		return a.first < b.first
	})
	for _, key := range groupOrder {
		if len(result.Frequent) == quickFrequentLimit {
			break
		}
		entry := groups[key]
		row := entry.row
		result.Frequent = append(result.Frequent, quickFrequent{
			Description: collapseSpaces(row.Description),
			Amount:      entry.amount,
			Type:        row.Type,
			CategoryID:  nonEmpty(row.CategoryID),
			AccountID:   nonEmpty(row.AccountID),
		})
	}

	categoryCount := map[string]int{}
	categoryFirst := map[string]int{}
	var categoryOrder []string
	keyCategory := map[string]map[string]int{}
	var keyOrder []string

	for index, row := range historyRows {
		key := normalizeDescription(row.Description)
		if key == "" || row.CategoryID == "" {
			continue
		}
		if _, ok := categoryFirst[row.CategoryID]; !ok {
			categoryFirst[row.CategoryID] = index
			categoryOrder = append(categoryOrder, row.CategoryID)
		}
		categoryCount[row.CategoryID]++
		if keyCategory[key] == nil {
			keyCategory[key] = map[string]int{}
			keyOrder = append(keyOrder, key)
		}
		keyCategory[key][row.CategoryID]++
	}

	sort.SliceStable(categoryOrder, func(i, j int) bool {
		a, b := categoryOrder[i], categoryOrder[j]
		if categoryCount[a] != categoryCount[b] {
			return categoryCount[a] > categoryCount[b]
		}
		return categoryFirst[a] < categoryFirst[b]
	})
	if len(categoryOrder) > quickCategoryLimit {
		categoryOrder = categoryOrder[:quickCategoryLimit]
	}
	result.RecentCategories = append(result.RecentCategories, categoryOrder...)

	if len(keyOrder) > quickDescriptionKeys {
		keyOrder = keyOrder[:quickDescriptionKeys]
	}
	for _, key := range keyOrder {
		best, bestCount := "", 0
		for _, row := range historyRows {
			if normalizeDescription(row.Description) != key || row.CategoryID == "" {
				continue
			}
			if count := keyCategory[key][row.CategoryID]; count > bestCount {
				best, bestCount = row.CategoryID, count
			}
		}
		if best != "" {
			result.DescriptionMap[key] = best
		}
	}
	return result
}

func (h *Handler) queryQuickRows(ctx context.Context, query string, householdID string) ([]quickRow, error) {
	rows, err := h.pool.Query(ctx, query, householdID, quickScanLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scanned []quickRow
	for rows.Next() {
		var row quickRow
		if err := rows.Scan(&row.Description, &row.Amount, &row.Type, &row.CategoryID, &row.AccountID); err != nil {
			return nil, err
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return scanned, nil
}

const quickFrequentQuery = `
	SELECT t.description, t.amount::text, t.type,
	       CASE WHEN c.active THEN t.category_id::text ELSE '' END,
	       CASE WHEN a.active THEN t.account_id::text ELSE '' END
	FROM transaction t
	JOIN LATERAL (
		SELECT s.source_type
		FROM transaction_evidence te
		JOIN source_event s ON s.id = te.source_event_id AND s.household_id = t.household_id
		WHERE te.transaction_id = t.id
		  AND COALESCE(te.metadata_json->>'link_status', 'ACTIVE') = 'ACTIVE'
		ORDER BY te.created_at, te.id
		LIMIT 1
	) e ON e.source_type IN ('WEB_MANUAL', 'TELEGRAM_TEXT')
	LEFT JOIN category c ON c.id = t.category_id AND c.household_id = t.household_id
	LEFT JOIN account a ON a.id = t.account_id AND a.household_id = t.household_id
	WHERE t.household_id = $1 AND t.status = 'CONFIRMED' AND t.type IN ('INCOME','EXPENSE')
	  AND btrim(COALESCE(t.description, '')) <> ''
	ORDER BY t.transaction_at DESC, t.id DESC
	LIMIT $2`

const quickHistoryQuery = `
	SELECT t.description, t.amount::text, t.type,
	       CASE WHEN c.active THEN t.category_id::text ELSE '' END,
	       CASE WHEN a.active THEN t.account_id::text ELSE '' END
	FROM transaction t
	LEFT JOIN category c ON c.id = t.category_id AND c.household_id = t.household_id
	LEFT JOIN account a ON a.id = t.account_id AND a.household_id = t.household_id
	WHERE t.household_id = $1 AND t.status = 'CONFIRMED' AND t.type IN ('INCOME','EXPENSE')
	  AND btrim(COALESCE(t.description, '')) <> ''
	ORDER BY t.transaction_at DESC, t.id DESC
	LIMIT $2`

func (h *Handler) QuickSuggestions(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principal.HasHousehold {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	frequentRows, err := h.queryQuickRows(r.Context(), quickFrequentQuery, principal.HouseholdID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load suggestions"})
		return
	}
	historyRows, err := h.queryQuickRows(r.Context(), quickHistoryQuery, principal.HouseholdID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load suggestions"})
		return
	}
	writeJSON(w, 200, buildQuickSuggestions(frequentRows, historyRows))
}
