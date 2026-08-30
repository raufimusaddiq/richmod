package blob

import (
	"context"
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
}
