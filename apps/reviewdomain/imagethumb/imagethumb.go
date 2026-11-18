// Package imagethumb renders the small grid preview a document card shows. The
// worker writes it at ingestion and the API falls back to it, so both share the
// one implementation and the output stays identical. Stdlib only: no resize
// dependency.
package imagethumb

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
)

// MaxEdge is the longest edge of a grid preview. The grid renders at 96x126 CSS
// px, so 200px stays sharp on 2x screens without shipping the page.
const MaxEdge = 200

// JPEG decodes an image and box-filters it down to MaxEdge on its longest edge,
// preserving aspect ratio.
func JPEG(raw []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("image has no pixels")
	}
	tw, th := width, height
	if width >= height {
		tw = min(width, MaxEdge)
		th = max(1, int(math.Round(float64(height)*float64(tw)/float64(width))))
	} else {
		th = min(height, MaxEdge)
		tw = max(1, int(math.Round(float64(width)*float64(th)/float64(height))))
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		y0, y1 := b.Min.Y+y*height/th, b.Min.Y+(y+1)*height/th
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < tw; x++ {
			x0, x1 := b.Min.X+x*width/tw, b.Min.X+(x+1)*width/tw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), uint8(a / n >> 8)})
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, dst, &jpeg.Options{Quality: 70}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
