package financialentity

import (
	"context"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

// Normalize is intentionally provider-neutral. It creates stable token identity
// for human hints while Go still rejects anything that is not uniquely resolved.
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
func matchScore(hint, candidate string) int {
	h, c := strings.Fields(Normalize(hint)), strings.Fields(Normalize(candidate))
	if len(h) == 0 || len(c) == 0 {
		return 0
	}
	if strings.Join(h, " ") == strings.Join(c, " ") {
		return 1000
	}
	set := map[string]bool{}
	for _, token := range h {
		set[token] = true
	}
	matches := 0
	for _, token := range c {
		if set[token] && len([]rune(token)) >= 3 {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	return matches*100 - len(c)
}
func unique(ctx context.Context, tx pgx.Tx, query string, household, hint string) (string, error) {
	rows, err := tx.Query(ctx, query, household)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	scores := map[string]int{}
	for rows.Next() {
		var id, name, institution, alias string
		if err = rows.Scan(&id, &name, &institution, &alias); err != nil {
			return "", err
		}
		score := matchScore(hint, name)
		if v := matchScore(hint, institution); v > score {
			score = v
		}
		if v := matchScore(hint, alias); v > score {
			score = v
		}
		if score > scores[id] {
			scores[id] = score
		}
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	best, winner, ties := 0, "", 0
	for id, score := range scores {
		if score > best {
			best, winner, ties = score, id, 1
		} else if score == best && score > 0 {
			ties++
		}
	}
	if best == 0 || ties != 1 {
		return "", nil
	}
	return winner, nil
}
func Account(ctx context.Context, tx pgx.Tx, household, hint string) (string, error) {
	return unique(ctx, tx, `SELECT a.id::text,a.name,COALESCE(a.name,''),COALESCE(fea.alias,'') FROM account a LEFT JOIN financial_entity_alias fea ON fea.account_id=a.id AND fea.active WHERE a.household_id=$1 AND a.active`, household, hint)
}
func WealthAccount(ctx context.Context, tx pgx.Tx, household, hint string) (string, error) {
	return unique(ctx, tx, `SELECT wa.id::text,wa.name,COALESCE(wa.institution,''),COALESCE(fea.alias,'') FROM wealth_account wa LEFT JOIN financial_entity_alias fea ON fea.wealth_account_id=wa.id AND fea.active WHERE wa.household_id=$1 AND wa.active`, household, hint)
}
