package document

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/blob"
)

const maxUploadBytes = 10 << 20
const maxImagePixels = 24_000_000
const maxImagesPerDocument = 10

type Handler struct {
	pool    *pgxpool.Pool
	root    string
	storage *blob.Store
}
type normalizedPage struct {
	normalized                  []byte
	mediaType                   string
	width, height               int
	extension, storageRef, path string
}

func NewHandler(pool *pgxpool.Pool, root string) (*Handler, error) {
	storage, err := blob.NewLocal(root)
	if err != nil {
		return nil, err
	}
	return &Handler{pool: pool, root: filepath.Clean(root), storage: storage}, nil
}

func NewHandlerWithStorage(pool *pgxpool.Pool, storage *blob.Store) *Handler {
	return &Handler{pool: pool, storage: storage}
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	p, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
	parts, err := uploadParts(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if len(parts) == 0 || len(parts) > maxImagesPerDocument {
		writeJSON(w, 400, map[string]string{"error": "unggah 1 sampai 10 gambar"})
		return
	}
	pages := make([]normalizedPage, 0, len(parts))
	combined := sha256.New()
	totalNormalized := 0
	for _, part := range parts {
		raw, readErr := readUpload(part.reader)
		if readErr != nil {
			writeJSON(w, 400, map[string]string{"error": "image must be between 1 byte and 10 MB"})
			return
		}
		normalized, mediaType, width, height, extension, normErr := normalizeImage(raw, part.filename)
		if normErr != nil {
			writeJSON(w, 400, map[string]string{"error": normErr.Error()})
			return
		}
		totalNormalized += len(normalized)
		if totalNormalized > maxUploadBytes {
			writeJSON(w, 400, map[string]string{"error": "total normalized document exceeds 10 MB"})
			return
		}
		ref, path, storeErr := h.store(household, normalized, extension)
		if storeErr != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to store document"})
			return
		}
		combined.Write(normalized)
		pages = append(pages, normalizedPage{normalized, mediaType, width, height, extension, ref, path})
	}
	removeNew := true
	defer func() {
		if removeNew {
			for _, pg := range pages {
				_ = h.storage.Delete(context.Background(), pg.storageRef)
			}
		}
	}()
	digest := sha256.Sum256(combined.Sum(nil))
	documentID, err := h.persistPages(r.Context(), p, household, digest[:], pages)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to register document"})
		return
	}
	removeNew = false
	writeJSON(w, 201, map[string]any{"id": documentID, "status": "RECEIVED", "pageCount": len(pages)})
}

func readUpload(source io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(source, maxUploadBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxUploadBytes {
		return nil, fmt.Errorf("image must be between 1 byte and 10 MB")
	}
	return raw, nil
}

type uploadPartData struct {
	filename string
	reader   io.Reader
}

func uploadParts(r *http.Request) ([]uploadPartData, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("multipart file upload required")
	}
	result := make([]uploadPartData, 0, maxImagesPerDocument)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid multipart upload")
		}
		if part.FormName() == "file" && part.FileName() != "" {
			data, readErr := io.ReadAll(io.LimitReader(part, maxUploadBytes+1))
			part.Close()
			if readErr != nil {
				return nil, fmt.Errorf("invalid multipart upload")
			}
			result = append(result, uploadPartData{part.FileName(), bytes.NewReader(data)})
			if len(result) > maxImagesPerDocument {
				return nil, fmt.Errorf("too many images")
			}
		}
		part.Close()
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("file is required")
	}
	return result, nil
}

