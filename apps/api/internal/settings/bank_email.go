package settings

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func (h *Handler) BankEmailListeners(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		jsonError(w, 403, "household membership required")
		return
	}
	hid := p.HouseholdID
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
		// Sender identity is not account identity. Reuse a single, deterministic
		// logical account for the same bank, otherwise create an ID-derived key.
		e = tx.QueryRow(r.Context(), `SELECT id FROM account WHERE household_id=$1 AND account_type='BANK' AND tracking_policy='SPENDING_ONLY' AND active AND system_managed AND lower(name)=lower('Bank · '||$2) ORDER BY created_at,id LIMIT 1 FOR UPDATE`, hid, in.BankName).Scan(&aid)
		if e == pgx.ErrNoRows {
			e = tx.QueryRow(r.Context(), `INSERT INTO account(household_id,name,account_type,tracking_policy,system_managed) VALUES($1,$2,'BANK','SPENDING_ONLY',true) RETURNING id`, hid, "Bank · "+in.BankName).Scan(&aid)
			if e == nil {
				_, e = tx.Exec(r.Context(), `UPDATE account SET system_key='bank-email-account:'||id::text,updated_at=now() WHERE id=$1`, aid)
			}
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
	var currentAccountID, currentBankName, currentSender string
	var currentActive bool
	e = tx.QueryRow(r.Context(), `SELECT COALESCE(account_id::text,''),bank_name,sender_address,active FROM bank_email_listener WHERE id=$1 AND household_id=$2 FOR UPDATE`, id, hid).Scan(&currentAccountID, &currentBankName, &currentSender, &currentActive)
	if e != nil {
		jsonError(w, 404, "listener not found")
		return
	}
	bankName := currentBankName
	if in.BankName != nil {
		bankName = *in.BankName
	}
	senderAddress := currentSender
	if in.SenderAddress != nil {
		senderAddress = *in.SenderAddress
	}
	active := currentActive
	if in.Active != nil {
		active = *in.Active
	}
	if active {
		var duplicate bool
		if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM bank_email_listener WHERE household_id=$1 AND sender_address=$2 AND active AND id<>$3)`, hid, senderAddress, id).Scan(&duplicate); e != nil {
			jsonError(w, 500, "unable to check listener sender")
			return
		}
		if duplicate {
			jsonError(w, 409, "active sender already exists")
			return
		}
	}
	if currentAccountID != "" {
		if _, e = tx.Exec(r.Context(), `UPDATE account SET name=CASE WHEN system_managed THEN $2 ELSE name END,updated_at=now() WHERE id=$1`, currentAccountID, "Bank · "+bankName); e != nil {
			jsonError(w, 409, "listener account is unavailable")
			return
		}
	}
	var found string
	e = tx.QueryRow(r.Context(), `UPDATE bank_email_listener SET bank_name=$3,sender_address=$4,active=$5,updated_at=now() WHERE id=$1 AND household_id=$2 RETURNING id`, id, hid, bankName, senderAddress, active).Scan(&found)
	if e != nil {
		jsonError(w, 409, "active sender already exists")
		return
	}
	if auditCreate(r.Context(), tx, hid, p.UserID, "bank_email_listener", id, map[string]any{"bankName": bankName, "senderAddress": senderAddress, "active": active, "trackingPolicy": "SPENDING_ONLY"}) != nil || tx.Commit(r.Context()) != nil {
		jsonError(w, 500, "unable to audit listener")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func validEmail(v string) bool {
	a, e := mail.ParseAddress(v)
	return e == nil && a.Address == v && strings.Contains(v, "@")
}
