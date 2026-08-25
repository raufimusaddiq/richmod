package document

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
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

func TestMaliciousFilenameCannotControlStoragePath(t *testing.T) {
	normalized, _, _, _, extension, err := normalizeImage(testPNG(t, 10, 10), "../../outside.png")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	handler := &Handler{root: root}
	ref, path, err := handler.store("household-id", normalized, extension)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || filepath.IsAbs(ref) || filepath.Base(path) == "outside.png" {
		t.Fatalf("unsafe storage result: ref=%q path=%q relative=%q", ref, path, relative)
	}
}