func (h *Handler) persistPages(ctx context.Context, p auth.Principal, household string, digest []byte, pages []normalizedPage) (string, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var sourceID string
	if err := tx.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,received_at,payload_hash,processing_status) VALUES($1,'WEB_IMAGE',now(),$2,'RECEIVED') RETURNING id`, household, digest).Scan(&sourceID); err != nil {
		return "", err
	}
	var primaryAttachment, documentID string
	for i, pg := range pages {
		metadata, _ := json.Marshal(map[string]any{"media_type": pg.mediaType, "byte_size": len(pg.normalized), "width": pg.width, "height": pg.height, "page_index": i})
		if i == 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO source_event_payload(source_event_id,payload_json) VALUES($1,$2::jsonb)`, sourceID, string(metadata)); err != nil {
				return "", err
			}
		}
		var attachmentID string
		if err := tx.QueryRow(ctx, `INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(household_id,content_hash) DO UPDATE SET content_hash=excluded.content_hash RETURNING id`, household, sha256Bytes(pg.normalized), pg.mediaType, len(pg.normalized), pg.width, pg.height, pg.storageRef).Scan(&attachmentID); err != nil {
			return "", err
		}
		if i == 0 {
			primaryAttachment = attachmentID
			if err := tx.QueryRow(ctx, `INSERT INTO document(household_id,source_event_id,attachment_id,status) VALUES($1,$2,$3,'RECEIVED') RETURNING id`, household, sourceID, attachmentID).Scan(&documentID); err != nil {
				return "", err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_page(document_id,source_event_id,attachment_id,page_index) VALUES($1,$2,$3,$4)`, documentID, sourceID, attachmentID, i); err != nil {
			return "", err
		}
	}
	if primaryAttachment == "" || documentID == "" {
		return "", fmt.Errorf("document has no pages")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json,max_attempts) VALUES('PROCESS_DOCUMENT',jsonb_build_object('document_id',$1::uuid),5)`, documentID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'UPLOAD_DOCUMENT','source_event',$3,jsonb_build_object('document_id',$4::uuid,'page_count',$5::integer))`, household, p.UserID, sourceID, documentID, len(pages)); err != nil {
		return "", err
	}
	return documentID, tx.Commit(ctx)
}

func sha256Bytes(value []byte) []byte { sum := sha256.Sum256(value); return sum[:] }

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
	if h.storage == nil {
		storage, err := blob.NewLocal(h.root)
		if err != nil {
			return "", "", err
		}
		h.storage = storage
	}
	ref, err := h.storage.Ref(household, extension)
	if err != nil {
		return "", "", err
	}
	if err := h.storage.Put(context.Background(), ref, content, http.DetectContentType(content)); err != nil {
		return "", "", err
	}
	return ref, filepath.Join(h.root, filepath.FromSlash(ref)), nil
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
	rows, err := h.pool.Query(r.Context(), `
		SELECT d.id,d.document_type,d.status,a.media_type,a.byte_size,a.width,a.height,d.created_at,
		       s.source_type,x.confidence::text,x.output_json,COALESCE(linked.transaction_ids,'{}'::text[]),COALESCE(linked.needs_review,false)
		FROM document d
		JOIN attachment a ON a.id=d.attachment_id
		JOIN source_event s ON s.id=d.source_event_id
		LEFT JOIN LATERAL (SELECT confidence,output_json FROM document_extraction WHERE document_id=d.id ORDER BY created_at DESC LIMIT 1) x ON true
		LEFT JOIN LATERAL (
			SELECT array_agg(DISTINCT t.id::text) AS transaction_ids,bool_or(t.status='NEEDS_REVIEW') AS needs_review
			FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id
			WHERE te.source_event_id=d.source_event_id
		) linked ON true
		WHERE d.household_id=$1 ORDER BY d.created_at DESC LIMIT 100`, household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list documents"})
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, status, media, source string
		var kind *string
		var confidence *string
		var summary json.RawMessage
		var linked []string
		var needsReview bool
		var size int64
		var width, height int
		var created any
		if rows.Scan(&id, &kind, &status, &media, &size, &width, &height, &created, &source, &confidence, &summary, &linked, &needsReview) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list documents"})
			return
		}
		result = append(result, map[string]any{"id": id, "documentType": kind, "status": status, "mediaType": media, "byteSize": size, "width": width, "height": height, "createdAt": created, "sourceType": source, "confidence": confidence, "summary": summary, "linkedTransactionIds": linked, "needsReview": needsReview})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list documents"})
		return
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
	r.SetPathValue("index", "0")
	h.PageContent(w, r)
}

func (h *Handler) Pages(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT dp.page_index,a.media_type,a.byte_size,a.width,a.height FROM document_page dp JOIN document d ON d.id=dp.document_id JOIN attachment a ON a.id=dp.attachment_id WHERE d.id=$1 AND d.household_id=$2 ORDER BY dp.page_index`, r.PathValue("id"), household)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to list document pages"})
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var index, width, height int
		var media string
		var size int64
		if rows.Scan(&index, &media, &size, &width, &height) != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to list document pages"})
			return
		}
		result = append(result, map[string]any{"index": index, "mediaType": media, "byteSize": size, "width": width, "height": height})
	}
	writeJSON(w, 200, result)
}

func (h *Handler) PageContent(w http.ResponseWriter, r *http.Request) {
	_, household, ok := principalHousehold(w, r)
	if !ok {
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 || index >= maxImagesPerDocument {
		writeJSON(w, 400, map[string]string{"error": "invalid document page index"})
		return
	}
	var ref, media string
	if err := h.pool.QueryRow(r.Context(), `SELECT a.storage_ref,a.media_type FROM document_page dp JOIN document d ON d.id=dp.document_id JOIN attachment a ON a.id=dp.attachment_id WHERE d.id=$1 AND d.household_id=$2 AND dp.page_index=$3`, r.PathValue("id"), household, index).Scan(&ref, &media); errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "document not found"})
		return
	} else if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load document"})
		return
	}
	if h.storage == nil {
		storage, storageErr := blob.NewLocal(h.root)
		if storageErr != nil {
			writeJSON(w, 500, map[string]string{"error": "document content unavailable"})
			return
		}
		h.storage = storage
	}
	location, remote, err := h.storage.PresignedGet(r.Context(), ref, media)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "document content unavailable"})
		return
	}
	if remote {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusFound)
		return
	}
	raw, err := h.storage.Read(r.Context(), ref)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "document content unavailable"})
		return
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(raw)
}

func principalHousehold(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !p.HasHousehold {
		writeJSON(w, 403, map[string]string{"error": "household membership required"})
		return auth.Principal{}, "", false
	}
	return p, p.HouseholdID, true
}
func writeJSON(w http.ResponseWriter, status int, output any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(output)
}
