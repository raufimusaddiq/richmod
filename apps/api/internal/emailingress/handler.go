package emailingress

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

const maxRawMIMEBytes = 25 << 20

type Handler struct {
	service *Service
	secret  string
}

func NewHandler(service *Service, secret string) *Handler {
	return &Handler{service: service, secret: secret}
}

func (h *Handler) Inbound(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "message/rfc822") {
		http.Error(w, `{"error":"content type must be message/rfc822"}`, http.StatusBadRequest)
		return
	}
	signed, err := parseSignedHeaders(r.Header.Get("X-Richmod-Recipient"), r.Header.Get("X-Richmod-Envelope-From"), r.Header.Get("X-Richmod-Timestamp"), r.Header.Get("X-Richmod-Content-SHA256"), r.Header.Get("X-Richmod-Signature"), r.Header.Get("X-Richmod-Message-ID"), r.Header.Get("X-Richmod-Object-Key"), h.service.domain)
	if err != nil {
		http.Error(w, `{"error":"invalid ingress authentication"}`, http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRawMIMEBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"invalid email body"}`, http.StatusBadRequest)
		return
	}
	if err = signed.verify(raw, h.secret, h.service.now()); err != nil {
		status := http.StatusUnauthorized
		if strings.Contains(err.Error(), "content sha") {
			status = http.StatusBadRequest
		}
		if strings.Contains(err.Error(), "not configured") {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, `{"error":"ingress authentication rejected"}`, status)
		return
	}
	parsed, err := parseMIME(raw)
	if err != nil {
		http.Error(w, `{"error":"malformed email"}`, http.StatusBadRequest)
		return
	}
	if err = h.service.Deliver(r.Context(), deliveryInput{Signed: signed, Email: parsed, Raw: raw}); err != nil {
		http.Error(w, `{"error":"email delivery unavailable"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) Integration(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "household membership required"})
		return
	}
	householdID := p.Memberships[0].HouseholdID
	if r.Method == http.MethodGet {
		address, found, err := h.service.Current(r.Context(), householdID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to load email ingress"})
			return
		}
		if !found {
			writeJSON(w, 200, map[string]any{"address": nil, "status": nil, "provider": "CLOUDFLARE_EMAIL", "lastReceivedAt": nil})
			return
		}
		writeJSON(w, 200, address)
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		writeJSON(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	address, err := h.service.Provision(r.Context(), householdID, p.UserID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to provision email ingress"})
		return
	}
	writeJSON(w, http.StatusCreated, address)
}

func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) { h.change(w, r, "activate") }
func (h *Handler) Rotate(w http.ResponseWriter, r *http.Request)   { h.change(w, r, "rotate") }

func (h *Handler) change(w http.ResponseWriter, r *http.Request, operation string) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return
	}
	if p.Memberships[0].Role != "OWNER" {
		writeJSON(w, 403, map[string]string{"error": "owner role required"})
		return
	}
	var err error
	if operation == "activate" {
		err = h.service.Activate(r.Context(), p.Memberships[0].HouseholdID, p.UserID)
	} else {
		var address Address
		address, err = h.service.Rotate(r.Context(), p.Memberships[0].HouseholdID, p.UserID)
		if err == nil {
			writeJSON(w, 201, address)
			return
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 409, map[string]string{"error": "provisioned email ingress address required"})
		return
	}
	if errors.Is(err, ErrActivationNotReady) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "verify one forwarded email and configure trusted authserv IDs before activation"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to " + operation + " email ingress"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
