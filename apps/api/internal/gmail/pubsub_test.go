package gmail

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func configuredPubSubHandler(claims tokenClaims, verifyErr error) *Handler {
	return &Handler{
		mailbox:              "ruangkreatif.ekslusif@gmail.com",
		pubsubAudience:       "https://finance.example/webhooks/gmail/pubsub",
		pubsubServiceAccount: "gmail-push@example.iam.gserviceaccount.com",
		verifyToken: func(context.Context, string, string) (tokenClaims, error) {
			return claims, verifyErr
		},
	}
}

func TestPubSubRequiresBearerToken(t *testing.T) {
	handler := configuredPubSubHandler(tokenClaims{}, nil)
	request := httptest.NewRequest(http.MethodPost, "/webhooks/gmail/pubsub", strings.NewReader(`{}`))
	response := httptest.NewRecorder()

	handler.PubSub(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestPubSubRejectsUnexpectedServiceAccount(t *testing.T) {
	handler := configuredPubSubHandler(tokenClaims{Email: "other@example.iam.gserviceaccount.com", EmailVerified: true}, nil)
	request := httptest.NewRequest(http.MethodPost, "/webhooks/gmail/pubsub", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()

	handler.PubSub(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestPubSubRejectsUnexpectedMailbox(t *testing.T) {
	handler := configuredPubSubHandler(tokenClaims{Email: "gmail-push@example.iam.gserviceaccount.com", EmailVerified: true}, nil)
	data := base64.StdEncoding.EncodeToString([]byte(`{"emailAddress":"other@example.com","historyId":"123"}`))
	body := `{"message":{"data":"` + data + `","messageId":"message-1"}}`
	request := httptest.NewRequest(http.MethodPost, "/webhooks/gmail/pubsub", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()

	handler.PubSub(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
