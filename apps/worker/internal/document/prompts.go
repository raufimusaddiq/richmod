package document

import (
	"encoding/json"
	"strings"
)

const documentMoneyRule = `Money: whole IDR digits only, no "Rp", separators, decimals or leading zeros; "Rp15.000,00" becomes "15000". Never invent or zero-fill an amount; use null only for an amount field that allows null and is not printed.`

const documentDateRule = `Dates are day-first (03/11/2026 is 3 November 2026) and may use Indonesian months: Januari, Februari, Maret, April, Mei, Juni, Juli, Agustus (Agu, Agt), September, Oktober (Okt), November, Desember (Des).`

const documentTimeRule = `transaction_at: RFC3339 with the +07:00 offset, e.g. "2026-11-03T14:05:00+07:00"; when only a date is printed use T00:00:00+07:00; null when no date is printed.`

const documentCategoryRule = `category_slug: choose from the supplied household categories (each a slug with its "Parent > Child" label). Pick the most specific child that fits; a parent only when no child fits; "lainnya" (if listed) only when the purchase is identified but fits no category; null only when nothing hints at a category.
category_confidence: 0.9-1 when the merchant or items make the category unambiguous; 0.5-0.8 when inferred from the merchant name alone; below 0.5 for a guess; 0 when category_slug is null.`

const documentConfidenceRule = `confidence: 0-1, how sure you are that the extracted facts match what is printed.`

const categorySlugDescription = "Most specific matching household category slug; parent only when no child fits; null only with no clue."

const categoryConfidenceDescription = "0.9-1 unambiguous; 0.5-0.8 inferred from the name; below 0.5 a guess; 0 when category_slug is null."

const wholeMoneyDescription = `Whole IDR digits, e.g. "15000".`

type categoryChoice struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

func categoryChoicesJSON(categories []categoryOption) string {
	choices := make([]categoryChoice, 0, len(categories))
	for _, category := range categories {
		choices = append(choices, categoryChoice{Slug: category.Slug, Label: category.Label})
	}
	encoded, _ := json.Marshal(choices)
	return string(encoded)
}

var repairIssueRules = map[string]string{
	"INVALID_AMOUNT":                "whole IDR digits, no separators, decimals or leading zeros; totals, amounts and net pay above 0; only other_components may start with -",
	"INVALID_FORMAT":                `transaction_at is RFC3339 with +07:00, e.g. "2026-11-03T14:05:00+07:00", or null`,
	"IMPLAUSIBLE_TIME":              "transaction_at is within the last 2 years and not after tomorrow; re-check the year and day-first order",
	"INVALID_CONFIDENCE":            "a number from 0 to 1",
	"INVALID_CURRENCY":              "currency is IDR",
	"INVALID_PERIOD":                "period is YYYY-MM, within the last 2 years and at most 60 days ahead",
	"INVALID_PAY_DATE":              "pay_date is YYYY-MM-DD, no earlier than 7 days before the period month and no later than the 7th day of the second month after it, or null",
	"TEXT_TOO_LONG":                 "merchant, account_hint and payment_method_hint at most 160 characters; description at most 500",
	"INVALID_TEXT":                  "a non-empty name as printed; employer at most 160 characters, item names at most 300",
	"INVALID_DIRECTION_OR_CURRENCY": "direction is OUT or IN and currency is IDR",
	"INVALID_ROW_COUNT":             "1 to 50 transaction rows",
	"INVALID_TRANSACTION_IDENTITY":  "follow the issue detail and the transaction_identifiers description",
}

func repairIssueLegend(fields []repairableField) string {
	seen := map[string]bool{}
	var parts []string
	for _, field := range fields {
		rule, ok := repairIssueRules[field.Issue]
		if !ok || seen[field.Issue] {
			continue
		}
		seen[field.Issue] = true
		parts = append(parts, field.Issue+" = "+rule)
	}
	return strings.Join(parts, "; ")
}
