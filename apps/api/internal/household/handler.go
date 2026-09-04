package household

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

type Handler struct {
	pool        *pgxpool.Pool
	botUsername string
	now         func() time.Time
}

func NewHandler(pool *pgxpool.Pool, botUsername string) *Handler {
	return &Handler{pool: pool, botUsername: strings.TrimPrefix(strings.TrimSpace(botUsername), "@"), now: time.Now}
}

func principal(r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		return auth.Principal{}, "", false
	}
	return p, p.Memberships[0].HouseholdID, true
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, householdID, ok := principal(r)
	if !ok {
		out(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	var response struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	if err := h.pool.QueryRow(r.Context(), `SELECT id,name,timezone FROM household WHERE id=$1`, householdID).Scan(&response.ID, &response.Name, &response.Timezone); err != nil {
		out(w, 500, map[string]string{"error": "unable to load household"})
		return
	}
	out(w, 200, response)
}

func (h *Handler) Members(w http.ResponseWriter, r *http.Request) {
	p, householdID, ok := principal(r)
	if !ok {
		out(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	if r.Method == http.MethodGet {
		h.listMembers(w, r, householdID)
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		out(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	var in struct {
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
	}
	if err := decode(r, &in); err != nil {
		out(w, 400, map[string]string{"error": "invalid member request"})
		return
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if len(in.DisplayName) < 1 || len(in.DisplayName) > 120 || len(in.Email) < 3 || len(in.Email) > 254 || !strings.Contains(in.Email, "@") {
		out(w, 400, map[string]string{"error": "invalid member fields"})
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		out(w, 500, map[string]string{"error": "unable to create member"})
		return
	}
	passwordHash, err := auth.HashPassword(base64.RawURLEncoding.EncodeToString(raw))
	if err != nil {
		out(w, 500, map[string]string{"error": "unable to create member"})
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		out(w, 500, map[string]string{"error": "unable to create member"})
		return
	}
	defer tx.Rollback(r.Context())
	var userID string
	if err = tx.QueryRow(r.Context(), `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES($1,$2,$3,NULL) RETURNING id`, in.Email, in.DisplayName, passwordHash).Scan(&userID); err != nil {
		out(w, 409, map[string]string{"error": "email already exists"})
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, householdID, userID); err != nil {
		out(w, 500, map[string]string{"error": "unable to create member"})
		return
	}
	if err = audit(r, tx, householdID, p.UserID, "CREATE", "household_member", userID, nil, map[string]any{"displayName": in.DisplayName, "email": in.Email, "role": "MEMBER", "active": true}); err != nil || tx.Commit(r.Context()) != nil {
		out(w, 500, map[string]string{"error": "unable to create member"})
		return
	}
	out(w, 201, map[string]any{"id": userID, "displayName": in.DisplayName, "email": in.Email, "role": "MEMBER", "active": true, "telegramConnected": false})
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request, householdID string) {
	rows, err := h.pool.Query(r.Context(), `SELECT u.id,u.display_name,u.email,hm.role,hm.active,ti.telegram_user_id IS NOT NULL,di.status,di.expires_at FROM household_member hm JOIN "user" u ON u.id=hm.user_id LEFT JOIN telegram_identity ti ON ti.household_id=hm.household_id AND ti.user_id=u.id AND ti.active LEFT JOIN LATERAL (SELECT status,expires_at FROM dashboard_account_invite WHERE household_id=hm.household_id AND user_id=u.id AND status='PENDING' ORDER BY created_at DESC LIMIT 1) di ON true WHERE hm.household_id=$1 ORDER BY hm.created_at`, householdID)
	if err != nil {
		out(w, 500, map[string]string{"error": "unable to list members"})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, email, role string
		var active, connected bool; var inviteStatus *string; var inviteExpires *time.Time
		if rows.Scan(&id, &name, &email, &role, &active, &connected, &inviteStatus, &inviteExpires) != nil {
			out(w, 500, map[string]string{"error": "unable to list members"})
			return
		}
		items = append(items, map[string]any{"id": id, "displayName": name, "email": email, "role": role, "active": active, "telegramConnected": connected, "dashboardInviteStatus": inviteStatus, "dashboardInviteExpiresAt": inviteExpires})
	}
	out(w, 200, items)
}

func (h *Handler) CreateDashboardInvite(w http.ResponseWriter, r *http.Request) {
	p, householdID, ok := principal(r); if !ok || p.Memberships[0].Role!="OWNER" { out(w,403,map[string]string{"error":"owner role required"}); return }
	memberID:=r.PathValue("id"); raw:=make([]byte,32); if _,err:=rand.Read(raw); err!=nil { out(w,500,map[string]string{"error":"unable to create invite"}); return }; token:=base64.RawURLEncoding.EncodeToString(raw); d:=sha256.Sum256([]byte(token)); exp:=h.now().Add(24*time.Hour)
	tx,err:=h.pool.Begin(r.Context()); if err!=nil {out(w,500,map[string]string{"error":"unable to create invite"});return}; defer tx.Rollback(r.Context())
	_,_=tx.Exec(r.Context(),`UPDATE dashboard_account_invite SET status='EXPIRED' WHERE household_id=$1 AND user_id=$2 AND status='PENDING' AND expires_at<=now()`,householdID,memberID)
	var id string; err=tx.QueryRow(r.Context(),`INSERT INTO dashboard_account_invite(household_id,user_id,token_hash,status,expires_at,created_by_user_id) SELECT $1,$2,$3,'PENDING',$4,$5 FROM household_member WHERE household_id=$1 AND user_id=$2 AND role='MEMBER' AND active RETURNING id`,householdID,memberID,d[:],exp,p.UserID).Scan(&id); if err!=nil {out(w,409,map[string]string{"error":"member is not eligible or already has a pending invite"});return}
	if audit(r,tx,householdID,p.UserID,"CREATE","dashboard_account_invite",id,nil,map[string]any{"userId":memberID,"expiresAt":exp})!=nil || tx.Commit(r.Context())!=nil {out(w,500,map[string]string{"error":"unable to create invite"});return}
	out(w,201,map[string]any{"id":id,"link":"/invite#"+token,"expiresAt":exp})
}

func (h *Handler) RevokeDashboardInvite(w http.ResponseWriter, r *http.Request) {
	p, householdID, ok := principal(r); if !ok || p.Memberships[0].Role!="OWNER" { out(w,403,map[string]string{"error":"owner role required"}); return }
	memberID:=r.PathValue("id"); if _,err:=h.pool.Exec(r.Context(),`UPDATE dashboard_account_invite SET status='REVOKED',revoked_at=now() WHERE household_id=$1 AND user_id=$2 AND status='PENDING'`,householdID,memberID); err!=nil {out(w,500,map[string]string{"error":"unable to revoke invite"});return}; w.WriteHeader(204)
}

func (h *Handler) PatchMember(w http.ResponseWriter, r *http.Request) {
	p, householdID, ok := principal(r)
	if !ok {
		out(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		out(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	var in struct {
		Active *bool `json:"active"`
	}
	if decode(r, &in) != nil || in.Active == nil || *in.Active {
		out(w, 400, map[string]string{"error": "only member deactivation is supported"})
		return
	}
	memberID := r.PathValue("id")
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		out(w, 500, map[string]string{"error": "unable to deactivate member"})
		return
	}
	defer tx.Rollback(r.Context())
	var role string
	if err = tx.QueryRow(r.Context(), `UPDATE household_member SET active=false,deactivated_at=now() WHERE household_id=$1 AND user_id=$2 AND role='MEMBER' AND active RETURNING role`, householdID, memberID).Scan(&role); err != nil {
		out(w, 404, map[string]string{"error": "active member not found"})
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE telegram_identity SET active=false,updated_at=now() WHERE household_id=$1 AND user_id=$2`, householdID, memberID)
	_, _ = tx.Exec(r.Context(), `UPDATE telegram_link_invite SET status='REVOKED',revoked_at=now() WHERE household_id=$1 AND user_id=$2 AND status='PENDING'`, householdID, memberID)
	if audit(r, tx, householdID, p.UserID, "DEACTIVATE", "household_member", memberID, map[string]any{"active": true}, map[string]any{"active": false}) != nil || tx.Commit(r.Context()) != nil {
		out(w, 500, map[string]string{"error": "unable to deactivate member"})
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	p, householdID, ok := principal(r)
	if !ok {
		out(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		out(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	memberID := r.PathValue("id")
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		out(w, 500, map[string]string{"error": "unable to create invite"})
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	expires := h.now().Add(15 * time.Minute)
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		out(w, 500, map[string]string{"error": "unable to create invite"})
		return
	}
	defer tx.Rollback(r.Context())
	_, _ = tx.Exec(r.Context(), `UPDATE telegram_link_invite SET status='EXPIRED' WHERE household_id=$1 AND user_id=$2 AND status='PENDING' AND expires_at<=now()`, householdID, memberID)
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO telegram_link_invite(household_id,user_id,token_hash,expires_at,created_by_user_id) SELECT $1,$2,$3,$4,$5 FROM household_member hm WHERE hm.household_id=$1 AND hm.user_id=$2 AND hm.role='MEMBER' AND hm.active AND NOT EXISTS(SELECT 1 FROM telegram_identity ti WHERE ti.household_id=$1 AND ti.user_id=$2 AND ti.active) RETURNING id`, householdID, memberID, digest[:], expires, p.UserID).Scan(&id)
	if err != nil {
		out(w, 409, map[string]string{"error": "member is not eligible or already has a pending invite"})
		return
	}
	if audit(r, tx, householdID, p.UserID, "CREATE", "telegram_link_invite", id, nil, map[string]any{"userId": memberID, "status": "PENDING", "expiresAt": expires}) != nil || tx.Commit(r.Context()) != nil {
		out(w, 500, map[string]string{"error": "unable to create invite"})
		return
	}
	link := ""
	if h.botUsername != "" {
		link = "https://t.me/" + url.PathEscape(h.botUsername) + "?start=" + url.QueryEscape(token)
	}
	out(w, 201, map[string]any{"id": id, "token": token, "link": link, "expiresAt": expires})
}

func (h *Handler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	p, householdID, ok := principal(r)
	if !ok {
		out(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		out(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	memberID := r.PathValue("id")
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		out(w, 500, map[string]string{"error": "unable to revoke invite"})
		return
	}
	defer tx.Rollback(r.Context())
	var inviteID string
	if err = tx.QueryRow(r.Context(), `UPDATE telegram_link_invite SET status='REVOKED',revoked_at=now() WHERE household_id=$1 AND user_id=$2 AND status='PENDING' RETURNING id`, householdID, memberID).Scan(&inviteID); err != nil {
		out(w, 404, map[string]string{"error": "pending invite not found"})
		return
	}
	if audit(r, tx, householdID, p.UserID, "REVOKE", "telegram_link_invite", inviteID, map[string]any{"status": "PENDING"}, map[string]any{"status": "REVOKED"}) != nil || tx.Commit(r.Context()) != nil {
		out(w, 500, map[string]string{"error": "unable to revoke invite"})
		return
	}
	w.WriteHeader(204)
}

func decode(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("one JSON value required")
	}
	return nil
}
func out(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func audit(r *http.Request, tx pgx.Tx, householdID, actorID, action, entityType, entityID string, before, after any) error {
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	_, err := tx.Exec(r.Context(), `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json) VALUES($1,'USER',$2,$3,$4,$5,$6::jsonb,$7::jsonb)`, householdID, actorID, action, entityType, entityID, b, a)
	return err
}
