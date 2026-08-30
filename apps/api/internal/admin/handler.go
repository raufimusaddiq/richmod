package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

const (
	workerHealthyAfter = 60 * time.Second
	interactiveDueAge  = 5 * time.Second
	defaultDueAge      = 30 * time.Second
	backgroundDueAge   = 2 * time.Minute
)

type Handler struct {
	pool              *pgxpool.Pool
	gatewayConfigured bool
	protocol          string
}

func NewHandler(pool *pgxpool.Pool, gatewayConfigured bool, protocol string) *Handler {
	return &Handler{pool: pool, gatewayConfigured: gatewayConfigured, protocol: protocol}
}

func (h *Handler) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok || !principal.IsSuperAdmin {
			writeError(w, http.StatusForbidden, "SUPER_ADMIN_REQUIRED")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) Users(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT u.id,u.email,u.display_name,u.active,u.is_super_admin,u.password_initialized_at,u.created_at,
		       count(hm.user_id) FILTER (WHERE hm.active)
		FROM "user" u LEFT JOIN household_member hm ON hm.user_id=u.id
		GROUP BY u.id ORDER BY u.created_at DESC`)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, email, name string
		var active, super bool
		var initialized *time.Time
		var created time.Time
		var households int
		if err := rows.Scan(&id, &email, &name, &active, &super, &initialized, &created, &households); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		out = append(out, map[string]any{"id": id, "email": email, "displayName": name, "active": active, "isSuperAdmin": super, "passwordInitialized": initialized != nil, "createdAt": created, "households": households})
	}
	writeJSON(w, 200, out)
}

func (h *Handler) PatchUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Active       *bool `json:"active"`
		IsSuperAdmin *bool `json:"isSuperAdmin"`
	}
	if id == "" || json.NewDecoder(r.Body).Decode(&input) != nil || (input.Active == nil && input.IsSuperAdmin == nil) {
		writeError(w, 400, "INVALID_REQUEST")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "ADMIN_UPDATE_FAILED")
		return
	}
	defer tx.Rollback(r.Context())
	var email string
	var currentActive, currentSuper bool
	if err := tx.QueryRow(r.Context(), `SELECT email,active,is_super_admin FROM "user" WHERE id=$1 FOR UPDATE`, id).Scan(&email, &currentActive, &currentSuper); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, 404, "USER_NOT_FOUND")
		} else {
			writeError(w, 500, "ADMIN_UPDATE_FAILED")
		}
		return
	}
	nextActive, nextSuper := currentActive, currentSuper
	if input.Active != nil {
		nextActive = *input.Active
	}
	if input.IsSuperAdmin != nil {
		nextSuper = *input.IsSuperAdmin
	}
	if id == principal.UserID && (!nextActive || !nextSuper) {
		writeError(w, 409, "ADMIN_SELF_LOCKOUT")
		return
	}
	if currentActive && currentSuper && (!nextActive || !nextSuper) {
		rows, err := tx.Query(r.Context(), `SELECT id FROM "user" WHERE active AND is_super_admin FOR UPDATE`)
		if err != nil {
			writeError(w, 500, "ADMIN_UPDATE_FAILED")
			return
		}
		count := 0
		for rows.Next() {
			count++
		}
		rows.Close()
		if count <= 1 {
			writeError(w, 409, "FINAL_SUPER_ADMIN_REQUIRED")
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `UPDATE "user" SET active=$1,is_super_admin=$2,updated_at=now() WHERE id=$3`, nextActive, nextSuper, id); err != nil {
		writeError(w, 500, "ADMIN_UPDATE_FAILED")
		return
	}
	if currentActive != nextActive {
		action := "ADMIN_USER_DEACTIVATE"
		if nextActive {
			action = "ADMIN_USER_ACTIVATE"
		}
		if err := h.audit(r.Context(), tx, principal.UserID, action, "USER", id, map[string]any{"targetEmail": email, "oldActive": currentActive, "newActive": nextActive}, r.Header.Get("X-Request-ID")); err != nil {
			writeError(w, 500, "ADMIN_AUDIT_FAILED")
			return
		}
	}
	if currentSuper != nextSuper {
		action := "ADMIN_REVOKE_SUPERADMIN"
		if nextSuper {
			action = "ADMIN_GRANT_SUPERADMIN"
		}
		if err := h.audit(r.Context(), tx, principal.UserID, action, "USER", id, map[string]any{"targetEmail": email, "oldSuperAdmin": currentSuper, "newSuperAdmin": nextSuper}, r.Header.Get("X-Request-ID")); err != nil {
			writeError(w, 500, "ADMIN_AUDIT_FAILED")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "ADMIN_UPDATE_FAILED")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Households(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	like := "%" + strings.ToLower(q) + "%"
	rows, err := h.pool.Query(r.Context(), `
		SELECT h.id,h.name,h.timezone,h.created_at,count(DISTINCT hm.user_id) FILTER (WHERE hm.active),count(DISTINCT t.id),count(DISTINCT ri.id) FILTER (WHERE ri.status IN ('OPEN','PENDING_SEND')),max(se.created_at)
		FROM household h LEFT JOIN household_member hm ON hm.household_id=h.id LEFT JOIN transaction t ON t.household_id=h.id LEFT JOIN review_item ri ON ri.household_id=h.id LEFT JOIN source_event se ON se.household_id=h.id
		WHERE $1='' OR lower(h.name) LIKE $2 OR EXISTS(SELECT 1 FROM household_member m JOIN "user" u ON u.id=m.user_id WHERE m.household_id=h.id AND lower(u.email) LIKE $2)
		GROUP BY h.id ORDER BY h.created_at DESC`, q, like)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, tz string
		var created time.Time
		var members, transactions, reviews int
		var last *time.Time
		if err := rows.Scan(&id, &name, &tz, &created, &members, &transactions, &reviews, &last); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "timezone": tz, "createdAt": created, "members": members, "transactions": transactions, "openReviews": reviews, "lastActivityAt": last})
	}
	writeJSON(w, 200, out)
}

func (h *Handler) Members(w http.ResponseWriter, r *http.Request) {
	hid := r.PathValue("householdId")
	rows, err := h.pool.Query(r.Context(), `SELECT u.id,u.email,u.display_name,hm.role,hm.active FROM household_member hm JOIN "user" u ON u.id=hm.user_id WHERE hm.household_id=$1 ORDER BY u.email`, hid)
	if err != nil {
		writeError(w, 500, "ADMIN_QUERY_FAILED")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, email, name, role string
		var active bool
		if err := rows.Scan(&id, &email, &name, &role, &active); err != nil {
			writeError(w, 500, "ADMIN_QUERY_FAILED")
			return
		}
		out = append(out, map[string]any{"id": id, "email": email, "displayName": name, "role": role, "active": active})
	}
	writeJSON(w, 200, out)
}

func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	hid := r.PathValue("householdId")
	var input struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || !strings.Contains(input.Email, "@") || strings.TrimSpace(input.DisplayName) == "" {
		writeError(w, 400, "INVALID_REQUEST")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "ADMIN_UPDATE_FAILED")
		return
	}
	defer tx.Rollback(r.Context())
	var exists bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM household WHERE id=$1)`, hid).Scan(&exists); err != nil || !exists {
		writeError(w, 404, "HOUSEHOLD_NOT_FOUND")
		return
	}
	var userID string
	err = tx.QueryRow(r.Context(), `INSERT INTO "user"(email,display_name,password_hash) VALUES(lower(trim($1)),trim($2),'!') ON CONFLICT(email) DO UPDATE SET display_name="user".display_name RETURNING id`, input.Email, input.DisplayName).Scan(&userID)
	if err != nil {
		writeError(w, 409, "USER_CREATE_FAILED")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO household_member(household_id,user_id,role,active) VALUES($1,$2,'MEMBER',true) ON CONFLICT(household_id,user_id) DO UPDATE SET active=true`, hid, userID)
	if err != nil {
		writeError(w, 409, "MEMBERSHIP_CREATE_FAILED")
		return
	}
	if err := h.audit(r.Context(), tx, principal.UserID, "ADMIN_ADD_HOUSEHOLD_MEMBER", "HOUSEHOLD", hid, map[string]any{"memberEmail": strings.ToLower(strings.TrimSpace(input.Email)), "role": "MEMBER"}, r.Header.Get("X-Request-ID")); err != nil {
		writeError(w, 500, "ADMIN_AUDIT_FAILED")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "ADMIN_UPDATE_FAILED")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) audit(ctx context.Context, tx pgx.Tx, actor, action, entityType, entityID string, metadata map[string]any, requestID string) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO platform_audit_log(actor_user_id,action,entity_type,entity_id,metadata_json,request_id) VALUES($1,$2,$3,$4,$5,CASE WHEN $6 ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' THEN $6::uuid ELSE NULL END)`, actor, action, entityType, entityID, raw, requestID)
	return err
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
