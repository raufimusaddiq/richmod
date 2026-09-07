package financialentity

import (
	"context"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

type Status string

const (
	NoMatch   Status = "NO_MATCH"
	Resolved  Status = "RESOLVED"
	Ambiguous Status = "AMBIGUOUS"
)

type Resolution struct {
	ID     string
	Status Status
}

var lowInformation = map[string]bool{"account": true, "autodebit": true, "bank": true, "payment": true, "rekening": true, "transfer": true}

func Normalize(value string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(norm.NFKC.String(value))) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	return b.String()
}

func plausible(hint, candidate string) bool {
	hintTokens := map[string]bool{}
	for _, token := range strings.Fields(Normalize(hint)) {
		hintTokens[token] = true
	}
	for _, token := range strings.Fields(Normalize(candidate)) {
		if hintTokens[token] && len([]rune(token)) >= 3 && !lowInformation[token] {
			return true
		}
	}
	return false
}

func resolve(ctx context.Context, tx pgx.Tx, query, household, hint string) (Resolution, error) {
	rows, err := tx.Query(ctx, query, household)
	if err != nil {
		return Resolution{}, err
	}
	defer rows.Close()
	exact, candidates := map[string]bool{}, map[string]bool{}
	normalizedHint := Normalize(hint)
	for rows.Next() {
		var id, name, institution, alias string
		if err = rows.Scan(&id, &name, &institution, &alias); err != nil {
			return Resolution{}, err
		}
		if normalizedHint != "" && (normalizedHint == Normalize(name) || normalizedHint == Normalize(institution) || normalizedHint == Normalize(alias)) {
			exact[id] = true
		}
		if plausible(hint, name) || plausible(hint, institution) || plausible(hint, alias) {
			candidates[id] = true
		}
	}
	if err = rows.Err(); err != nil {
		return Resolution{}, err
	}
	if len(exact) == 1 {
		for id := range exact {
			return Resolution{ID: id, Status: Resolved}, nil
		}
	}
	if len(exact) > 1 {
		return Resolution{Status: Ambiguous}, nil
	}
	if len(candidates) == 1 {
		for id := range candidates {
			return Resolution{ID: id, Status: Resolved}, nil
		}
	}
	if len(candidates) > 1 {
		return Resolution{Status: Ambiguous}, nil
	}
	return Resolution{Status: NoMatch}, nil
}

func ResolveAccount(ctx context.Context, tx pgx.Tx, household, hint string) (Resolution, error) {
	return resolve(ctx, tx, `SELECT a.id::text,a.name,COALESCE(a.name,''),COALESCE(fea.alias,'') FROM account a LEFT JOIN financial_entity_alias fea ON fea.account_id=a.id AND fea.active WHERE a.household_id=$1 AND a.active`, household, hint)
}

func ResolveWealthAccount(ctx context.Context, tx pgx.Tx, household, hint string) (Resolution, error) {
	return resolve(ctx, tx, `SELECT wa.id::text,wa.name,COALESCE(wa.institution,''),COALESCE(fea.alias,'') FROM wealth_account wa LEFT JOIN financial_entity_alias fea ON fea.wealth_account_id=wa.id AND fea.active WHERE wa.household_id=$1 AND wa.active`, household, hint)
}

func Account(ctx context.Context, tx pgx.Tx, household, hint string) (string, error) {
	result, err := ResolveAccount(ctx, tx, household, hint)
	return result.ID, err
}

func WealthAccount(ctx context.Context, tx pgx.Tx, household, hint string) (string, error) {
	result, err := ResolveWealthAccount(ctx, tx, household, hint)
	return result.ID, err
}
