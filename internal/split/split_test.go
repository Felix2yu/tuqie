package split

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"tuqie/internal/axis"
	"tuqie/internal/exif"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jxl"
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
	for _, format := range []string{FormatPNG, FormatJPEG, FormatHEIC, FormatAVIF, FormatJXL} {
		var buf bytes.Buffer
		if err := Encode(&buf, src, format, 90, exif.Date{}); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		dec, err := decode(format, &buf)
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

// decode reads a slice back with the decoder that belongs to its format, so a
// round trip never rests on the encoder's own word for what it wrote.
func decode(format string, r io.Reader) (image.Image, error) {
	switch format {
	case FormatPNG:
		return png.Decode(r)
	case FormatJPEG:
		return jpeg.Decode(r)
	case FormatHEIC:
		return heic.Decode(r)
	case FormatAVIF:
		return avif.Decode(r)
	}
	return jxl.Decode(r)
}

func TestEncodeCarriesTheDateIntoEveryContainer(t *testing.T) {
	// HEIC's encoder takes metadata; AVIF's and JXL's do not, so their date is
	// written into the container afterwards, which is precisely the part that can
	// quietly produce a file nobody else can read.
	src := gradient(20, 30)
	taken := exif.Date{Wall: "2026:10:07 09:15:30"}

	var heicBuf, avifBuf, jxlBuf bytes.Buffer
	for _, tc := range []struct {
		format string
		buf    *bytes.Buffer
		read   func(io.Reader) (string, error)
	}{
		{FormatHEIC, &heicBuf, func(r io.Reader) (string, error) {
			e, err := heic.DecodeExif(r)
			if err != nil {
				return "", err
			}
			return e.DateTimeOriginal, nil
		}},
		{FormatAVIF, &avifBuf, func(r io.Reader) (string, error) {
			e, err := avif.DecodeExif(r)
			if err != nil {
				return "", err
			}
			return e.DateTimeOriginal, nil
		}},
		{FormatJXL, &jxlBuf, func(r io.Reader) (string, error) {
			e, err := jxl.DecodeExif(r)
			if err != nil {
				return "", err
			}
			return e.DateTimeOriginal, nil
		}},
	} {
		if err := Encode(tc.buf, src, tc.format, 90, taken); err != nil {
			t.Fatalf("%s: %v", tc.format, err)
		}
		got, err := tc.read(bytes.NewReader(tc.buf.Bytes()))
		if err != nil {
			t.Errorf("%s: no Exif came back: %v", tc.format, err)
			continue
		}
		if got != taken.Wall {
			t.Errorf("%s date read back = %q, want %q", tc.format, got, taken.Wall)
		}
	}

	// With nothing to say, nothing gets written: the plain encoder's bytes stand.
	var bare bytes.Buffer
	if err := Encode(&bare, src, FormatAVIF, 90, exif.Date{}); err != nil {
		t.Fatal(err)
	}
	if _, err := avif.DecodeExif(bytes.NewReader(bare.Bytes())); err == nil {
		t.Error("an undated AVIF should not carry an Exif item")
	}
}

func TestEncoderQualitySpreadsTheSliderAcrossTheScales(t *testing.T) {
	// The slider means "how it should look", and each codec's quality number means
	// something else; the table below is the mapping measured against JPEG.
	cases := []struct {
		format       string
		slider, want int
	}{
		{FormatJPEG, 90, 90},
		{FormatPNG, 90, 90},
		{FormatHEIC, 60, 38},
		{FormatHEIC, 65, 41},
		{FormatHEIC, 90, 60},
		{FormatHEIC, 100, 83},
		{FormatAVIF, 90, 76},
		{FormatJXL, 90, 89},
		{FormatJXL, 100, 96},
		{FormatHEIC, 0, 38},
		{FormatHEIC, 130, 83},
	}
	for _, tc := range cases {
		if got := encoderQuality(tc.format, tc.slider); got != tc.want {
			t.Errorf("%s at slider %d: got %d, want %d", tc.format, tc.slider, got, tc.want)
		}
	}

	for _, format := range []string{FormatHEIC, FormatAVIF, FormatJXL} {
		last := -1
		for slider := 60; slider <= 100; slider++ {
			q := encoderQuality(format, slider)
			if q < last {
				t.Fatalf("%s: quality went backwards from %d to %d at slider %d", format, last, q, slider)
			}
			last = q
		}
	}
}

func TestAvifWithDateKeepsThePictureInRange(t *testing.T) {
	// The rewrite moves where the picture's bytes start; a wrong shift costs the
	// image rather than the date, so every extent has to land inside the file.
	var buf bytes.Buffer
	src := gradient(64, 48)
	if err := Encode(&buf, src, FormatAVIF, 90, exif.Date{Wall: "2026:10:07 09:15:30"}); err != nil {
		t.Fatal(err)
	}
	file := buf.Bytes()

	var meta []byte
	for _, b := range splitBoxes(file) {
		if b.name == "meta" {
			meta = b.body[4:]
		}
	}
	if meta == nil {
		t.Fatal("no meta box")
	}
	var items []ilocItem
	for _, c := range splitBoxes(meta) {
		if c.name == "iloc" {
			var ok bool
			if items, _, ok = readIloc(c.body); !ok {
				t.Fatal("iloc no longer parses")
			}
		}
	}
	if len(items) != 2 {
		t.Fatalf("iloc names %d items, want the picture and its Exif", len(items))
	}
	for _, it := range items {
		if it.pos+it.length > uint64(len(file)) {
			t.Errorf("item %d runs off the end: %d + %d > %d", it.id, it.pos, it.length, len(file))
		}
	}
	if _, err := avif.Decode(bytes.NewReader(file)); err != nil {
		t.Errorf("dated AVIF no longer decodes: %v", err)
	}

	// A file that is not the encoder's own shape comes back untouched.
	junk := []byte("not an avif at all, but long enough to have a header")
	if got := avifWithDate(junk, []byte("tiff")); !bytes.Equal(got, junk) {
		t.Error("avifWithDate rewrote a file it should have left alone")
	}
}

func TestHeicOfAnOddSliceKeepsItsSize(t *testing.T) {
	for _, size := range [][2]int{{1080, 921}, {1081, 900}, {1080, 922}, {30, 1080}} {
		var buf bytes.Buffer
		if err := Encode(&buf, gradient(size[0], size[1]), FormatHEIC, 90, exif.Date{}); err != nil {
			t.Fatalf("%dx%d: %v", size[0], size[1], err)
		}
		if bytes.Contains(buf.Bytes(), []byte("clap")) {
			t.Errorf("%dx%d: coded picture had to be padded, so the file disagrees with its own size", size[0], size[1])
		}
		dec, err := heic.Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("%dx%d decode: %v", size[0], size[1], err)
		}
		if got := dec.Bounds().Size(); got.X != size[0] || got.Y != size[1] {
			t.Errorf("%dx%d came back %v", size[0], size[1], got)
		}
	}
}

