package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	ChatID           int64  `json:"chat_id"`
	ReplyToMessageID int64  `json:"reply_to_message_id"`
	Text             string `json:"text"`
	ReviewRequestID  string `json:"review_request_id,omitempty"`
	// BindDocumentID, when set, records the sent message as about this document so
	// a reply to it binds to the evidence.
	BindDocumentID string `json:"bind_document_id,omitempty"`
	// BindMerchantLearningRequestID, when set, records the sent message as the
	// merchant-learning question of this review_request, so its buttons and
	// replies bind to the pending decision after the review card is retired.
	BindMerchantLearningRequestID string                `json:"bind_merchant_learning_request_id,omitempty"`
	ReplyMarkup                   *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	CallbackQueryID               string                `json:"callback_query_id,omitempty"`
}

type EditPayload struct {
	ChatID      int64                 `json:"chat_id"`
	MessageID   int64                 `json:"message_id"`
	Text        string                `json:"text"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// ReviewRequestID, when set, marks an edit of a live review card. The worker
	// skips it once the request is terminal, so a queued edit cannot restore
	// buttons on a card that retirement already stripped.
	ReviewRequestID string `json:"review_request_id,omitempty"`
}

// APIError is a Telegram Bot API rejection: the HTTP status and Telegram's own
// description. It never carries the request URL, which contains the bot token.
type APIError struct {
	Method      string
	StatusCode  int
	Description string
}

func (e *APIError) Error() string {
	if e.Description == "" {
		return fmt.Sprintf("Telegram %s API returned HTTP %d", e.Method, e.StatusCode)
	}
	return fmt.Sprintf("Telegram %s API returned HTTP %d: %s", e.Method, e.StatusCode, e.Description)
}

// apiError reads Telegram's error description from a non-2xx response. The body
// is bounded and only the description string is kept.
func apiError(method string, response *http.Response) *APIError {
	var body struct {
		Description string `json:"description"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&body)
	return &APIError{Method: method, StatusCode: response.StatusCode, Description: clean(body.Description, 200)}
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

func (b *Bot) Typing(ctx context.Context, chatID int64) error {
	if b.token == "" || chatID == 0 {
		return fmt.Errorf("Telegram typing is not configured")
	}
	body, err := json.Marshal(map[string]any{"chat_id": chatID, "action": "typing"})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/bot"+b.token+"/sendChatAction", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("send Telegram typing failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram typing API returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || !result.OK {
		return fmt.Errorf("Telegram typing API returned an invalid response")
	}
	return nil
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

func (b *Bot) Edit(ctx context.Context, payload EditPayload) error {
	if b.token == "" || payload.ChatID == 0 || payload.MessageID == 0 || payload.Text == "" {
		return fmt.Errorf("invalid Telegram edit payload")
	}
	markup := payload.ReplyMarkup
	if markup == nil {
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	}
	return b.edit(ctx, "editMessageText", map[string]any{"chat_id": payload.ChatID, "message_id": payload.MessageID, "text": clean(payload.Text, 4000), "reply_markup": markup})
}

// EditReplyMarkup removes every inline button from a message without touching
// its text, so a card whose text is unknown can still be retired.
func (b *Bot) EditReplyMarkup(ctx context.Context, chatID, messageID int64) error {
	if b.token == "" || chatID == 0 || messageID == 0 {
		return fmt.Errorf("invalid Telegram reply markup edit")
	}
	return b.edit(ctx, "editMessageReplyMarkup", map[string]any{"chat_id": chatID, "message_id": messageID, "reply_markup": &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}})
}

func (b *Bot) edit(ctx context.Context, method string, requestBody map[string]any) error {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("encode Telegram edit: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/bot"+b.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Telegram edit request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		// The request URL contains the bot token, so never wrap the transport error.
		return fmt.Errorf("edit Telegram message failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(method, resp)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || !result.OK {
		return fmt.Errorf("Telegram %s API returned an invalid response", method)
	}
	return nil
}

// editFinished classifies a failed card edit. A card that is already in the
// wanted state, is gone, can no longer be edited, or sits in a chat the bot
// cannot reach is finished: retrying cannot change it. Rate limits, server
// errors, and transport failures are retried by the queue.
func editFinished(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.StatusCode == http.StatusForbidden {
		return true // bot blocked, kicked, or the user is deactivated
	}
	description := strings.ToLower(apiErr.Description)
	for _, finished := range []string{"message is not modified", "message to edit not found", "message can't be edited", "message_id_invalid", "chat not found", "bot was blocked", "user is deactivated"} {
		if strings.Contains(description, finished) {
			return true
		}
	}
	return false
}

// editRejected reports a Telegram rejection of the request itself (a 4xx other
// than a rate limit) that the same request will never pass.
func editRejected(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != http.StatusTooManyRequests
}

func DecodeEditPayload(raw json.RawMessage) (EditPayload, error) {
	var payload EditPayload
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return EditPayload{}, fmt.Errorf("decode edit payload: %w", err)
	}
	if payload.ChatID == 0 || payload.MessageID == 0 || payload.Text == "" {
		return EditPayload{}, fmt.Errorf("invalid edit payload")
	}
	return payload, nil
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
