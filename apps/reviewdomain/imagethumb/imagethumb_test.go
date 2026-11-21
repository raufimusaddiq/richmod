package imagethumb

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestJPEGKeepsAspectRatioAndMaxEdge(t *testing.T) {
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
	thumb, err := JPEG(original.Bytes())
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
	if config.Width > MaxEdge || config.Height > MaxEdge {
		t.Fatalf("thumbnail %dx%d exceeds %dpx", config.Width, config.Height, MaxEdge)
	}
	if config.Width != 133 || config.Height != 200 {
		t.Fatalf("thumbnail %dx%d does not preserve the 2:3 aspect ratio", config.Width, config.Height)
	}
}

func TestJPEGRejectsUndecodableBytes(t *testing.T) {
	if _, err := JPEG([]byte("not an image")); err == nil {
		t.Fatal("expected undecodable bytes to fail so the caller can fall back")
	}
}
