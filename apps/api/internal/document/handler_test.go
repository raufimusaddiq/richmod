package document

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/api/internal/blob"
)

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, width, height))
	value.Set(0, 0, color.RGBA{R: 20, G: 80, B: 40, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestNormalizeImageValidatesAndStripsMetadata(t *testing.T) {
	raw := testPNG(t, 20, 10)
	normalized, mediaType, width, height, extension, err := normalizeImage(raw, "receipt.png")
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "image/png" || extension != ".png" || width != 20 || height != 10 || len(normalized) == 0 {
		t.Fatalf("unexpected normalized image: %s %s %dx%d %d", mediaType, extension, width, height, len(normalized))
	}
}

func TestNormalizeImageRejectsMismatchedExtension(t *testing.T) {
	if _, _, _, _, _, err := normalizeImage(testPNG(t, 10, 10), "receipt.jpg"); err == nil {
		t.Fatal("expected MIME and extension mismatch to fail")
	}
}

func TestReadUploadRejectsOversizedBody(t *testing.T) {
	if _, err := readUpload(bytes.NewReader(make([]byte, maxUploadBytes+1))); err == nil {
		t.Fatal("oversized upload was accepted")
	}
}

func TestThumbnailIsSmallerAndKeepsAspectRatio(t *testing.T) {
	// Noise defeats PNG compression, so the original is genuinely large.
	value := image.NewRGBA(image.Rect(0, 0, 600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 600; x++ {
			value.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 5), B: uint8((x * y) % 251), A: 255})
		}
	}
	var original bytes.Buffer
	if err := png.Encode(&original, value); err != nil {
		t.Fatal(err)
	}
	thumb, err := thumbnailJPEG(original.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(thumb) >= original.Len() {
		t.Fatalf("thumbnail %d bytes is not smaller than original %d bytes", len(thumb), original.Len())
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(thumb))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width > thumbnailMaxEdge || config.Height > thumbnailMaxEdge {
		t.Fatalf("thumbnail %dx%d exceeds %dpx", config.Width, config.Height, thumbnailMaxEdge)
	}
	if config.Width != 133 || config.Height != 200 {
		t.Fatalf("thumbnail %dx%d does not preserve the 2:3 aspect ratio", config.Width, config.Height)
	}
}

func TestThumbnailRejectsUndecodableBytes(t *testing.T) {
	if _, err := thumbnailJPEG([]byte("not an image")); err == nil {
		t.Fatal("expected undecodable bytes to fail so the caller can fall back")
	}
}

func TestMaliciousFilenameCannotControlStoragePath(t *testing.T) {
	normalized, _, _, _, extension, err := normalizeImage(testPNG(t, 10, 10), "../../outside.png")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	storage, err := blob.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{storage: storage}
	ref, err := handler.store("household-id", normalized, extension)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(ref))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stored file is not under the storage root: %v", err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || filepath.IsAbs(ref) || filepath.Base(path) == "outside.png" {
		t.Fatalf("unsafe storage result: ref=%q path=%q relative=%q", ref, path, relative)
	}
}
