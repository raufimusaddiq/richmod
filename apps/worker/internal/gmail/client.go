package gmail

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const gmailAPI = "https://gmail.googleapis.com/gmail/v1/users/me"

type Config struct {
	OAuthClientPath string
	TokenKeyHex     string
	Mailbox         string
	PubSubTopic     string
}

type oauthClient struct {
	ClientID     string
	ClientSecret string
}

type client struct {
	oauth      oauthClient
	key        []byte
	mailbox    string
	topic      string
	httpClient *http.Client
}

func newClient(config Config) (*client, error) {
	if config.OAuthClientPath == "" || config.TokenKeyHex == "" || config.Mailbox == "" || config.PubSubTopic == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(config.OAuthClientPath)
	if err != nil {
		return nil, fmt.Errorf("read Google OAuth client: %w", err)
	}
	var document struct {
		Web struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"web"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode Google OAuth client: %w", err)
	}
	if document.Web.ClientID == "" || document.Web.ClientSecret == "" {
		return nil, fmt.Errorf("Google OAuth client credentials are incomplete")
	}
	key, err := hex.DecodeString(config.TokenKeyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("GMAIL_TOKEN_ENCRYPTION_KEY must be 32-byte hex")
	}
	return &client{
		oauth:      oauthClient{ClientID: document.Web.ClientID, ClientSecret: document.Web.ClientSecret},
		key:        key,
		mailbox:    strings.ToLower(strings.TrimSpace(config.Mailbox)),
		topic:      strings.TrimSpace(config.PubSubTopic),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *client) decrypt(householdID string, ciphertext []byte) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted Gmail token")
	}
	plain, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], []byte(householdID))
	if err != nil {
		return "", fmt.Errorf("decrypt Gmail token: %w", err)
	}
	return string(plain), nil
}

func (c *client) accessToken(ctx context.Context, refreshToken string) (string, error) {
	form := url.Values{
		"client_id":     {c.oauth.ClientID},
		"client_secret": {c.oauth.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", errors.New("Google token refresh failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Google token refresh returned HTTP %d", response.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil || body.AccessToken == "" {
		return "", fmt.Errorf("invalid Google token response")
	}
	return body.AccessToken, nil
}

func (c *client) doJSON(ctx context.Context, method, endpoint, accessToken string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(encoded))
	}
	request, _ := http.NewRequestWithContext(ctx, method, endpoint, body)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return errors.New("Gmail API request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Gmail API returned HTTP %d", response.StatusCode)
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode Gmail API response: %w", err)
	}
	return nil
}

type watchResponse struct {
	HistoryID  string `json:"historyId"`
	Expiration string `json:"expiration"`
}

func (c *client) watch(ctx context.Context, accessToken string) (watchResponse, error) {
	var output watchResponse
	err := c.doJSON(ctx, http.MethodPost, gmailAPI+"/watch", accessToken, map[string]any{
		"topicName":           c.topic,
		"labelIds":            []string{"INBOX"},
		"labelFilterBehavior": "INCLUDE",
	}, &output)
	if err != nil {
		return watchResponse{}, err
	}
	if output.HistoryID == "" {
		return watchResponse{}, fmt.Errorf("Gmail watch response omitted history ID")
	}
	if _, err := strconv.ParseInt(output.Expiration, 10, 64); err != nil {
		return watchResponse{}, fmt.Errorf("Gmail watch response has invalid expiration")
	}
	return output, nil
}

type historyResponse struct {
	History []struct {
		MessagesAdded []struct {
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
		} `json:"messagesAdded"`
	} `json:"history"`
	HistoryID     string `json:"historyId"`
	NextPageToken string `json:"nextPageToken"`
}

func (c *client) history(ctx context.Context, accessToken, startHistoryID, pageToken string) (historyResponse, error) {
	query := url.Values{"startHistoryId": {startHistoryID}, "historyTypes": {"messageAdded"}}
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}
	var output historyResponse
	err := c.doJSON(ctx, http.MethodGet, gmailAPI+"/history?"+query.Encode(), accessToken, nil, &output)
	return output, err
}

type gmailMessage struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	Payload  struct {
		MimeType string `json:"mimeType"`
		Headers  []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		Body struct {
			Data string `json:"data"`
		} `json:"body"`
		Parts []messagePart `json:"parts"`
	} `json:"payload"`
}

type messagePart struct {
	MimeType string        `json:"mimeType"`
	Body     messageBody   `json:"body"`
	Parts    []messagePart `json:"parts"`
}

type messageBody struct {
	Data string `json:"data"`
}

func (c *client) messageMetadata(ctx context.Context, accessToken, messageID string) (gmailMessage, error) {
	var output gmailMessage
	query := url.Values{"format": {"metadata"}, "metadataHeaders": {"From", "Subject", "Authentication-Results", "Date"}}
	endpoint := gmailAPI + "/messages/" + url.PathEscape(messageID) + "?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, endpoint, accessToken, nil, &output); err != nil {
		return gmailMessage{}, err
	}
	return output, nil
}

func (c *client) messageFull(ctx context.Context, accessToken, messageID string) (gmailMessage, []byte, error) {
	var output gmailMessage
	endpoint := gmailAPI + "/messages/" + url.PathEscape(messageID) + "?format=full"
	if err := c.doJSON(ctx, http.MethodGet, endpoint, accessToken, nil, &output); err != nil {
		return gmailMessage{}, nil, err
	}
	raw, err := json.Marshal(output)
	return output, raw, err
}
