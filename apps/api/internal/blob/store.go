package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const maxObjectSize = 10 << 20
const presignedGETTTL = 2 * time.Minute

type Store struct {
	root, prefix string
	client       *minio.Client
	bucket       string
}

func NewLocal(root string) (*Store, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("DOCUMENT_STORAGE_PATH must be absolute")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create document storage: %w", err)
	}
	return &Store{root: filepath.Clean(root)}, nil
}

func NewFromEnv(root string) (*Store, error) {
	s, err := NewLocal(root)
	if err != nil {
		return nil, err
	}
	s.prefix = strings.Trim(os.Getenv("OSS_PREFIX"), "/")
	endpoint := strings.TrimSpace(os.Getenv("OSS_ENDPOINT"))
	access, secret, bucket := os.Getenv("OSS_ACCESS_KEY"), os.Getenv("OSS_SECRET_KEY"), os.Getenv("OSS_BUCKET")
	if endpoint == "" && access == "" && secret == "" && bucket == "" {
		return s, nil
	}
	if endpoint == "" || access == "" || secret == "" || bucket == "" {
		return nil, fmt.Errorf("OSS_ENDPOINT, OSS_ACCESS_KEY, OSS_SECRET_KEY, and OSS_BUCKET must be set together")
	}
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	secure := !strings.HasPrefix(strings.TrimSpace(os.Getenv("OSS_ENDPOINT")), "http://")
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: secure, Region: os.Getenv("OSS_REGION")})
	if err != nil {
		return nil, fmt.Errorf("configure OSS: %w", err)
	}
	s.client, s.bucket = client, bucket
	return s, nil
}

func (s *Store) Ref(household, extension string) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(household, hex.EncodeToString(b)+extension)), nil
}
func (s *Store) key(ref string) string {
	if s.prefix == "" {
		return "attachments/" + ref
	}
	return s.prefix + "/attachments/" + ref
}
func (s *Store) localPath(ref string) (string, error) {
	path := filepath.Join(s.root, filepath.FromSlash(ref))
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(ref) {
		return "", fmt.Errorf("invalid document storage reference")
	}
	return path, nil
}
func (s *Store) Put(ctx context.Context, ref string, content []byte, mediaType string) error {
	path, err := s.localPath(ref)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		return err
	}
	if s.client == nil {
		return nil
	}
	if _, err := s.client.PutObject(ctx, s.bucket, s.key(ref), bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{ContentType: mediaType}); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("upload object: %w", err)
	}
	return nil
}
func (s *Store) Read(ctx context.Context, ref string) ([]byte, error) {
	path, err := s.localPath(ref)
	if err != nil {
		return nil, err
	}
	if raw, err := os.ReadFile(path); err == nil {
		if len(raw) == 0 || len(raw) > maxObjectSize {
			return nil, fmt.Errorf("stored document size is invalid")
		}
		return raw, nil
	}
	if s.client == nil {
		return nil, os.ErrNotExist
	}
	obj, err := s.client.GetObject(ctx, s.bucket, s.key(ref), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	raw, err := io.ReadAll(io.LimitReader(obj, maxObjectSize+1))
	if err != nil || len(raw) == 0 || len(raw) > maxObjectSize {
		return nil, fmt.Errorf("stored document size is invalid")
	}
	return raw, nil
}
func (s *Store) EvictLocal(ref string) error {
	path, err := s.localPath(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (s *Store) PresignedGet(ctx context.Context, ref, mediaType string) (string, bool, error) {
	if _, err := s.localPath(ref); err != nil {
		return "", false, err
	}
	if s.client == nil {
		return "", false, nil
	}
	location, err := s.client.PresignedGetObject(ctx, s.bucket, s.key(ref), presignedGETTTL, url.Values{
		"response-content-disposition": {"inline"},
		"response-content-type":        {mediaType},
	})
	if err != nil {
		return "", false, fmt.Errorf("presign object: %w", err)
	}
	return location.String(), true, nil
}
func (s *Store) Delete(ctx context.Context, ref string) error {
	path, err := s.localPath(ref)
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	if s.client == nil {
		return nil
	}
	return s.client.RemoveObject(ctx, s.bucket, s.key(ref), minio.RemoveObjectOptions{})
}
