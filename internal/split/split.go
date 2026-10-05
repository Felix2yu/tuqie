// Package split crops bands out of a long screenshot and encodes them.
package split

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"tuqie/internal/axis"
)

const (
	FormatPNG  = "png"
	FormatJPEG = "jpeg"
)

// Slice returns the band [from, to) along ax of src as a new RGBA image. src must
// have its origin at (0,0) — store guarantees that. Cut positions come from the
// client, so they are clamped rather than trusted.
func Slice(src *image.RGBA, ax axis.Axis, from, to int) (*image.RGBA, error) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	length := ax.Length(w, h)
	lo := clamp(from, 0, length)
	hi := clamp(to, lo, length)
	if hi-lo < 1 {
		return nil, fmt.Errorf("empty slice %d..%d", from, to)
	}
	if ax == axis.X {
		out := image.NewRGBA(image.Rect(0, 0, hi-lo, h))
		for y := 0; y < h; y++ {
			s := y*src.Stride + lo*4
			d := y * out.Stride
			copy(out.Pix[d:d+(hi-lo)*4], src.Pix[s:s+(hi-lo)*4])
		}
		return out, nil
	}
	out := image.NewRGBA(image.Rect(0, 0, w, hi-lo))
	for y := 0; y < hi-lo; y++ {
		s := (y + lo) * src.Stride
		d := y * out.Stride
		copy(out.Pix[d:d+w*4], src.Pix[s:s+w*4])
	}
	return out, nil
}

// Encode writes img in the requested format.
func Encode(w io.Writer, img image.Image, format string, quality int) error {
	switch format {
	case FormatJPEG:
		if quality <= 0 || quality > 100 {
			quality = 92
		}
		return jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
	case FormatPNG:
		enc := &png.Encoder{CompressionLevel: png.BestCompression}
		return enc.Encode(w, img)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func Ext(format string) string {
	if format == FormatJPEG {
		return "jpg"
	}
	return "png"
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
