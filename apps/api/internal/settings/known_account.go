package settings

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

var maskedAccountHint = regexp.MustCompile(`^[0-9]{4,19}$`)

func (h *Handler) KnownAccounts(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		jsonError(w, 403, "household membership required")
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	household := p.HouseholdID
	if r.Method == http.MethodGet {
		rows, err := h.pool.Query(r.Context(), `SELECT id,user_id,institution,display_name,match_hint,relationship,active,wealth_account_id FROM known_account WHERE household_id=$1 ORDER BY institution,display_name`, household)
		if err != nil {
			jsonError(w, 500, "unable to list known accounts")
			return
		}
		defer rows.Close()
		items := make([]map[string]any, 0)
		for rows.Next() {
			var id, institution, name, hint, relationship string
			var userID, wealthAccountID *string
			var active bool
			if rows.Scan(&id, &userID, &institution, &name, &hint, &relationship, &active, &wealthAccountID) != nil {
				jsonError(w, 500, "unable to list known accounts")
				return
			}
			items = append(items, map[string]any{"id": id, "userId": userID, "institution": institution, "displayName": name, "matchHint": hint, "relationship": relationship, "active": active, "wealthAccountId": wealthAccountID})
		}
		jsonOut(w, 200, items)
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	var in struct {
		UserID          *string `json:"userId"`
		Institution     string  `json:"institution"`
		DisplayName     string  `json:"displayName"`
		MatchHint       string  `json:"matchHint"`
		Relationship    string  `json:"relationship"`
		WealthAccountID *string `json:"wealthAccountId"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		jsonError(w, 400, "invalid known account request")
		return
	}
	in.Institution = strings.TrimSpace(in.Institution)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.MatchHint = strings.TrimSpace(in.MatchHint)
	if in.Institution == "" || in.DisplayName == "" || !maskedAccountHint.MatchString(in.MatchHint) || !oneOf(in.Relationship, "OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT", "INVESTMENT_ACCOUNT", "OTHER") {
		jsonError(w, 400, "invalid known account fields")
		return
	}
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		jsonError(w, 500, "unable to create known account")
		return
	}
	defer tx.Rollback(r.Context())
	if in.WealthAccountID != nil {
		var exists bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM wealth_account WHERE id=$1 AND household_id=$2)`, *in.WealthAccountID, household).Scan(&exists); err != nil || !exists {
			jsonError(w, 400, "wealthAccountId must identify a household wealth account")
			return
		}
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO known_account(household_id,user_id,institution,display_name,match_hint,relationship,wealth_account_id) SELECT $1,u.id,$3,$4,$5,$6,$7 FROM (SELECT $2::uuid AS requested_id) input LEFT JOIN "user" u ON u.id=input.requested_id AND EXISTS(SELECT 1 FROM household_member hm WHERE hm.household_id=$1 AND hm.user_id=u.id AND hm.active) WHERE input.requested_id IS NULL OR u.id IS NOT NULL RETURNING id`, household, in.UserID, in.Institution, in.DisplayName, in.MatchHint, in.Relationship, in.WealthAccountID).Scan(&id)
	if err != nil {
		jsonError(w, 400, "unable to create known account")
		return
	}
	if auditCreate(r.Context(), tx, household, p.UserID, "known_account", id, map[string]any{"institution": in.Institution, "displayName": in.DisplayName, "matchHint": in.MatchHint, "relationship": in.Relationship, "wealthAccountId": in.WealthAccountID}) != nil || tx.Commit(r.Context()) != nil {
		jsonError(w, 500, "unable to audit known account")
		return
	}
	jsonOut(w, 201, map[string]string{"id": id})
}

func (h *Handler) PatchKnownAccount(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		jsonError(w, 403, "household membership required")
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	var in struct {
		Active          *bool   `json:"active"`
		WealthAccountID *string `json:"wealthAccountId"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || (in.Active == nil && in.WealthAccountID == nil) {
		jsonError(w, 400, "active or wealthAccountId is required")
		return
	}
	household := p.HouseholdID
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		jsonError(w, 500, "unable to update known account")
		return
	}
	defer tx.Rollback(r.Context())
	if in.WealthAccountID != nil {
		var exists bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM wealth_account WHERE id=$1 AND household_id=$2)`, *in.WealthAccountID, household).Scan(&exists); err != nil || !exists {
			jsonError(w, 400, "wealthAccountId must identify a household wealth account")
			return
		}
	}
	var id string
	if err = tx.QueryRow(r.Context(), `UPDATE known_account SET active=COALESCE($3,active),wealth_account_id=CASE WHEN $4::boolean THEN $5::uuid ELSE wealth_account_id END,updated_at=now() WHERE id=$1 AND household_id=$2 RETURNING id`, r.PathValue("id"), household, in.Active, in.WealthAccountID != nil, in.WealthAccountID).Scan(&id); err != nil {
		jsonError(w, 404, "known account not found")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'UPDATE','known_account',$3,jsonb_build_object('active',$4::boolean,'wealthAccountId',$5::text))`, household, p.UserID, id, in.Active, in.WealthAccountID); err != nil || tx.Commit(r.Context()) != nil {
		jsonError(w, 500, "unable to update known account")
		return
	}
	w.WriteHeader(204)
}
