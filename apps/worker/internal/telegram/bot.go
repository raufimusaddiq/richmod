package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Bot struct {
	token string
	http  *http.Client
}

type SendPayload struct {
	ChatID           int64  `json:"chat_id"`
	ReplyToMessageID int64  `json:"reply_to_message_id"`
	Text             string `json:"text"`
}

func NewBot(token string) *Bot {
	return &Bot{token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

func (b *Bot) Send(ctx context.Context, payload SendPayload) error {
	if b.token == "" {
		return fmt.Errorf("Telegram bot token is not configured")
	}
	body, err := json.Marshal(map[string]any{
		"chat_id": payload.ChatID,
		"text":    clean(payload.Text, 4000),
		"reply_parameters": map[string]any{
			"message_id":                  payload.ReplyToMessageID,
			"allow_sending_without_reply": true,
		},
	})
	if err != nil {
		return fmt.Errorf("encode Telegram response: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+b.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Telegram response: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := b.http.Do(request)
	if err != nil {
		// The request URL contains the bot token, so never wrap the transport error.
		return fmt.Errorf("send Telegram response failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram API returned HTTP %d", response.StatusCode)
	}
	return nil
}

func DecodeSendPayload(raw json.RawMessage) (SendPayload, error) {
	var payload SendPayload
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return SendPayload{}, fmt.Errorf("decode send payload: %w", err)
	}
	if payload.ChatID == 0 || payload.Text == "" {
		return SendPayload{}, fmt.Errorf("invalid send payload")
	}
	return payload, nil
}
