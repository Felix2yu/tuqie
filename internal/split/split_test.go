package split

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"tuqie/internal/axis"
	"tuqie/internal/exif"
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
		if err := Encode(&buf, src, format, 90, exif.Date{}); err != nil {
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
	if err := Encode(&bytes.Buffer{}, src, "tiff", 90, exif.Date{}); err == nil {
		t.Fatal("unknown format should error")
	}
}

func TestEncodeCarriesCaptureDate(t *testing.T) {
	// The album importer reads the shoot time out of the image bytes, so a slice has
	// to keep the source date instead of reporting the moment it was cut.
	src := gradient(20, 30)
	taken := exif.Date{Wall: "2026:10:07 09:15:30", Subsec: "482", Offset: "+08:00"}

	var buf bytes.Buffer
	if err := Encode(&buf, src, FormatJPEG, 90, taken); err != nil {
		t.Fatal(err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("dated JPEG no longer decodes: %v", err)
	}
	if got := exif.Scan(buf.Bytes()); got != taken {
		t.Fatalf("date read back = %+v, want %+v", got, taken)
	}

	// Without a source date nothing is attached, so output stays byte-identical to
	// the plain encoder.
	plain := gradient(20, 30)
	var undated, bare bytes.Buffer
	if err := Encode(&undated, plain, FormatJPEG, 90, exif.Date{}); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&bare, plain, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(undated.Bytes(), bare.Bytes()) {
		t.Fatal("empty date should not add metadata")
	}

	// PNG has no date field readers agree on; the date is dropped, not faked.
	var pngBuf bytes.Buffer
	if err := Encode(&pngBuf, src, FormatPNG, 90, taken); err != nil {
		t.Fatal(err)
	}
	if d := exif.Scan(pngBuf.Bytes()); d.Valid() {
		t.Fatalf("PNG picked up %+v", d)
	}
}

func TestEncodeFallsBackToDefaultQuality(t *testing.T) {
	// The client owns the quality query parameter, so out-of-range values must
	// still produce a sane JPEG rather than a 0-quality failure.
	src := gradient(16, 24)
	for _, q := range []int{0, -1, 101, 1000} {
		var buf bytes.Buffer
		if err := Encode(&buf, src, FormatJPEG, q, exif.Date{}); err != nil {
			t.Fatalf("quality %d: %v", q, err)
		}
		if _, err := jpeg.Decode(&buf); err != nil {
			t.Fatalf("quality %d decode: %v", q, err)
		}
	}
	var ref bytes.Buffer
	if err := Encode(&ref, src, FormatJPEG, 92, exif.Date{}); err != nil {
		t.Fatal(err)
	}
	var clamped bytes.Buffer
	if err := Encode(&clamped, src, FormatJPEG, 0, exif.Date{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ref.Bytes(), clamped.Bytes()) {
		t.Fatal("out-of-range quality should be replaced by the 92 default")
	}
}

func TestExt(t *testing.T) {
	cases := map[string]string{
		FormatJPEG: "jpg",
		FormatPNG:  "png",
		"tiff":     "png",
		"":         "png",
	}
	for format, want := range cases {
		if got := Ext(format); got != want {
			t.Fatalf("Ext(%q) = %q, want %q", format, got, want)
		}
	}
}

func TestClamp(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{11, 0, 10, 10},
	}
	for _, c := range cases {
		if got := clamp(c.v, c.lo, c.hi); got != c.want {
			t.Fatalf("clamp(%d,%d,%d) = %d, want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}
