package settings

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func (h *Handler) TestFinancialEmailSource(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold || !owner(p) {
		jsonError(w, http.StatusForbidden, "owner role required")
		return
	}
	var in struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Body) == "" || len(in.Body) > 200000 {
		jsonError(w, 400, "sample email body is required")
		return
	}
	var previewID string
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		jsonError(w, 500, "unable to create preview")
		return
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(r.Context(), `INSERT INTO financial_email_preview(household_id,financial_source_id,subject,body,created_by_user_id) SELECT $1,id,$3,$4,$5 FROM financial_email_source WHERE id=$2 AND household_id=$1 RETURNING id`, p.HouseholdID, r.PathValue("id"), in.Subject, in.Body, p.UserID).Scan(&previewID)
	if err != nil {
		jsonError(w, 404, "financial source not found")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO job(type,payload_json,max_attempts) VALUES('PROCESS_FINANCIAL_EMAIL_PREVIEW',jsonb_build_object('preview_id',$1::uuid),3)`, previewID); err != nil || tx.Commit(r.Context()) != nil {
		jsonError(w, 500, "unable to enqueue preview")
		return
	}
	jsonOut(w, http.StatusAccepted, map[string]string{"id": previewID, "status": "PENDING"})
}

func (h *Handler) FinancialEmailPreview(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		jsonError(w, http.StatusForbidden, "household membership required")
		return
	}
	var status string
	var result json.RawMessage
	var message *string
	err := h.pool.QueryRow(r.Context(), `SELECT status,COALESCE(result_json,'null'::jsonb),error_message FROM financial_email_preview WHERE id=$1 AND household_id=$2`, r.PathValue("id"), p.HouseholdID).Scan(&status, &result, &message)
	if err != nil {
		jsonError(w, 404, "preview not found")
		return
	}
	jsonOut(w, 200, map[string]any{"id": r.PathValue("id"), "status": status, "result": result, "error": message})
}

func (h *Handler) FinancialEmailSources(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		jsonError(w, 403, "household membership required")
		return
	}
	if r.Method == http.MethodGet {
		rows, err := h.pool.Query(r.Context(), `SELECT id,provider_name,sender_address,capabilities,COALESCE(default_wealth_account_id::text,''),status,last_received_at FROM financial_email_source WHERE household_id=$1 ORDER BY provider_name,sender_address`, p.HouseholdID)
		if err != nil {
			jsonError(w, 500, "unable to list financial email sources")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, provider, sender, def, status string
			var caps []string
			var last any
			if err := rows.Scan(&id, &provider, &sender, &caps, &def, &status, &last); err != nil {
				jsonError(w, 500, "unable to list financial email sources")
				return
			}
			out = append(out, map[string]any{"id": id, "providerName": provider, "senderAddress": sender, "capabilities": caps, "defaultWealthAccountId": nilIfEmpty(def), "status": status, "lastReceivedAt": last})
		}
		jsonOut(w, 200, out)
		return
	}
	if !owner(p) {
		jsonError(w, 403, "owner role required")
		return
	}
	id := r.PathValue("id")
	if r.Method == http.MethodPost {
		var in struct {
			ProviderName, SenderAddress string
			Capabilities                []string `json:"capabilities"`
			DefaultWealthAccountID      *string  `json:"defaultWealthAccountId"`
			Status                      string   `json:"status"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			jsonError(w, 400, "invalid financial source request")
			return
		}
		in.ProviderName = strings.TrimSpace(in.ProviderName)
		in.SenderAddress = strings.ToLower(strings.TrimSpace(in.SenderAddress))
		if in.Capabilities == nil {
			in.Capabilities = []string{"CASH_MOVEMENT", "WEALTH_VALUE"}
		}
		if in.Status == "" {
			in.Status = "DRAFT"
		}
		if in.ProviderName == "" || len(in.ProviderName) > 120 || !validEmail(in.SenderAddress) || (in.Status != "DRAFT" && in.Status != "DISABLED") || !validCapabilities(in.Capabilities) {
			jsonError(w, 400, "invalid financial source fields")
			return
		}
		tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			jsonError(w, 500, "unable to create financial source")
			return
		}
		defer tx.Rollback(r.Context())
		if in.DefaultWealthAccountID != nil {
			var valid string
			if err = tx.QueryRow(r.Context(), `SELECT id FROM wealth_account WHERE id=$1 AND household_id=$2 AND active`, *in.DefaultWealthAccountID, p.HouseholdID).Scan(&valid); err != nil {
				jsonError(w, 400, "invalid default Wealth Account")
				return
			}
		}
		var sourceID string
		err = tx.QueryRow(r.Context(), `INSERT INTO financial_email_source(household_id,provider_name,sender_address,capabilities,default_wealth_account_id,status,created_by_user_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, p.HouseholdID, in.ProviderName, in.SenderAddress, in.Capabilities, in.DefaultWealthAccountID, in.Status, p.UserID).Scan(&sourceID)
		if err != nil {
			jsonError(w, 409, "active sender already exists")
			return
		}
		if in.Status == "ACTIVE" {
			if _, err = tx.Exec(r.Context(), `INSERT INTO email_sender_route(household_id,sender_address,route_kind,financial_source_id) VALUES($1,$2,'FINANCIAL',$3)`, p.HouseholdID, in.SenderAddress, sourceID); err != nil {
				jsonError(w, 409, "active sender already exists")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			jsonError(w, 500, "unable to create financial source")
			return
		}
		jsonOut(w, 201, map[string]string{"id": sourceID})
		return
	}
	if id == "" {
		jsonError(w, 400, "source id is required")
		return
	}
	var in struct {
		ProviderName           *string   `json:"providerName"`
		SenderAddress          *string   `json:"senderAddress"`
		Capabilities           *[]string `json:"capabilities"`
		DefaultWealthAccountID **string  `json:"defaultWealthAccountId"`
		Status                 *string   `json:"status"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		jsonError(w, 400, "invalid financial source change")
		return
	}
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		jsonError(w, 500, "unable to update financial source")
		return
	}
	defer tx.Rollback(r.Context())
	var oldSender, oldStatus string
	var oldDefault *string
	if err = tx.QueryRow(r.Context(), `SELECT sender_address,status,default_wealth_account_id::text FROM financial_email_source WHERE id=$1 AND household_id=$2 FOR UPDATE`, id, p.HouseholdID).Scan(&oldSender, &oldStatus, &oldDefault); err != nil {
		jsonError(w, 404, "financial source not found")
		return
	}
	sender := oldSender
	status := oldStatus
	if in.SenderAddress != nil {
		sender = strings.ToLower(strings.TrimSpace(*in.SenderAddress))
		if !validEmail(sender) {
			jsonError(w, 400, "invalid sender address")
			return
		}
	}
	if in.Status != nil {
		status = strings.TrimSpace(*in.Status)
		if status != "DRAFT" && status != "ACTIVE" && status != "DISABLED" {
			jsonError(w, 400, "invalid source status")
			return
		}
	}
	if status == "ACTIVE" && oldStatus != "ACTIVE" {
		var tested bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM financial_email_preview WHERE financial_source_id=$1 AND household_id=$2 AND status='SUCCEEDED')`, id, p.HouseholdID).Scan(&tested); err != nil || !tested {
			jsonError(w, 400, "test configuration before activation")
			return
		}
	}
	capsSQL := in.Capabilities
	if capsSQL != nil && !validCapabilities(*capsSQL) {
		jsonError(w, 400, "invalid capabilities")
		return
	}
	var def any = oldDefault
	if in.DefaultWealthAccountID != nil {
		if *in.DefaultWealthAccountID == nil || **in.DefaultWealthAccountID == "" {
			def = nil
		} else {
			var valid string
			if err = tx.QueryRow(r.Context(), `SELECT id FROM wealth_account WHERE id=$1 AND household_id=$2 AND active`, **in.DefaultWealthAccountID, p.HouseholdID).Scan(&valid); err != nil {
				jsonError(w, 400, "invalid default Wealth Account")
				return
			}
			def = **in.DefaultWealthAccountID
		}
	}
	_, err = tx.Exec(r.Context(), `UPDATE financial_email_source SET provider_name=COALESCE($2,provider_name),sender_address=$3,capabilities=COALESCE($4,capabilities),default_wealth_account_id=$5,status=$6,updated_at=now() WHERE id=$1 AND household_id=$7`, id, in.ProviderName, sender, capsSQL, def, status, p.HouseholdID)
	if err != nil {
		jsonError(w, 409, "unable to update financial source")
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE email_sender_route SET active=false,updated_at=now() WHERE financial_source_id=$1`, id)
	if status == "ACTIVE" {
		if _, err = tx.Exec(r.Context(), `INSERT INTO email_sender_route(household_id,sender_address,route_kind,financial_source_id,active) VALUES($1,$2,'FINANCIAL',$3,true)`, p.HouseholdID, sender, id); err != nil {
			jsonError(w, 409, "active sender already exists")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		jsonError(w, 500, "unable to update financial source")
		return
	}
	jsonOut(w, 204, nil)
}
func validCapabilities(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		if v != "CASH_MOVEMENT" && v != "WEALTH_VALUE" || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func nilIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
