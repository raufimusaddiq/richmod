package settings

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func (h *Handler) Merchants(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		jsonError(w, 403, "household membership required")
		return
	}
	household := p.Memberships[0].HouseholdID
	if r.Method == http.MethodGet {
		rows, err := h.pool.Query(r.Context(), `SELECT id,normalized_name FROM merchant WHERE household_id=$1 ORDER BY normalized_name`, household)
		if err != nil {
			jsonError(w, 500, "unable to list merchants")
			return
		}
		defer rows.Close()
		out := make([]map[string]string, 0)
		for rows.Next() {
			var id, name string
			if rows.Scan(&id, &name) != nil {
				jsonError(w, 500, "unable to list merchants")
				return
			}
			out = append(out, map[string]string{"id": id, "normalizedName": name})
		}
		jsonOut(w, 200, out)
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	var in struct {
		NormalizedName string `json:"normalizedName"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		jsonError(w, 400, "invalid merchant request")
		return
	}
	in.NormalizedName = strings.TrimSpace(in.NormalizedName)
	if in.NormalizedName == "" {
		jsonError(w, 400, "normalizedName is required")
		return
	}
	var id string
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		jsonError(w, 500, "unable to create merchant")
		return
	}
	defer tx.Rollback(r.Context())
	if tx.QueryRow(r.Context(), `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, household, in.NormalizedName).Scan(&id) != nil {
		jsonError(w, 400, "unable to create merchant")
		return
	}
	if auditCreate(r.Context(), tx, household, p.UserID, "merchant", id, map[string]any{"normalizedName": in.NormalizedName}) != nil || tx.Commit(r.Context()) != nil {
		jsonError(w, 500, "unable to audit merchant")
		return
	}
	jsonOut(w, 201, map[string]string{"id": id})
}

func (h *Handler) CreateMerchantAlias(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		jsonError(w, 403, "household membership required")
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	household := p.Memberships[0].HouseholdID
	var in struct {
		RawName           string  `json:"rawName"`
		DefaultCategoryID *string `json:"defaultCategoryId"`
		AutoApply         bool    `json:"autoApply"`
		Confirmed         bool    `json:"confirmed"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		jsonError(w, 400, "invalid merchant alias request")
		return
	}
	in.RawName = strings.TrimSpace(in.RawName)
	if in.RawName == "" || !aliasPolicy(in.AutoApply, in.Confirmed) {
		jsonError(w, 400, "auto-apply requires explicit confirmation")
		return
	}
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		jsonError(w, 500, "unable to create merchant alias")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,$2,m.id,c.id,$5,$6 FROM merchant m LEFT JOIN category c ON c.id=$4 AND c.household_id=$1 WHERE m.id=$3 AND m.household_id=$1 RETURNING id`, household, in.RawName, r.PathValue("id"), in.DefaultCategoryID, in.AutoApply, in.Confirmed).Scan(&id)
	if err != nil {
		jsonError(w, 400, "unable to create merchant alias")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'CREATE','merchant_alias',$3,jsonb_build_object('raw_name',$4::text,'auto_apply',$5::boolean))`, household, p.UserID, id, in.RawName, in.AutoApply); err != nil {
		jsonError(w, 500, "unable to audit merchant alias")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		jsonError(w, 500, "unable to create merchant alias")
		return
	}
	jsonOut(w, 201, map[string]string{"id": id})
}

func aliasPolicy(autoApply, confirmed bool) bool { return !autoApply || confirmed }
