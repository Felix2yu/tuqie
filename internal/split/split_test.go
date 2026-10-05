package split

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"tuqie/internal/axis"
)

func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 40, A: 255})
		}
	}
	return img
}

func TestSliceCopiesExactBand(t *testing.T) {
	src := gradient(32, 100)
	got, err := Slice(src, axis.Y, 20, 45)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rect.Dx() != 32 || got.Rect.Dy() != 25 {
		t.Fatalf("size %v, want 32x25", got.Rect.Size())
	}
	for y := 0; y < 25; y++ {
		for x := 0; x < 32; x++ {
			if got.RGBAAt(x, y) != src.RGBAAt(x, y+20) {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got.RGBAAt(x, y), src.RGBAAt(x, y+20))
			}
		}
	}
}

func TestSliceCopiesExactColumn(t *testing.T) {
	src := gradient(100, 32)
	got, err := Slice(src, axis.X, 20, 45)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rect.Dx() != 25 || got.Rect.Dy() != 32 {
		t.Fatalf("size %v, want 25x32", got.Rect.Size())
	}
	for y := 0; y < 32; y++ {
		for x := 0; x < 25; x++ {
			if got.RGBAAt(x, y) != src.RGBAAt(x+20, y) {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got.RGBAAt(x, y), src.RGBAAt(x+20, y))
			}
		}
	}
}

func TestSliceClampsClientCoordinates(t *testing.T) {
	src := gradient(10, 50)
	if _, err := Slice(src, axis.Y, -100, 9999); err != nil {
		t.Fatal(err)
	}
	if _, err := Slice(src, axis.Y, 30, 30); err == nil {
		t.Fatal("empty band should be rejected")
	}
	if _, err := Slice(src, axis.X, 5, 5); err == nil {
		t.Fatal("empty column band should be rejected")
	}
	got, err := Slice(src, axis.X, -100, 9999)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rect.Dx() != 10 || got.Rect.Dy() != 50 {
		t.Fatalf("clamped column size %v, want 10x50", got.Rect.Size())
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	src := gradient(24, 40)
	for _, format := range []string{FormatPNG, FormatJPEG} {
		var buf bytes.Buffer
		if err := Encode(&buf, src, format, 90); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		dec, err := png.Decode(bytes.NewReader(buf.Bytes()))
		if format == FormatJPEG {
			dec, err = jpeg.Decode(bytes.NewReader(buf.Bytes()))
		}
		if err != nil {
			t.Fatalf("%s decode: %v", format, err)
		}
		if s := dec.Bounds().Size(); s.X != 24 || s.Y != 40 {
			t.Fatalf("%s size %v", format, s)
		}
	}
	if err := Encode(&bytes.Buffer{}, src, "tiff", 90); err == nil {
		t.Fatal("unknown format should error")
	}
}
