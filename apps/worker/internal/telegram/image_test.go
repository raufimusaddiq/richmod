package telegram

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestNormalizeTelegramImageValidatesAndStripsInput(t *testing.T) {
	raw := testPNG(t)
	normalized, media, width, height, extension, err := normalizeTelegramImage(raw, "receipt.png")
	if err != nil {
		t.Fatal(err)
	}
	if media != "image/png" || width != 2 || height != 2 || extension != ".png" || len(normalized) == 0 {
		t.Fatalf("media=%s width=%d height=%d ext=%s", media, width, height, extension)
	}
	if _, _, _, _, _, err = normalizeTelegramImage(raw, "receipt.jpg"); err == nil {
		t.Fatal("mismatched extension accepted")
	}
	if _, _, _, _, _, err = normalizeTelegramImage([]byte("not an image"), "receipt.png"); err == nil {
		t.Fatal("invalid MIME accepted")
	}
}

func TestBotDownloadUsesBoundedTelegramFileFlow(t *testing.T) {
	raw := testPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"documents/receipt.png","file_size":80}}`))
		case strings.Contains(r.URL.Path, "/file/bottest-token/documents/receipt.png"):
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	bot := NewBot("test-token")
	bot.base = server.URL
	bot.http = server.Client()
	got, path, err := bot.Download(t.Context(), "file-id", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) || path != "documents/receipt.png" {
		t.Fatalf("path=%s bytes=%d", path, len(got))
	}
}

func TestBotDownloadRejectsOversizedMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"large.png","file_size":9999}}`))
	}))
	defer server.Close()
	bot := NewBot("test-token")
	bot.base = server.URL
	bot.http = server.Client()
	if _, _, err := bot.Download(t.Context(), "file-id", 100); err == nil {
		t.Fatal("oversized Telegram file accepted")
	}
}
