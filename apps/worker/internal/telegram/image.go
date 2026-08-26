package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxTelegramImageBytes int64 = 10 << 20

type ImagePayload struct {
	SourceEventID  string `json:"source_event_id"`
	FileID         string `json:"file_id"`
	FileName       string `json:"file_name"`
	MIMEType       string `json:"mime_type"`
	Caption        string `json:"caption"`
	TelegramUserID int64  `json:"telegram_user_id"`
	UserID         string `json:"user_id"`
	MediaGroupID   string `json:"media_group_id"`
	MessageID      int64  `json:"message_id"`
}

type ImageProcessor struct {
	pool *pgxpool.Pool
	bot  *Bot
	root string
}

func NewImageProcessor(pool *pgxpool.Pool, bot *Bot, root string) (*ImageProcessor, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("DOCUMENT_STORAGE_PATH must be absolute")
	}
	return &ImageProcessor{pool: pool, bot: bot, root: filepath.Clean(root)}, nil
}

func DecodeImagePayload(raw json.RawMessage) (ImagePayload, error) {
	var p ImagePayload
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || p.SourceEventID == "" || p.FileID == "" || p.UserID == "" || p.TelegramUserID == 0 {
		return ImagePayload{}, fmt.Errorf("invalid Telegram image job payload")
	}
	return p, nil
}

func (p *ImageProcessor) Process(ctx context.Context, input ImagePayload) error {
	var householdID, status string
	if err := p.pool.QueryRow(ctx, `SELECT household_id,processing_status FROM source_event WHERE id=$1 AND source_type='TELEGRAM_IMAGE'`, input.SourceEventID).Scan(&householdID, &status); err != nil {
		return fmt.Errorf("load Telegram image source: %w", err)
	}
	if status == "PROCESSING" || status == "PROCESSED" || status == "NEEDS_REVIEW" || status == "IGNORED" {
		return nil
	}
	raw, pathName, err := p.bot.Download(ctx, input.FileID, maxTelegramImageBytes)
	if err != nil {
		return err
	}
	filename := input.FileName
	if filename == "" {
		filename = filepath.Base(pathName)
	}
	normalized, media, width, height, extension, err := normalizeTelegramImage(raw, filename)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(normalized)
	storageRef, path, err := storeTelegramImage(p.root, householdID, normalized, extension)
	if err != nil {
		return fmt.Errorf("store Telegram image: %w", err)
	}
	removeNew := true
	defer func() {
		if removeNew {
			_ = os.Remove(path)
		}
	}()
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Telegram delivers album images as separate updates. Serialize grouped
	// updates so concurrent workers converge on one logical document.
	if input.MediaGroupID != "" {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, householdID+":"+input.MediaGroupID); err != nil {
			return err
		}
	}
	var attachmentID, actualRef string
	if err = tx.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(household_id,content_hash) DO UPDATE SET content_hash=excluded.content_hash RETURNING id,storage_ref`, householdID, digest[:], media, len(normalized), width, height, storageRef).Scan(&attachmentID, &actualRef); err != nil {
		return err
	}
	var documentID string
	if input.MediaGroupID != "" {
		err = tx.QueryRow(ctx, `SELECT d.id FROM document d JOIN source_event s ON s.id=d.source_event_id WHERE d.household_id=$1 AND s.telegram_media_group_id=$2 ORDER BY d.created_at,d.id LIMIT 1`, householdID, input.MediaGroupID).Scan(&documentID)
	}
	if errors.Is(err, pgx.ErrNoRows) || input.MediaGroupID == "" {
		err = tx.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'RECEIVED') ON CONFLICT(source_event_id) DO UPDATE SET source_event_id=excluded.source_event_id RETURNING id`, householdID, input.SourceEventID, attachmentID).Scan(&documentID)
	}
	if err != nil {
		return err
	}
	pageIndex := input.MessageID
	if input.MediaGroupID == "" {
		pageIndex = 0
	} else if pageIndex == 0 {
		if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(page_index)+1,0) FROM document_page WHERE document_id=$1`, documentID).Scan(&pageIndex); err != nil { return err }
	}
	if _, err = tx.Exec(ctx, `INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, documentID, input.SourceEventID, attachmentID, pageIndex); err != nil { return err }
	metadata, _ := json.Marshal(map[string]any{"media_type": media, "byte_size": len(normalized), "width": width, "height": height, "caption": input.Caption, "telegram_user_id": input.TelegramUserID})
	if _, err = tx.Exec(ctx, `UPDATE source_event_payload SET payload_json=payload_json || $2::jsonb WHERE source_event_id=$1`, input.SourceEventID, string(metadata)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json,run_after,max_attempts) SELECT 'PROCESS_DOCUMENT',jsonb_build_object('document_id',$1::uuid),CASE WHEN $2<>'' THEN now()+interval '5 seconds' ELSE now() END,5 WHERE NOT EXISTS(SELECT 1 FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id'=$1::text AND status IN ('PENDING','RUNNING'))`, documentID, input.MediaGroupID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSING',parser_name='telegram-image-v1',parser_version='1' WHERE id=$1`, input.SourceEventID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'UPLOAD_DOCUMENT','source_event',$3,jsonb_build_object('document_id',$4::uuid,'media_type',$5::text,'byte_size',$6::bigint))`, householdID, input.UserID, input.SourceEventID, documentID, media, len(normalized)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if actualRef == storageRef {
		removeNew = false
	}
	return nil
}

func normalizeTelegramImage(raw []byte, filename string) ([]byte, string, int, int, string, error) {
	media := http.DetectContentType(raw)
	extension := strings.ToLower(filepath.Ext(filename))
	if !((media == "image/jpeg" && (extension == ".jpg" || extension == ".jpeg")) || (media == "image/png" && extension == ".png")) {
		return nil, "", 0, 0, "", fmt.Errorf("only matching JPEG or PNG Telegram images are supported")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 10000 || config.Height > 10000 || int64(config.Width)*int64(config.Height) > 24_000_000 {
		return nil, "", 0, 0, "", fmt.Errorf("Telegram image dimensions are invalid")
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", 0, 0, "", fmt.Errorf("Telegram image content is invalid")
	}
	var output bytes.Buffer
	if media == "image/jpeg" {
		err = jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90})
		extension = ".jpg"
	} else {
		err = png.Encode(&output, decoded)
	}
	if err != nil || output.Len() > int(maxTelegramImageBytes) {
		return nil, "", 0, 0, "", fmt.Errorf("unable to normalize Telegram image")
	}
	return output.Bytes(), media, config.Width, config.Height, extension, nil
}

func storeTelegramImage(root, householdID string, content []byte, extension string) (string, string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	directory := filepath.Join(root, householdID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", "", err
	}
	name := hex.EncodeToString(random) + extension
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", err
	}
	if _, err = file.Write(content); err != nil {
		file.Close()
		_ = os.Remove(path)
		return "", "", err
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		return "", "", err
	}
	return filepath.Join(householdID, name), path, nil
}
