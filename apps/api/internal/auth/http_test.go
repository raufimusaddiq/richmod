package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionCookieUsesSlidingDayDuration(t *testing.T) {
	handler := NewHandler(nil, true)
	response := httptest.NewRecorder()
	handler.setSessionCookie(response, "token", time.Now().Add(SessionIdleTimeout))
	cookie := response.Result().Cookies()[0]
	if cookie.MaxAge != 24*60*60 {
		t.Fatalf("cookie MaxAge = %d, want 86400", cookie.MaxAge)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie security attributes are incorrect: %#v", cookie)
	}
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"owner@example.com","extra":true}`))
	request.Header.Set("Content-Type", "application/json")
	var input struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(request, &input); err == nil {
		t.Fatal("decodeJSON accepted an unknown field")
	}
}