func TestEncodeSizesTinyAndThinSlices(t *testing.T) {
	// A cut near the edge of the image leaves a band a few pixels tall, and a
	// codec that cannot code that is a failure path the user would hit first.
	for _, size := range [][2]int{{1, 1}, {8, 4}, {30, 1080}} {
		src := gradient(size[0], size[1])
		for _, format := range []string{FormatHEIC, FormatAVIF, FormatJXL} {
			var buf bytes.Buffer
			if err := Encode(&buf, src, format, 90, exif.Date{}); err != nil {
				t.Fatalf("%s at %dx%d: %v", format, size[0], size[1], err)
			}
			dec, err := decode(format, bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("%s at %dx%d decode: %v", format, size[0], size[1], err)
			}
			if got := dec.Bounds().Size(); got.X != size[0] || got.Y != size[1] {
				t.Errorf("%s at %dx%d came back %v", format, size[0], size[1], got)
			}
		}
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

	// PNG keeps the same date in an eXIf chunk, so both formats a slice can be
	// written in report when the shot happened.
	var pngBuf bytes.Buffer
	if err := Encode(&pngBuf, src, FormatPNG, 90, taken); err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(pngBuf.Bytes())); err != nil {
		t.Fatalf("dated PNG no longer decodes: %v", err)
	}
	if got := exif.Scan(pngBuf.Bytes()); got != taken {
		t.Fatalf("PNG date read back = %+v, want %+v", got, taken)
	}

	// Undated input gets no chunk, so the bytes match the plain encoder exactly.
	var undatedPNG, barePNG bytes.Buffer
	if err := Encode(&undatedPNG, plain, FormatPNG, 90, exif.Date{}); err != nil {
		t.Fatal(err)
	}
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&barePNG, plain); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(undatedPNG.Bytes(), barePNG.Bytes()) {
		t.Fatal("empty date should not add metadata to PNG either")
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
		FormatHEIC: "heic",
		FormatAVIF: "avif",
		FormatJXL:  "jxl",
		// A format the server never names still gets the suffix it would
		// actually have written.
		"tiff": "jpg",
		"":     "jpg",
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
