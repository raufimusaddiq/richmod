package blob

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestLiveOSSRoundTrip(t *testing.T) {
	if os.Getenv("TEST_OSS") != "1" {
		t.Skip("set TEST_OSS=1 for live OSS smoke")
	}
	store, err := NewFromEnv(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Ref("smoke", ".txt")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Delete(context.Background(), ref)
	if err := store.Put(context.Background(), ref, []byte("richmod-oss-smoke"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.root, filepath.FromSlash(ref))); err != nil {
		t.Fatal(err)
	}
	raw, err := store.Read(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "richmod-oss-smoke" {
		t.Fatalf("unexpected object content %q", raw)
	}
	if _, err := os.Stat(filepath.Join(store.root, filepath.FromSlash(ref))); !os.IsNotExist(err) {
		t.Fatalf("remote read repopulated local cache: %v", err)
	}
	location, remote, err := store.PresignedGet(context.Background(), ref, "text/plain")
	if err != nil || !remote {
		t.Fatalf("presign = %q, %v, %v", location, remote, err)
	}
	response, err := http.Get(location)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || string(content) != "richmod-oss-smoke" {
		t.Fatalf("presigned response = %d %q %v", response.StatusCode, content, err)
	}
}
