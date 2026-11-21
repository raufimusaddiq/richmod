package ledger

import (
	"net/http"
	"sort"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// Quick suggestions feed the global "+ Catat" sheet. Everything here is a
// deterministic read over the household's own confirmed history: no AI, no
// writes, and only active categories/accounts are ever suggested.
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

// normalizeDescription is the key shared with the web client: lower-case with
// whitespace collapsed.
func normalizeDescription(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func nonEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// buildQuickSuggestions aggregates rows that are already ordered newest first.
func buildQuickSuggestions(rows []quickRow) quickSuggestions {
	result := quickSuggestions{Frequent: []quickFrequent{}, RecentCategories: []string{}, DescriptionMap: map[string]string{}}

	type group struct {
		first, count int
		row          quickRow
	}
	groups := map[string]*group{}
	var groupOrder []string
	categoryCount := map[string]int{}
	categoryFirst := map[string]int{}
	var categoryOrder []string
	keyCategory := map[string]map[string]int{}
	var keyOrder []string

	for index, row := range rows {
		key := normalizeDescription(row.Description)
		if key == "" {
			continue
		}
		groupKey := row.Type + "|" + row.Amount + "|" + key
		if existing, ok := groups[groupKey]; ok {
			existing.count++
		} else {
			groups[groupKey] = &group{first: index, count: 1, row: row}
			groupOrder = append(groupOrder, groupKey)
		}
		if row.CategoryID == "" {
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
		row := groups[key].row
		result.Frequent = append(result.Frequent, quickFrequent{Description: strings.TrimSpace(row.Description), Amount: row.Amount, Type: row.Type, CategoryID: nonEmpty(row.CategoryID), AccountID: nonEmpty(row.AccountID)})
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

	// keyOrder is newest first, so the cap keeps the most recent patterns.
	if len(keyOrder) > quickDescriptionKeys {
		keyOrder = keyOrder[:quickDescriptionKeys]
	}
	for _, key := range keyOrder {
		best, bestCount := "", 0
		for _, row := range rows {
			// Walk newest first so a tie goes to the most recent category.
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

func (h *Handler) QuickSuggestions(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principal.HasHousehold {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT t.description, t.amount::text, t.type,
		       CASE WHEN c.active THEN t.category_id::text ELSE '' END,
		       CASE WHEN a.active THEN t.account_id::text ELSE '' END
		FROM transaction t
		LEFT JOIN category c ON c.id = t.category_id AND c.household_id = t.household_id
		LEFT JOIN account a ON a.id = t.account_id AND a.household_id = t.household_id
		WHERE t.household_id = $1 AND t.status = 'CONFIRMED' AND t.type IN ('INCOME','EXPENSE')
		  AND btrim(COALESCE(t.description, '')) <> ''
		ORDER BY t.transaction_at DESC, t.id DESC
		LIMIT $2`, principal.HouseholdID, quickScanLimit)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load suggestions"})
		return
	}
	defer rows.Close()
	var scanned []quickRow
	for rows.Next() {
		var row quickRow
		if err := rows.Scan(&row.Description, &row.Amount, &row.Type, &row.CategoryID, &row.AccountID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to load suggestions"})
			return
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load suggestions"})
		return
	}
	writeJSON(w, 200, buildQuickSuggestions(scanned))
}
