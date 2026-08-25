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
)

const maxWebhookBytes = 1 << 20

var ErrUnauthorized = errors.New("telegram user is not authorized")

type CaptureInput struct {
	UpdateID       int64
	TelegramUserID int64
	RawPayload     []byte
}

type Store interface {
	Capture(context.Context, CaptureInput) (bool, error)
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
			MessageID int64  `json:"message_id"`
			Text      string `json:"text"`
			From      struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Chat struct {
				Type string `json:"type"`
			} `json:"chat"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &update); err != nil || update.UpdateID == 0 {
		http.Error(w, "invalid webhook payload", http.StatusBadRequest)
		return
	}
	if update.Message == nil || update.Message.Text == "" || update.Message.From.ID == 0 || update.Message.Chat.Type != "private" {
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
