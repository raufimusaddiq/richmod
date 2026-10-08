package settings

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func (h *Handler) MerchantAliases(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		jsonError(w, 403, "household membership required")
		return
	}
	household := p.HouseholdID
	rows, err := h.pool.Query(r.Context(), `SELECT ma.id,ma.raw_name,m.normalized_name,ma.default_category_id,c.name,ma.auto_apply,ma.created_from_user_confirmation FROM merchant_alias ma JOIN merchant m ON m.id=ma.normalized_merchant_id LEFT JOIN category c ON c.id=ma.default_category_id WHERE ma.household_id=$1 ORDER BY ma.raw_name`, household)
	if err != nil {
		jsonError(w, 500, "unable to list merchant aliases")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, raw, name string
		var categoryID, categoryName *string
		var autoApply, confirmed bool
		if rows.Scan(&id, &raw, &name, &categoryID, &categoryName, &autoApply, &confirmed) != nil {
			jsonError(w, 500, "unable to list merchant aliases")
			return
		}
		items = append(items, map[string]any{"id": id, "rawName": raw, "normalizedName": name, "defaultCategoryId": categoryID, "defaultCategoryName": categoryName, "autoApply": autoApply, "createdFromUserConfirmation": confirmed})
	}
	if err := rows.Err(); err != nil {
		jsonError(w, 500, "unable to list merchant aliases")
		return
	}
	jsonOut(w, 200, items)
}

func (h *Handler) PatchMerchantAlias(w http.ResponseWriter, r *http.Request) {
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
		AutoApply *bool `json:"autoApply"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.AutoApply == nil {
		jsonError(w, 400, "autoApply is required")
		return
	}
	household := p.HouseholdID
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		jsonError(w, 500, "unable to update merchant alias")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	if err = tx.QueryRow(r.Context(), `UPDATE merchant_alias SET auto_apply=$3 WHERE id=$1 AND household_id=$2 RETURNING id`, r.PathValue("id"), household, *in.AutoApply).Scan(&id); err != nil {
		jsonError(w, 404, "merchant alias not found")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'UPDATE','merchant_alias',$3,jsonb_build_object('auto_apply',$4::boolean))`, household, p.UserID, id, *in.AutoApply); err != nil {
		jsonError(w, 500, "unable to update merchant alias")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		jsonError(w, 500, "unable to update merchant alias")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
