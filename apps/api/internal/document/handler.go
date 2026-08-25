package document

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
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

const maxUploadBytes = 10 << 20
const maxImagePixels = 24_000_000

type Handler struct {
	pool *pgxpool.Pool
	root string
}

func NewHandler(pool *pgxpool.Pool, root string) (*Handler, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("DOCUMENT_STORAGE_PATH must be absolute")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create document storage: %w", err)
	}
	return &Handler{pool: pool, root: filepath.Clean(root)}, nil
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
	part, err := uploadPart(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	defer part.Close()
	raw, err := io.ReadAll(io.LimitReader(part, maxUploadBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxUploadBytes {
		writeJSON(w, 400, map[string]string{"error": "image must be between 1 byte and 10 MB"})
		return
	}
	normalized, mediaType, width, height, extension, err := normalizeImage(raw, part.FileName())
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if len(normalized) > maxUploadBytes {
		writeJSON(w, 400, map[string]string{"error": "normalized image exceeds 10 MB"})
		return
	}
	digest := sha256.Sum256(normalized)
	storageRef, path, err := h.store(household, normalized, extension)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to store document"})
		return
	}
	removeNew := true
	defer func() {
		if removeNew {
			_ = os.Remove(path)
		}
	}()
	documentID, actualRef, err := h.persist(r.Context(), p, household, digest[:], normalized, mediaType, width, height, storageRef)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to register document"})
		return
	}
	if actualRef == storageRef {
		removeNew = false
	}
	writeJSON(w, 201, map[string]any{"id": documentID, "status": "RECEIVED", "mediaType": mediaType, "width": width, "height": height})
}

func uploadPart(r *http.Request) (*multipart.Part, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("multipart file upload required")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("file is required")
		}
		if err != nil {
			return nil, fmt.Errorf("invalid multipart upload")
		}
		if part.FormName() == "file" && part.FileName() != "" {
			return part, nil
		}
		part.Close()
	}
}

func normalizeImage(raw []byte, filename string) ([]byte, string, int, int, string, error) {
	extension := strings.ToLower(filepath.Ext(filename))
	mediaType := http.DetectContentType(raw)
	validExtension := (mediaType == "image/jpeg" && (extension == ".jpg" || extension == ".jpeg")) || (mediaType == "image/png" && extension == ".png")
	if !validExtension {
		return nil, "", 0, 0, "", fmt.Errorf("only matching JPEG or PNG images are supported")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 10000 || config.Height > 10000 || int64(config.Width)*int64(config.Height) > maxImagePixels {
		return nil, "", 0, 0, "", fmt.Errorf("image dimensions are invalid or exceed 24 megapixels")
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", 0, 0, "", fmt.Errorf("image content is invalid")
	}
	var output bytes.Buffer
	if mediaType == "image/jpeg" {
		err = jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90})
		extension = ".jpg"
	} else {
		err = png.Encode(&output, decoded)
	}
	if err != nil {
		return nil, "", 0, 0, "", fmt.Errorf("unable to normalize image")
	}
	return output.Bytes(), mediaType, config.Width, config.Height, extension, nil
}

func (h *Handler) store(household string, content []byte, extension string) (string, string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	directory := filepath.Join(h.root, household)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", "", err
	}
	name := hex.EncodeToString(random) + extension
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		_ = os.Remove(path)
		return "", "", err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return filepath.Join(household, name), path, err
}

func (h *Handler) persist(ctx context.Context, p auth.Principal, household string, digest, content []byte, mediaType string, width, height int, storageRef string) (string, string, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var sourceID string
	if err := tx.QueryRow(ctx, `INSERT INTO source_event (household_id,source_type,received_at,payload_hash,processing_status) VALUES ($1,'WEB_IMAGE',now(),$2,'RECEIVED') RETURNING id`, household, digest).Scan(&sourceID); err != nil {
		return "", "", err
	}
	metadata, _ := json.Marshal(map[string]any{"media_type": mediaType, "byte_size": len(content), "width": width, "height": height})
	if _, err := tx.Exec(ctx, `INSERT INTO source_event_payload (source_event_id,payload_json) VALUES ($1,$2::jsonb)`, sourceID, string(metadata)); err != nil {
		return "", "", err
	}
	var attachmentID, actualRef string
	if err := tx.QueryRow(ctx, `INSERT INTO attachment (household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (household_id,content_hash) DO UPDATE SET content_hash=excluded.content_hash RETURNING id,storage_ref`, household, digest, mediaType, len(content), width, height, storageRef).Scan(&attachmentID, &actualRef); err != nil {
		return "", "", err
	}
	var documentID string
	if err := tx.QueryRow(ctx, `INSERT INTO document (household_id,source_event_id,attachment_id,status) VALUES ($1,$2,$3,'RECEIVED') RETURNING id`, household, sourceID, attachmentID).Scan(&documentID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO job (type,payload_json,max_attempts) VALUES ('PROCESS_DOCUMENT',jsonb_build_object('document_id',$1::uuid),5)`, documentID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES ($1,'USER',$2,'UPLOAD_DOCUMENT','source_event',$3,jsonb_build_object('document_id',$4::uuid,'media_type',$5::text,'byte_size',$6::bigint))`, household, p.UserID, sourceID, documentID, mediaType, len(content)); err != nil {
		return "", "", err
	}
	return documentID, actualRef, tx.Commit(ctx)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT d.id,d.document_type,d.status,a.media_type,a.byte_size,a.width,a.height,d.created_at FROM document d JOIN attachment a ON a.id=d.attachment_id WHERE d.household_id=$1 ORDER BY d.created_at DESC LIMIT 100`, household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list documents"})
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, status, media string
		var kind *string
		var size int64
		var width, height int
		var created any
		if rows.Scan(&id, &kind, &status, &media, &size, &width, &height, &created) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list documents"})
			return
		}
		result = append(result, map[string]any{"id": id, "documentType": kind, "status": status, "mediaType": media, "byteSize": size, "width": width, "height": height, "createdAt": created})
	}
	writeJSON(w, 200, result)
}

func (h *Handler) Extraction(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT e.stage,e.schema_version,e.output_json,e.confidence::text,e.validated,e.created_at FROM document_extraction e JOIN document d ON d.id=e.document_id WHERE d.id=$1 AND d.household_id=$2 ORDER BY e.created_at`, r.PathValue("id"), household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load extraction"})
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var stage, version string
		var output json.RawMessage
		var confidence *string
		var validated bool
		var created any
		if rows.Scan(&stage, &version, &output, &confidence, &validated, &created) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to load extraction"})
			return
		}
		result = append(result, map[string]any{"stage": stage, "schemaVersion": version, "output": output, "confidence": confidence, "validated": validated, "createdAt": created})
	}
	writeJSON(w, 200, result)
}

func (h *Handler) Content(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	var ref, media string
	if err := h.pool.QueryRow(r.Context(), `SELECT a.storage_ref,a.media_type FROM document d JOIN attachment a ON a.id=d.attachment_id WHERE d.id=$1 AND d.household_id=$2`, r.PathValue("id"), household).Scan(&ref, &media); errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "document not found"})
		return
	} else if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load document"})
		return
	}
	path := filepath.Join(h.root, ref)
	relative, err := filepath.Rel(h.root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		writeJSON(w, 500, map[string]string{"error": "invalid document storage reference"})
		return
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, path)
}

func principalHousehold(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || len(p.Memberships) == 0 {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return auth.Principal{}, "", false
	}
	return p, p.Memberships[0].HouseholdID, true
}
func writeJSON(w http.ResponseWriter, status int, output any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(output)
}
