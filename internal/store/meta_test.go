package store

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tuqie/internal/exif"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jxl"
)

// swatch is a small picture with a clear top and bottom, 60x40 so a quarter turn
// cannot be mistaken for the original shape.
func swatch() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 60, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 60; x++ {
			c := color.RGBA{30, 60, 90, 255}
			if y < 20 {
				c = color.RGBA{240, 240, 240, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encode(t *testing.T, name string, exif []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	var err error
	switch name {
	case "heic":
		err = heic.Encode(&buf, swatch(), heic.EncodeOptions{Exif: exif})
	case "avif":
		err = avif.Encode(&buf, swatch(), avif.Options{Quality: 60})
	case "jxl":
		err = jxl.Encode(&buf, swatch())
	default:
		t.Fatalf("no encoder for %s", name)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
	return buf.Bytes()
}

// tiffOrientation is a minimal big-endian TIFF carrying just tag 0x0112, which is
// how a camera says the pixels it wrote need a quarter turn.
func tiffOrientation(value int) []byte {
	return []byte{
		'M', 'M', 0x00, 0x2a, 0x00, 0x00, 0x00, 0x08,
		0x00, 0x01, // one entry
		0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x06, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, // no next IFD
	}
}

func TestPutAcceptsContainerFormats(t *testing.T) {
	for _, format := range []struct {
		name, ext, mime string
	}{
		{"heic", ".heic", "image/heic"},
		{"avif", ".avif", "image/avif"},
		{"jxl", ".jxl", "image/jxl"},
	} {
		t.Run(format.name, func(t *testing.T) {
			s := newStore(t, time.Hour)
			p := put(t, s, encode(t, format.name, nil), "shot"+format.ext)

			if p.Mime != format.mime {
				t.Fatalf("mime %q, want %q", p.Mime, format.mime)
			}
			if p.Width != 60 || p.Height != 40 {
				t.Fatalf("dims %dx%d, want 60x40", p.Width, p.Height)
			}
			img, err := p.Image()
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := img.Bounds().Dx(); got != 60 {
				t.Fatalf("decoded width %d, want 60", got)
			}
		})
	}
}

func TestPutReadsOrientationOutOfAContainer(t *testing.T) {
	s := newStore(t, time.Hour)
	p := put(t, s, encode(t, "heic", tiffOrientation(6)), "photo.heic")

	if p.Orientation != 6 {
		t.Fatalf("orientation %d, want 6", p.Orientation)
	}
	// The stored picture is 60x40 lying on its side; what the caller hears about
	// is the upright one every viewer will show.
	if p.Width != 40 || p.Height != 60 {
		t.Fatalf("dims %dx%d, want 40x60", p.Width, p.Height)
	}
	img, err := p.Image()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 60 {
		t.Fatalf("pixels %dx%d, want 40x60", b.Dx(), b.Dy())
	}
	// Orientation 6 stands the stored picture up with its top edge to the right,
	// so the light band that was the top half should now fill the right half. The
	// colour itself only has to read light or dark: the codec rounds it a little.
	for _, tc := range []struct {
		x, y  int
		light bool
	}{
		{2, 30, false},
		{38, 30, true},
	} {
		r, _, _, _ := img.At(tc.x, tc.y).RGBA()
		if got := r>>8 > 0x80; got != tc.light {
			t.Errorf("(%d,%d) reads %s, want %s", tc.x, tc.y, lit(got), lit(tc.light))
		}
	}
}

func lit(b bool) string {
	if b {
		return "light"
	}
	return "dark"
}

func TestContainerMetaStaysQuietOnFilesItCannotRead(t *testing.T) {
	dir := t.TempDir()

	if got := containerMeta("heic", filepath.Join(dir, "absent.heic")); got != (exif.Doc{}) {
		t.Fatalf("missing file gave %+v, want nothing", got)
	}

	// A container cut short keeps its upload; it just reports no metadata, so the
	// picture is taken upright and the uploader's timestamp stands in for a date.
	path := filepath.Join(dir, "half.heic")
	data := encode(t, "heic", nil)
	if err := os.WriteFile(path, data[:len(data)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if got := containerMeta("heic", path); got.Orientation != 0 || got.Taken.Valid() {
		t.Fatalf("truncated file gave %+v, want nothing", got)
	}
}

func TestDocKeepsOnlyWhatItCanTrust(t *testing.T) {
	const stamp = "2026:10:07 14:23:45"
	if got := doc(0, stamp); got.Orientation != 0 {
		t.Fatalf("orientation %d, want none for a tag outside 1..8", got.Orientation)
	}
	if got := doc(9, stamp); got.Orientation != 0 {
		t.Fatalf("orientation %d, want none above eight", got.Orientation)
	}
	if got := doc(3, "yesterday", stamp); !strings.HasPrefix(got.Taken.Wall, "2026:10:07") {
		t.Fatalf("taken %q, want the one value that parses", got.Taken.Wall)
	}
	if got := doc(1, "", ""); got.Taken.Valid() {
		t.Fatal("an empty date should stay unset")
	}
}
