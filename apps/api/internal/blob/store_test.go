package blob

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStoreRoundTripAndTraversalGuard(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Ref("household-1", ".png")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), ref, []byte("image"), "image/png"); err != nil {
		t.Fatal(err)
	}
	raw, err := store.Read(context.Background(), ref)
	if err != nil || string(raw) != "image" {
		t.Fatalf("read = %q, %v", raw, err)
	}
	if err := store.EvictLocal(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.root, filepath.FromSlash(ref))); !os.IsNotExist(err) {
		t.Fatalf("local cache still exists: %v", err)
	}
	outside := filepath.Join(filepath.Dir(store.root), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background(), "../outside"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestOSSConfigurationMustBeComplete(t *testing.T) {
	t.Setenv("OSS_ENDPOINT", "https://example.invalid")
	t.Setenv("OSS_ACCESS_KEY", "")
	t.Setenv("OSS_SECRET_KEY", "")
	t.Setenv("OSS_BUCKET", "")
	if _, err := NewFromEnv(t.TempDir()); err == nil {
		t.Fatal("partial OSS configuration accepted")
	}
}
