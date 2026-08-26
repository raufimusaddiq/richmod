package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Bot struct {
	token string
	http  *http.Client
	base  string
}

type SendPayload struct {
	ChatID           int64                 `json:"chat_id"`
	ReplyToMessageID int64                 `json:"reply_to_message_id"`
	Text             string                `json:"text"`
	ReviewRequestID  string                `json:"review_request_id,omitempty"`
	ReplyMarkup      *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	CallbackQueryID  string                `json:"callback_query_id,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

func NewBot(token string) *Bot {
	return &Bot{token: token, http: &http.Client{Timeout: 15 * time.Second}, base: "https://api.telegram.org"}
}

func (b *Bot) Send(ctx context.Context, payload SendPayload) (int64, error) {
	if b.token == "" {
		return 0, fmt.Errorf("Telegram bot token is not configured")
	}
	requestBody := map[string]any{
		"chat_id": payload.ChatID,
		"text":    clean(payload.Text, 4000),
	}
	if payload.ReplyMarkup != nil {
		requestBody["reply_markup"] = payload.ReplyMarkup
	}
	if payload.ReplyToMessageID != 0 {
		requestBody["reply_parameters"] = map[string]any{
			"message_id":                  payload.ReplyToMessageID,
			"allow_sending_without_reply": true,
		}
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return 0, fmt.Errorf("encode Telegram response: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/bot"+b.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create Telegram response: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := b.http.Do(request)
	if err != nil {
		// The request URL contains the bot token, so never wrap the transport error.
		return 0, fmt.Errorf("send Telegram response failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("Telegram API returned HTTP %d", response.StatusCode)
	}
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil || !result.OK || result.Result.MessageID == 0 {
		return 0, fmt.Errorf("Telegram API returned an invalid response")
	}
	return result.Result.MessageID, nil
}

func (b *Bot) AnswerCallback(ctx context.Context, callbackQueryID string) error {
	if b.token == "" || callbackQueryID == "" {
		return fmt.Errorf("Telegram callback is not configured")
	}
	body, _ := json.Marshal(map[string]string{"callback_query_id": callbackQueryID})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/bot"+b.token+"/answerCallbackQuery", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Telegram callback response")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := b.http.Do(request)
	if err != nil {
		return fmt.Errorf("answer Telegram callback failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram callback API returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (b *Bot) Download(ctx context.Context, fileID string, maxBytes int64) ([]byte, string, error) {
	if b.token == "" || fileID == "" {
		return nil, "", fmt.Errorf("Telegram image download is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+"/bot"+b.token+"/getFile?file_id="+url.QueryEscape(fileID), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create Telegram file request")
	}
	response, err := b.http.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("Telegram file metadata request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("Telegram file metadata returned HTTP %d", response.StatusCode)
	}
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
			FileSize int64  `json:"file_size"`
		} `json:"result"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || !result.OK || result.Result.FilePath == "" || result.Result.FileSize > maxBytes {
		return nil, "", fmt.Errorf("Telegram file metadata is invalid")
	}
	download, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+"/file/bot"+b.token+"/"+strings.TrimLeft(result.Result.FilePath, "/"), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create Telegram download request")
	}
	fileResponse, err := b.http.Do(download)
	if err != nil {
		return nil, "", fmt.Errorf("Telegram file download failed")
	}
	defer fileResponse.Body.Close()
	if fileResponse.StatusCode < 200 || fileResponse.StatusCode >= 300 {
		return nil, "", fmt.Errorf("Telegram file download returned HTTP %d", fileResponse.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(fileResponse.Body, maxBytes+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > maxBytes {
		return nil, "", fmt.Errorf("Telegram image size is invalid")
	}
	return raw, result.Result.FilePath, nil
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
