package settings

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

func (h *Handler) Accounts(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		jsonError(w, 403, "household membership required")
		return
	}
	household := p.Memberships[0].HouseholdID
	if r.Method == http.MethodGet {
		rows, err := h.pool.Query(r.Context(), `SELECT id,name,account_type,tracking_policy,active FROM account WHERE household_id=$1 ORDER BY name`, household)
		if err != nil {
			jsonError(w, 500, "unable to list accounts")
			return
		}
		defer rows.Close()
		var out []map[string]any
		for rows.Next() {
			var id, n, t, policy string
			var active bool
			if rows.Scan(&id, &n, &t, &policy, &active) != nil {
				jsonError(w, 500, "unable to list accounts")
				return
			}
			out = append(out, map[string]any{"id": id, "name": n, "accountType": t, "trackingPolicy": policy, "active": active})
		}
		jsonOut(w, 200, out)
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	var in struct {
		Name           string `json:"name"`
		AccountType    string `json:"accountType"`
		TrackingPolicy string `json:"trackingPolicy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, 400, "invalid account request")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || !oneOf(in.AccountType, "BANK", "CASH", "EWALLET", "OTHER") || !oneOf(in.TrackingPolicy, "FULL_LEDGER", "SPENDING_ONLY", "REFERENCE_ONLY") {
		jsonError(w, 400, "invalid account fields")
		return
	}
	var id string
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		jsonError(w, 500, "unable to create account")
		return
	}
	defer tx.Rollback(r.Context())
	if err := tx.QueryRow(r.Context(), `INSERT INTO account (household_id,name,account_type,tracking_policy) VALUES($1,$2,$3,$4) RETURNING id`, household, in.Name, in.AccountType, in.TrackingPolicy).Scan(&id); err != nil {
		jsonError(w, 400, "unable to create account")
		return
	}
	if auditCreate(r.Context(), tx, household, p.UserID, "account", id, map[string]any{"name": in.Name, "accountType": in.AccountType, "trackingPolicy": in.TrackingPolicy}) != nil || tx.Commit(r.Context()) != nil {
		jsonError(w, 500, "unable to audit account")
		return
	}
	jsonOut(w, 201, map[string]string{"id": id})
}

func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		jsonError(w, 403, "household membership required")
		return
	}
	household := p.Memberships[0].HouseholdID
	if r.Method == http.MethodGet {
		rows, err := h.pool.Query(r.Context(), `SELECT id,parent_id,name,slug,active,sort_order FROM category WHERE household_id=$1 ORDER BY sort_order,name`, household)
		if err != nil {
			jsonError(w, 500, "unable to list categories")
			return
		}
		defer rows.Close()
		var out []map[string]any
		for rows.Next() {
			var id, name, slug string
			var parent *string
			var active bool
			var sort int
			if rows.Scan(&id, &parent, &name, &slug, &active, &sort) != nil {
				jsonError(w, 500, "unable to list categories")
				return
			}
			out = append(out, map[string]any{"id": id, "parentId": parent, "name": name, "slug": slug, "active": active, "sortOrder": sort})
		}
		jsonOut(w, 200, out)
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	var in struct {
		Name     string  `json:"name"`
		Slug     string  `json:"slug"`
		ParentID *string `json:"parentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, 400, "invalid category request")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Name == "" || in.Slug == "" {
		jsonError(w, 400, "name and slug are required")
		return
	}
	var id string
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		jsonError(w, 500, "unable to create category")
		return
	}
	defer tx.Rollback(r.Context())
	if err := tx.QueryRow(r.Context(), `INSERT INTO category(household_id,parent_id,name,slug) VALUES($1,$2,$3,$4) RETURNING id`, household, in.ParentID, in.Name, in.Slug).Scan(&id); err != nil {
		jsonError(w, 400, "unable to create category")
		return
	}
	if auditCreate(r.Context(), tx, household, p.UserID, "category", id, map[string]any{"name": in.Name, "slug": in.Slug, "parentId": in.ParentID}) != nil || tx.Commit(r.Context()) != nil {
		jsonError(w, 500, "unable to audit category")
		return
	}
	jsonOut(w, 201, map[string]string{"id": id})
}
func owner(p auth.Principal) bool { return p.Memberships[0].Role == "OWNER" }
func oneOf(v string, xs ...string) bool {
	for _, x := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func jsonError(w http.ResponseWriter, c int, m string) { jsonOut(w, c, map[string]string{"error": m}) }
func jsonOut(w http.ResponseWriter, c int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(c)
	_ = json.NewEncoder(w).Encode(v)
}
