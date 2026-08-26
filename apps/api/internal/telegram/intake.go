package telegram

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxWebhookBytes = 1 << 20

var ErrUnauthorized = errors.New("telegram user is not authorized")

type CaptureInput struct {
	UpdateID       int64
	TelegramUserID int64
	RawPayload     []byte
}

type ImageInput struct {
	CaptureInput
	FileID       string
	FileName     string
	MIMEType     string
	Caption      string
	MediaGroupID string
	MessageID    int64
}

type Store interface {
	Capture(context.Context, CaptureInput) (bool, error)
	CaptureImage(context.Context, ImageInput) (bool, error)
	Link(context.Context, CaptureInput, string) (bool, error)
}

type callbackStore interface {
	CaptureCallback(context.Context, CaptureInput, string, int64, int64) (bool, error)
}

type Handler struct {
	store    Store
	secret   string
	botToken string
}

func NewHandler(store Store, secret string, botToken ...string) *Handler {
	token := ""
	if len(botToken) > 0 {
		token = botToken[0]
	}
	return &Handler{store: store, secret: secret, botToken: token}
}

func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	webhookStarted := time.Now()
	callback := false
	defer func() {
		// Structured logs provide callback latency observability without exposing
		// message contents or Telegram credentials.
		if callback {
			slog.Default().Info("telegram callback webhook", "latency_ms", time.Since(webhookStarted).Milliseconds())
		}
	}()
	if h.secret == "" {
		http.Error(w, "telegram integration unavailable", http.StatusServiceUnavailable)
		return
	}
	if !sameSecret(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"), h.secret) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid webhook payload", http.StatusBadRequest)
		return
	}
	var update struct {
		UpdateID int64 `json:"update_id"`
		Message  *struct {
			MessageID    int64  `json:"message_id"`
			Text         string `json:"text"`
			Caption      string `json:"caption"`
			MediaGroupID string `json:"media_group_id"`
			Photo        []struct {
				FileID string `json:"file_id"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"photo"`
			Document *struct {
				FileID   string `json:"file_id"`
				FileName string `json:"file_name"`
				MIMEType string `json:"mime_type"`
			} `json:"document"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Chat struct {
				Type string `json:"type"`
			} `json:"chat"`
		} `json:"message"`
		CallbackQuery *struct {
			ID   string `json:"id"`
			Data string `json:"data"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Message *struct {
				MessageID int64 `json:"message_id"`
				Chat      struct {
					ID   int64  `json:"id"`
					Type string `json:"type"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"callback_query"`
	}
	if err := json.Unmarshal(raw, &update); err != nil || update.UpdateID == 0 {
		http.Error(w, "invalid webhook payload", http.StatusBadRequest)
		return
	}
	if update.CallbackQuery != nil {
		callback = true
		if update.CallbackQuery.From.ID == 0 || update.CallbackQuery.Message == nil || update.CallbackQuery.Message.Chat.Type != "private" || !validCallbackAction(update.CallbackQuery.Data) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		cs, ok := h.store.(callbackStore)
		if !ok {
			http.Error(w, "telegram callback integration unavailable", http.StatusServiceUnavailable)
			return
		}
		_, err = cs.CaptureCallback(r.Context(), CaptureInput{UpdateID: update.UpdateID, TelegramUserID: update.CallbackQuery.From.ID, RawPayload: raw}, update.CallbackQuery.ID, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID)
		if errors.Is(err, ErrUnauthorized) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			http.Error(w, "webhook processing failed", http.StatusInternalServerError)
			return
		}
		_ = answerCallback(r.Context(), h.botToken, update.CallbackQuery.ID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if update.Message == nil || update.Message.From.ID == 0 || update.Message.Chat.Type != "private" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if token, ok := startToken(update.Message.Text); ok {
		_, err = h.store.Link(r.Context(), CaptureInput{UpdateID: update.UpdateID, TelegramUserID: update.Message.From.ID, RawPayload: raw}, token)
		if errors.Is(err, ErrUnauthorized) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			http.Error(w, "webhook processing failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if image, ok := imageFromUpdate(update.Message.Photo, update.Message.Document); ok {
		_, err = h.store.CaptureImage(r.Context(), ImageInput{CaptureInput: CaptureInput{UpdateID: update.UpdateID, TelegramUserID: update.Message.From.ID, RawPayload: raw}, FileID: image.fileID, FileName: image.fileName, MIMEType: image.mimeType, Caption: update.Message.Caption, MediaGroupID: update.Message.MediaGroupID, MessageID: update.Message.MessageID})
		if errors.Is(err, ErrUnauthorized) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			http.Error(w, "webhook processing failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if update.Message.Text == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	_, err = h.store.Capture(r.Context(), CaptureInput{
		UpdateID:       update.UpdateID,
		TelegramUserID: update.Message.From.ID,
		RawPayload:     raw,
	})
	if errors.Is(err, ErrUnauthorized) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "webhook processing failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func answerCallback(ctx context.Context, token, callbackID string) error {
	if token == "" || callbackID == "" {
		return nil
	}
	body := strings.NewReader(`{"callback_query_id":"` + strings.ReplaceAll(callbackID, `"`, ``) + `"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/answerCallbackQuery", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram callback ACK returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func validCallbackAction(value string) bool {
	switch value {
	case "review:expense", "review:own", "review:household", "review:confirm", "review:change", "review:remember", "review:once":
		return true
	}
	return strings.HasPrefix(value, "review:category:") && len(strings.TrimPrefix(value, "review:category:")) <= 120
}

type telegramImage struct{ fileID, fileName, mimeType string }

func imageFromUpdate(photos []struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}, document *struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MIMEType string `json:"mime_type"`
}) (telegramImage, bool) {
	if len(photos) > 0 {
		selected := photos[len(photos)-1]
		if selected.FileID != "" {
			return telegramImage{selected.FileID, "telegram-photo.jpg", "image/jpeg"}, true
		}
	}
	if document != nil && document.FileID != "" && (document.MIMEType == "image/jpeg" || document.MIMEType == "image/png") {
		return telegramImage{document.FileID, document.FileName, document.MIMEType}, true
	}
	return telegramImage{}, false
}

func startToken(text string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) != 2 || (parts[0] != "/start" && !strings.HasPrefix(parts[0], "/start@")) || len(parts[1]) < 20 || len(parts[1]) > 128 {
		return "", false
	}
	return parts[1], true
}

type PostgreSQLStore struct{ pool *pgxpool.Pool }

func NewPostgreSQLStore(pool *pgxpool.Pool) *PostgreSQLStore {
	return &PostgreSQLStore{pool: pool}
}

func (s *PostgreSQLStore) Capture(ctx context.Context, input CaptureInput) (bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin telegram intake: %w", err)
	}
	defer tx.Rollback(ctx)

	var householdID string
	if err := tx.QueryRow(ctx, `SELECT household_id FROM telegram_identity WHERE telegram_user_id=$1 AND active`, input.TelegramUserID).Scan(&householdID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrUnauthorized
		}
		return false, fmt.Errorf("authorize telegram user: %w", err)
	}

	payloadHash := sha256.Sum256(input.RawPayload)
	var sourceEventID string
	err = tx.QueryRow(ctx, `
		INSERT INTO source_event (household_id,source_type,external_id,received_at,payload_hash,processing_status)
		VALUES ($1,'TELEGRAM_TEXT',$2,now(),$3,'RECEIVED')
		ON CONFLICT DO NOTHING
		RETURNING id`, householdID, "telegram:update:"+strconv.FormatInt(input.UpdateID, 10), payloadHash[:]).Scan(&sourceEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create telegram source event: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO source_event_payload (source_event_id,payload_json) VALUES ($1,$2::jsonb)`, sourceEventID, string(input.RawPayload)); err != nil {
		return false, fmt.Errorf("preserve telegram payload: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO job (type,payload_json) VALUES ('PROCESS_TELEGRAM_TEXT',jsonb_build_object('source_event_id',$1::uuid))`, sourceEventID); err != nil {
		return false, fmt.Errorf("enqueue telegram processing: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit telegram intake: %w", err)
	}
	return true, nil
}

// CaptureCallback durably records a validated inline action without routing it
// through the general text/LLM pipeline.
func (s *PostgreSQLStore) CaptureCallback(ctx context.Context, input CaptureInput, callbackID string, chatID, messageID int64) (bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var householdID string
	if err := tx.QueryRow(ctx, `SELECT household_id FROM telegram_identity WHERE telegram_user_id=$1 AND active`, input.TelegramUserID).Scan(&householdID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrUnauthorized
		}
		return false, err
	}
	hash := sha256.Sum256(input.RawPayload)
	var sourceID string
	err = tx.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_CALLBACK',$2,now(),$3,'RECEIVED') ON CONFLICT DO NOTHING RETURNING id`, householdID, "telegram:update:"+strconv.FormatInt(input.UpdateID, 10), hash[:]).Scan(&sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(input.RawPayload)); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO job(type,lane,payload_json) VALUES('PROCESS_TELEGRAM_CALLBACK','INTERACTIVE',jsonb_build_object('source_event_id',$1::uuid,'callback_id',$2::text,'chat_id',$3::bigint,'message_id',$4::bigint))`, sourceID, callbackID, chatID, messageID); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *PostgreSQLStore) CaptureImage(ctx context.Context, input ImageInput) (bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin Telegram image intake: %w", err)
	}
	defer tx.Rollback(ctx)
	var householdID, userID string
	if err = tx.QueryRow(ctx, `SELECT household_id,user_id FROM telegram_identity WHERE telegram_user_id=$1 AND active`, input.TelegramUserID).Scan(&householdID, &userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrUnauthorized
		}
		return false, fmt.Errorf("authorize Telegram image: %w", err)
	}
	payloadHash := sha256.Sum256(input.RawPayload)
	var sourceID string
	err = tx.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_media_group_id,telegram_message_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'RECEIVED',NULLIF($4,''),NULLIF($5,0)) ON CONFLICT DO NOTHING RETURNING id`, householdID, "telegram:update:"+strconv.FormatInt(input.UpdateID, 10), payloadHash[:], input.MediaGroupID, input.MessageID).Scan(&sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create Telegram image source: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(input.RawPayload)); err != nil {
		return false, fmt.Errorf("preserve Telegram image payload: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json,max_attempts) VALUES('FETCH_TELEGRAM_IMAGE',jsonb_build_object('source_event_id',$1::uuid,'file_id',$2::text,'file_name',$3::text,'mime_type',$4::text,'caption',$5::text,'telegram_user_id',$6::bigint,'user_id',$7::uuid,'media_group_id',NULLIF($8,''),'message_id',NULLIF($9,0)),5)`, sourceID, input.FileID, input.FileName, input.MIMEType, input.Caption, input.TelegramUserID, userID, input.MediaGroupID, input.MessageID); err != nil {
		return false, fmt.Errorf("queue Telegram image: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit Telegram image intake: %w", err)
	}
	return true, nil
}

func (s *PostgreSQLStore) Link(ctx context.Context, input CaptureInput, token string) (bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin Telegram link: %w", err)
	}
	defer tx.Rollback(ctx)
	digest := sha256.Sum256([]byte(token))
	var inviteID, householdID, userID string
	err = tx.QueryRow(ctx, `SELECT id,household_id,user_id FROM telegram_link_invite WHERE token_hash=$1 AND status='PENDING' AND expires_at>now() FOR UPDATE`, digest[:]).Scan(&inviteID, &householdID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = tx.Exec(ctx, `UPDATE telegram_link_invite SET status='EXPIRED' WHERE token_hash=$1 AND status='PENDING' AND expires_at<=now()`, digest[:])
		_ = tx.Commit(ctx)
		return false, ErrUnauthorized
	}
	if err != nil {
		return false, fmt.Errorf("validate Telegram invite: %w", err)
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT active FROM household_member WHERE household_id=$1 AND user_id=$2`, householdID, userID).Scan(&active); err != nil || !active {
		return false, ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, input.TelegramUserID, householdID, userID); err != nil {
		return false, ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_link_invite SET status='CONSUMED',consumed_at=now() WHERE id=$1`, inviteID); err != nil {
		return false, fmt.Errorf("consume Telegram invite: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM','LINK','telegram_identity',$2,jsonb_build_object('userId',$3::text,'telegramUserId',$4::bigint))`, householdID, userID, userID, input.TelegramUserID); err != nil {
		return false, fmt.Errorf("audit Telegram link: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'text','Telegram berhasil terhubung ke Richmod.'))`, input.TelegramUserID); err != nil {
		return false, fmt.Errorf("queue Telegram confirmation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit Telegram link: %w", err)
	}
	return true, nil
}

func sameSecret(got, want string) bool {
	gotHash := sha256.Sum256([]byte(got))
	wantHash := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
}
