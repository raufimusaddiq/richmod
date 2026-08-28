package settings

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"net/http"
	"net/mail"
	"strings"
)

func (h *Handler) BankEmailListeners(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		jsonError(w, 403, "household membership required")
		return
	}
	hid := p.Memberships[0].HouseholdID
	if r.Method == http.MethodGet {
		rows, e := h.pool.Query(r.Context(), `SELECT id,bank_name,sender_address,active FROM bank_email_listener WHERE household_id=$1 ORDER BY bank_name,sender_address`, hid)
		if e != nil {
			jsonError(w, 500, "unable to list bank email listeners")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, b, s string
			var a bool
			if rows.Scan(&id, &b, &s, &a) != nil {
				jsonError(w, 500, "unable to list bank email listeners")
				return
			}
			out = append(out, map[string]any{"id": id, "bankName": b, "senderAddress": s, "active": a, "trackingPolicy": "SPENDING_ONLY"})
		}
		jsonOut(w, 200, out)
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	if r.Method == http.MethodPost {
		var in struct {
			BankName      string `json:"bankName"`
			SenderAddress string `json:"senderAddress"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			jsonError(w, 400, "invalid listener request")
			return
		}
		in.BankName = strings.TrimSpace(in.BankName)
		in.SenderAddress = strings.ToLower(strings.TrimSpace(in.SenderAddress))
		if in.BankName == "" || len(in.BankName) > 120 || !validEmail(in.SenderAddress) {
			jsonError(w, 400, "invalid listener fields")
			return
		}
		tx, e := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
		if e != nil {
			jsonError(w, 500, "unable to create listener")
			return
		}
		defer tx.Rollback(r.Context())
		var aid, lid string
		accountKey := "bank-email:" + in.SenderAddress
		e = tx.QueryRow(r.Context(), `SELECT id FROM account WHERE household_id=$1 AND system_key=$2`, hid, accountKey).Scan(&aid)
		if e == pgx.ErrNoRows {
			e = tx.QueryRow(r.Context(), `INSERT INTO account(household_id,name,account_type,tracking_policy,system_managed,system_key) VALUES($1,$2,'BANK','SPENDING_ONLY',true,$3) RETURNING id`, hid, "Bank · "+in.BankName, accountKey).Scan(&aid)
		} else if e == nil {
			_, e = tx.Exec(r.Context(), `UPDATE account SET active=true,tracking_policy='SPENDING_ONLY',updated_at=now() WHERE id=$1`, aid)
		}
		if e == nil {
			e = tx.QueryRow(r.Context(), `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,account_id,created_by_user_id) VALUES($1,$2,$3,$4,$5) RETURNING id`, hid, in.BankName, in.SenderAddress, aid, p.UserID).Scan(&lid)
		}
		if e != nil {
			jsonError(w, 409, "active sender already exists")
			return
		}
		if auditCreate(r.Context(), tx, hid, p.UserID, "bank_email_listener", lid, map[string]any{"bankName": in.BankName, "senderAddress": in.SenderAddress, "trackingPolicy": "SPENDING_ONLY"}) != nil || tx.Commit(r.Context()) != nil {
			jsonError(w, 500, "unable to audit listener")
			return
		}
		jsonOut(w, 201, map[string]string{"id": lid})
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "listener id is required")
		return
	}
	var in struct {
		BankName      *string `json:"bankName"`
		SenderAddress *string `json:"senderAddress"`
		Active        *bool   `json:"active"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || (in.BankName == nil && in.SenderAddress == nil && in.Active == nil) {
		jsonError(w, 400, "listener change is required")
		return
	}
	if in.BankName != nil {
		v := strings.TrimSpace(*in.BankName)
		if v == "" || len(v) > 120 {
			jsonError(w, 400, "invalid bank name")
			return
		}
		in.BankName = &v
	}
	if in.SenderAddress != nil {
		v := strings.ToLower(strings.TrimSpace(*in.SenderAddress))
		if !validEmail(v) {
			jsonError(w, 400, "invalid sender address")
			return
		}
		in.SenderAddress = &v
	}
	tx, e := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if e != nil {
		jsonError(w, 500, "unable to update listener")
		return
	}
	defer tx.Rollback(r.Context())
	var found string
	e = tx.QueryRow(r.Context(), `UPDATE bank_email_listener SET bank_name=COALESCE($3,bank_name),sender_address=COALESCE($4,sender_address),active=COALESCE($5,active),updated_at=now() WHERE id=$1 AND household_id=$2 RETURNING id`, id, hid, in.BankName, in.SenderAddress, in.Active).Scan(&found)
	if e != nil {
		jsonError(w, 404, "listener not found")
		return
	}
	if auditCreate(r.Context(), tx, hid, p.UserID, "bank_email_listener", id, map[string]any{"bankName": in.BankName, "senderAddress": in.SenderAddress, "active": in.Active}) != nil || tx.Commit(r.Context()) != nil {
		jsonError(w, 500, "unable to audit listener")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func validEmail(v string) bool {
	a, e := mail.ParseAddress(v)
	return e == nil && a.Address == v && strings.Contains(v, "@")
}
