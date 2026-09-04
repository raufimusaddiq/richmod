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

func TestDecodeWrappedPubSubNotification(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte(`{"emailAddress":"ruangkreatif.ekslusif@gmail.com","historyId":"456"}`))
	messageID, notification, err := decodePushNotification([]byte(`{"message":{"data":"`+data+`","message_id":"legacy-1"}}`), http.Header{})
	if err != nil || messageID != "legacy-1" || notification.HistoryID != "456" {
		t.Fatalf("id=%q notification=%#v err=%v", messageID, notification, err)
	}
}

func TestDecodeUnwrappedPubSubNotificationWithMetadata(t *testing.T) {
	header := http.Header{}
	header.Set("X-Goog-Pubsub-Message-Id", "unwrapped-1")
	messageID, notification, err := decodePushNotification([]byte(`{"emailAddress":"ruangkreatif.ekslusif@gmail.com","historyId":"789"}`), header)
	if err != nil || messageID != "unwrapped-1" || notification.HistoryID != "789" {
		t.Fatalf("id=%q notification=%#v err=%v", messageID, notification, err)
	}
}

func TestDecodeUnwrappedPubSubNotificationWithoutMetadataIsIdempotent(t *testing.T) {
	raw := []byte(`{"emailAddress":"ruangkreatif.ekslusif@gmail.com","historyId":"999"}`)
	first, _, err := decodePushNotification(raw, http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := decodePushNotification(raw, http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second || !strings.HasPrefix(first, "payload-") {
		t.Fatalf("ids=%q %q", first, second)
	}
}

func TestDecodeBase64BodyPubSubNotification(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte(`{"emailAddress":"ruangkreatif.ekslusif@gmail.com","historyId":"1001"}`))
	messageID, notification, err := decodePushNotification([]byte(raw), http.Header{})
	if err != nil || notification.HistoryID != "1001" || !strings.HasPrefix(messageID, "payload-") {
		t.Fatalf("id=%q notification=%#v err=%v", messageID, notification, err)
	}
}

func TestDecodeGmailBase64URLWrappedNotification(t *testing.T) {
	data := base64.RawURLEncoding.EncodeToString([]byte(`{"emailAddress":"ruangkreatif.ekslusif@gmail.com","historyId":"1002"}`))
	messageID, notification, err := decodePushNotification([]byte(`{"message":{"data":"`+data+`","messageId":"url-1"}}`), http.Header{})
	if err != nil || messageID != "url-1" || notification.HistoryID != "1002" {
		t.Fatalf("id=%q notification=%#v err=%v", messageID, notification, err)
	}
}

func TestDecodeNestedCloudEventNotification(t *testing.T) {
	data := base64.RawURLEncoding.EncodeToString([]byte(`{"emailAddress":"ruangkreatif.ekslusif@gmail.com","historyId":"1003"}`))
	raw := []byte(`{"data":{"message":{"data":"` + data + `","messageId":"inner-1"}}}`)
	_, notification, err := decodePushNotification(raw, http.Header{})
	if err != nil || notification.HistoryID != "1003" {
		t.Fatalf("notification=%#v err=%v", notification, err)
	}
}

func TestJSONShapeReportsKeysWithoutValues(t *testing.T) {
	shape := jsonShape([]byte(`{"message":{"data":"c2VjcmV0","messageId":"private-id"},"subscription":"private-project"}`), 0)
	if !strings.Contains(shape, "keys=message,subscription") || strings.Contains(shape, "private") || strings.Contains(shape, "secret") {
		t.Fatalf("unsafe shape=%q", shape)
	}
}

func TestDecodeNumericGmailHistoryID(t *testing.T) {
	data := base64.RawURLEncoding.EncodeToString([]byte(`{"emailAddress":"ruangkreatif.ekslusif@gmail.com","historyId":1004}`))
	_, notification, err := decodePushNotification([]byte(`{"message":{"data":"`+data+`","messageId":"numeric-1"}}`), http.Header{})
	if err != nil || notification.HistoryID != "1004" {
		t.Fatalf("notification=%#v err=%v", notification, err)
	}
}

func TestRejectsNonIntegerGmailHistoryID(t *testing.T) {
	data := base64.RawURLEncoding.EncodeToString([]byte(`{"emailAddress":"ruangkreatif.ekslusif@gmail.com","historyId":1e3}`))
	if _, _, err := decodePushNotification([]byte(`{"message":{"data":"`+data+`","messageId":"invalid-1"}}`), http.Header{}); err == nil {
		t.Fatal("accepted non-integer history ID")
	}
}
