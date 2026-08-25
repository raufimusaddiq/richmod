package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeStore struct {
	input CaptureInput
	err   error
	calls int
}

func (s *fakeStore) Capture(_ context.Context, input CaptureInput) (bool, error) {
	s.calls++
	s.input = input
	return true, s.err
}

func TestWebhookAuthenticatesAndCapturesPrivateText(t *testing.T) {
	store := &fakeStore{}
	handler := NewHandler(store, "webhook-secret")
	body := `{"update_id":42,"message":{"message_id":7,"from":{"id":123,"is_bot":false},"chat":{"id":123,"type":"private"},"date":1,"text":"makan siang 50rb"}}`
	request := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(body))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "webhook-secret")
	response := httptest.NewRecorder()

	handler.Webhook(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if store.calls != 1 || store.input.UpdateID != 42 || store.input.TelegramUserID != 123 {
		t.Fatalf("capture = %#v, calls = %d", store.input, store.calls)
	}
	if string(store.input.RawPayload) != body {
		t.Fatal("raw evidence payload was not preserved")
	}
}

func TestWebhookRejectsWrongSecret(t *testing.T) {
	store := &fakeStore{}
	handler := NewHandler(store, "webhook-secret")
	request := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(`{"update_id":42}`))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong")
	response := httptest.NewRecorder()

	handler.Webhook(response, request)

	if response.Code != http.StatusUnauthorized || store.calls != 0 {
		t.Fatalf("status = %d, calls = %d", response.Code, store.calls)
	}
}

func TestWebhookDoesNotDiscloseUnauthorizedIdentity(t *testing.T) {
	store := &fakeStore{err: ErrUnauthorized}
	handler := NewHandler(store, "webhook-secret")
	request := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(`{"update_id":42,"message":{"from":{"id":999},"chat":{"type":"private"},"text":"saldo?"}}`))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "webhook-secret")
	response := httptest.NewRecorder()

	handler.Webhook(response, request)

	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
}

func TestWebhookReturnsServerErrorWithoutDetails(t *testing.T) {
	store := &fakeStore{err: errors.New("database unavailable")}
	handler := NewHandler(store, "webhook-secret")
	request := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(`{"update_id":42,"message":{"from":{"id":123},"chat":{"type":"private"},"text":"help"}}`))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "webhook-secret")
	response := httptest.NewRecorder()

	handler.Webhook(response, request)

	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "database") {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
}
