package emailingress

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestProvisionRequiresOwner(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/email-ingress", nil)
	request = request.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{UserID: "user", Household: &auth.Membership{HouseholdID: "household", Role: "MEMBER"}}))
	response := httptest.NewRecorder()
	handler.Integration(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestDeliveriesRequiresOwner(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/email-ingress/deliveries", nil)
	request = request.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{UserID: "user", Household: &auth.Membership{HouseholdID: "household", Role: "MEMBER"}}))
	response := httptest.NewRecorder()
	handler.Deliveries(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestDeliveriesRequiresHousehold(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/email-ingress/deliveries", nil)
	request = request.WithContext(auth.ContextWithPrincipal(context.Background(), auth.Principal{UserID: "user"}))
	response := httptest.NewRecorder()
	handler.Deliveries(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestInboundRejectsBadSignatureBeforeReadingBody(t *testing.T) {
	handler := NewHandler(&Service{domain: "richmod.link", now: time.Now}, "secret")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/inbound/email", nil)
	request.Header.Set("Content-Type", "message/rfc822")
	request.Header.Set("X-Richmod-Recipient", "h_0123456789abcdef0123456789abcdef@richmod.link")
	request.Header.Set("X-Richmod-Envelope-From", "forwarder@example.test")
	request.Header.Set("X-Richmod-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	request.Header.Set("X-Richmod-Content-SHA256", strings.Repeat("0", 64))
	request.Header.Set("X-Richmod-Signature", strings.Repeat("1", 64))
	request.Header.Set("X-Richmod-Object-Key", "raw/key.eml")
	request.Body = readGuard{t}
	response := httptest.NewRecorder()
	handler.Inbound(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

type readGuard struct{ t *testing.T }

func (g readGuard) Read([]byte) (int, error) {
	g.t.Error("body read before signature verification")
	return 0, io.EOF
}

func (readGuard) Close() error { return nil }
