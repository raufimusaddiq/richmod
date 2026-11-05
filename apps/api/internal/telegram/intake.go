package telegram

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/provision"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
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
	// ChatID is the private chat the upload arrived in. Message ids are unique
	// only per chat, so reply binding needs it.
	ChatID int64
}

type Store interface {
	Capture(context.Context, CaptureInput) (bool, error)
	CaptureImage(context.Context, ImageInput) (bool, error)
	CaptureCallback(context.Context, CaptureInput, string, int64, int64) (bool, error)
	Link(context.Context, CaptureInput, string) (bool, error)
}

type Handler struct {
	store  Store
	secret string
}

func NewHandler(store Store, secret string) *Handler {
	return &Handler{store: store, secret: secret}
}

func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
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
				ID   int64  `json:"id"`
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
		if update.CallbackQuery.From.ID == 0 || update.CallbackQuery.Message == nil || update.CallbackQuery.Message.Chat.Type != "private" || !validCallbackAction(update.CallbackQuery.Data) {
			answerCallbackInResponse(w, update.CallbackQuery.ID)
			return
		}
		_, err = h.store.CaptureCallback(r.Context(), CaptureInput{UpdateID: update.UpdateID, TelegramUserID: update.CallbackQuery.From.ID, RawPayload: raw}, update.CallbackQuery.ID, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID)
		if err != nil && !errors.Is(err, ErrUnauthorized) {
			// Not answered: Telegram redelivers the update, and the retry answers.
			http.Error(w, "webhook processing failed", http.StatusInternalServerError)
			return
		}
		answerCallbackInResponse(w, update.CallbackQuery.ID)
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
		_, err = h.store.CaptureImage(r.Context(), ImageInput{CaptureInput: CaptureInput{UpdateID: update.UpdateID, TelegramUserID: update.Message.From.ID, RawPayload: raw}, FileID: image.fileID, FileName: image.fileName, MIMEType: image.mimeType, Caption: update.Message.Caption, MediaGroupID: update.Message.MediaGroupID, MessageID: update.Message.MessageID, ChatID: update.Message.Chat.ID})
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

// answerCallbackInResponse acknowledges a callback query in the webhook's own
// HTTP response: Telegram runs a Bot API method returned as the webhook reply,
// so the tap spinner clears as soon as the tap is durably captured, without the
// bot token in the API and without waiting for the worker's queue. The answer
// carries no text; the worker sends the outcome as a message.
func answerCallbackInResponse(w http.ResponseWriter, callbackQueryID string) {
	if callbackQueryID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"method": "answerCallbackQuery", "callback_query_id": callbackQueryID})
}

// validCallbackAction admits only the callback grammar the system issues; the
// grammar is shared with the worker so the two cannot drift apart.
func validCallbackAction(value string) bool {
	return reviewdomain.ValidTelegramCallback(value)
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
	if err := tx.QueryRow(ctx, `SELECT ti.household_id FROM telegram_identity ti JOIN "user" u ON u.id=ti.user_id AND u.active JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active WHERE ti.telegram_user_id=$1 AND ti.active`, input.TelegramUserID).Scan(&householdID); err != nil {
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
	if err := tx.QueryRow(ctx, `SELECT ti.household_id FROM telegram_identity ti JOIN "user" u ON u.id=ti.user_id AND u.active JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active WHERE ti.telegram_user_id=$1 AND ti.active`, input.TelegramUserID).Scan(&householdID); err != nil {
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
	if err = tx.QueryRow(ctx, `SELECT ti.household_id,ti.user_id FROM telegram_identity ti JOIN "user" u ON u.id=ti.user_id AND u.active JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active WHERE ti.telegram_user_id=$1 AND ti.active`, input.TelegramUserID).Scan(&householdID, &userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrUnauthorized
		}
		return false, fmt.Errorf("authorize Telegram image: %w", err)
	}
	payloadHash := sha256.Sum256(input.RawPayload)
	var sourceID string
	err = tx.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,telegram_media_group_id,telegram_message_id,telegram_chat_id) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'RECEIVED',NULLIF($4,''),NULLIF($5,0),NULLIF($6::bigint,0)) ON CONFLICT DO NOTHING RETURNING id`, householdID, "telegram:update:"+strconv.FormatInt(input.UpdateID, 10), payloadHash[:], input.MediaGroupID, input.MessageID, input.ChatID).Scan(&sourceID)
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
	if err = tx.QueryRow(ctx, `SELECT hm.active AND u.active FROM household_member hm JOIN "user" u ON u.id=hm.user_id WHERE hm.household_id=$1 AND hm.user_id=$2`, householdID, userID).Scan(&active); err != nil || !active {
		return false, ErrUnauthorized
	}
	// Telegram ID and user are bound 1:1: an ID already bound to anyone, or a
	// member who already has a Telegram ID, is refused (ADR-059).
	if err = provision.BindTelegramIdentity(ctx, tx, input.TelegramUserID, householdID, userID); err != nil {
		return false, ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_link_invite SET status='CONSUMED',consumed_at=now() WHERE id=$1`, inviteID); err != nil {
		return false, fmt.Errorf("consume Telegram invite: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM','LINK','telegram_identity',$2,jsonb_build_object('userId',$3::text,'telegramUserId',$4::bigint))`, householdID, userID, userID, input.TelegramUserID); err != nil {
		return false, fmt.Errorf("audit Telegram link: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('SEND_TELEGRAM_MESSAGE',jsonb_build_object('chat_id',$1::bigint,'text','✅ Telegram kamu sudah terhubung ke Richmod. Ketik /help untuk lihat contoh pesan.'))`, input.TelegramUserID); err != nil {
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
