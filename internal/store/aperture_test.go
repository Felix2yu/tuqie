package store

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The container headers built below always describe the same picture: a
// 1080x3620 item, which is a real coded size with one row of it padding.

func box(name string, body ...[]byte) []byte {
	size := 8
	for _, part := range body {
		size += len(part)
	}
	var b bytes.Buffer
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[:4], uint32(size))
	copy(hdr[4:], name)
	b.Write(hdr[:])
	for _, part := range body {
		b.Write(part)
	}
	return b.Bytes()
}

// box64 is a box whose size does not fit in the four bytes at the front.
func box64(name string, body ...[]byte) []byte {
	size := 16
	for _, part := range body {
		size += len(part)
	}
	head := append(u32(1), []byte(name)...)
	head = append(head, u32(uint32(size>>32))...)
	head = append(head, u32(uint32(size))...)
	var b bytes.Buffer
	b.Write(head)
	for _, part := range body {
		b.Write(part)
	}
	return b.Bytes()
}

// boxSized writes only a header, for the sizes a broken file can claim.
func boxSized(name string, size uint32) []byte {
	return append(u32(size), []byte(name)...)
}

// fullBox is a box whose payload starts with a version byte.
func fullBox(name string, version byte, body ...[]byte) []byte {
	return box(name, append([][]byte{{version, 0, 0, 0}}, body...)...)
}

func u32(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

func u16(v uint16) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return b[:]
}

// metaOf is the inside of a meta box: primary is the item a viewer shows, and
// assoc is the item the property list belongs to, which is not always the same
// one. props come after the size property every container carries.
func metaOf(primary, assoc uint32, props ...[]byte) []byte {
	all := append([][]byte{fullBox("ispe", 0, u32(1080), u32(3620))}, props...)
	out := []byte{0, 0, 0, 0}
	if primary != 0 {
		out = append(out, fullBox("pitm", 0, u16(uint16(primary)))...)
	}

	entry := u32(0)
	if assoc != 0 {
		entry = append(u32(1), u16(uint16(assoc))...)
		entry = append(entry, byte(len(all)))
		for i := range all {
			entry = append(entry, byte(i+1))
		}
	}
	out = append(out, box("iprp",
		box("ipco", all...),
		fullBox("ipma", 0, entry),
	)...)
	return out
}

// clapBox selects a rectangle of a coded picture. The box places the aperture by
// its centre, so an offset next to an odd size needs a denominator of two.
func clapBox(w, h, x, y, cw, ch int) []byte {
	return box("clap",
		u32(uint32(cw)), u32(1),
		u32(uint32(ch)), u32(1),
		u32(uint32(2*x+cw-w)), u32(2),
		u32(uint32(2*y+ch-h)), u32(2),
	)
}

func turn(angle byte) []byte { return box("irot", []byte{angle}) }

// oneRowOff is the aperture of the 1080x3620 picture these tests write: all of
// it but the padded bottom row.
var oneRowOff = clapBox(1080, 3620, 0, 0, 1080, 3619)

func TestDisplayReadsWhatTheContainerAsksFor(t *testing.T) {
	d := metaDisplay(metaOf(1, 1, oneRowOff, turn(3)))
	if d.orientation != 6 {
		t.Errorf("orientation: got %d, want 6 for three quarter turns anticlockwise", d.orientation)
	}
	if want := image.Rect(0, 0, 1080, 3619); d.crop != want {
		t.Errorf("crop: got %v, want %v", d.crop, want)
	}

	for angle, want := range map[byte]uint16{0: 1, 1: 8, 2: 3} {
		if d := metaDisplay(metaOf(1, 1, turn(angle))); d.orientation != want {
			t.Errorf("angle %d: got %d, want %d", angle, d.orientation, want)
		}
	}
}

func TestDisplayKeepsQuietAboutWhatItCannotRead(t *testing.T) {
	for name, meta := range map[string][]byte{
		"no primary item":           metaOf(0, 1, oneRowOff, turn(3)),
		"association names another": metaOf(1, 2, oneRowOff, turn(3)),
		"no association at all":     metaOf(1, 0, oneRowOff, turn(3)),
		"truncated":                 {0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := metaDisplay(meta); got != (display{}) {
				t.Errorf("got %+v, want nothing", got)
			}
		})
	}

	t.Run("aperture larger than the picture", func(t *testing.T) {
		d := metaDisplay(metaOf(1, 1, clapBox(1080, 3620, 0, 0, 2000, 3619), turn(3)))
		if !d.crop.Empty() {
			t.Errorf("crop: got %v, want none", d.crop)
		}
		if d.orientation != 6 {
			t.Errorf("orientation: got %d, want the turn to count on its own", d.orientation)
		}
	})
	t.Run("aperture on a half pixel", func(t *testing.T) {
		half := box("clap", u32(1080), u32(1), u32(3619), u32(1), u32(0), u32(1), u32(1), u32(3))
		if d := metaDisplay(metaOf(1, 1, half, turn(3))); !d.crop.Empty() {
			t.Errorf("crop: got %v, want none", d.crop)
		}
	})
	t.Run("mirrored upload keeps the EXIF turn", func(t *testing.T) {
		d := metaDisplay(metaOf(1, 1, oneRowOff, turn(3), box("imir", []byte{0})))
		if d.orientation != 0 {
			t.Errorf("orientation: got %d, want none for a file the four turns do not describe", d.orientation)
		}
		if d.crop != image.Rect(0, 0, 1080, 3619) {
			t.Errorf("crop: got %v, want the aperture to still count", d.crop)
		}
	})
}

func TestDisplayForReadsAFileAndIgnoresTheRest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(path, []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := displayFor("png", path); got != (display{}) {
		t.Errorf("png: got %+v", got)
	}
	if got := displayFor("heic", filepath.Join(dir, "missing.heic")); got != (display{}) {
		t.Errorf("missing file: got %+v", got)
	}

	header := box("ftyp", []byte("mif1"))
	header = append(header, box("meta", metaOf(1, 1, clapBox(1080, 3620, 0, 1, 1080, 3619), turn(2)))...)
	header = append(header, box("mdat", []byte("the pixels follow"))...)
	if err := os.WriteFile(path, header, 0o600); err != nil {
		t.Fatal(err)
	}

	got := displayFor("heic", path)
	if got.orientation != 3 {
		t.Errorf("orientation: got %d, want 3", got.orientation)
	}
	if want := image.Rect(0, 1, 1080, 3620); got.crop != want {
		t.Errorf("crop: got %v, want %v", got.crop, want)
	}
}

// TestDisplayReadsWideItemNumbers covers the files that number their items
// past what a sixteen-bit slot holds: the primary item and its properties sit in
// wider fields there.
func TestDisplayReadsWideItemNumbers(t *testing.T) {
	ipco := box("ipco", fullBox("ispe", 0, u32(1080), u32(3620)), turn(1))
	// ipma version 1 with the 16-bit index flag set, naming item 70000.
	ipma := box("ipma", []byte{1, 0, 0, 1}, u32(1), u32(70000), []byte{2}, u16(1), u16(2))
	meta := append([]byte{0, 0, 0, 0}, fullBox("pitm", 1, u32(70000))...)
	meta = append(meta, box("iprp", ipco, ipma)...)

	if d := metaDisplay(meta); d.orientation != 8 {
		t.Errorf("got %+v, want the turn to reach us from a wide item number", d)
	}
}

func TestBoxPayloadReadsTheSizesAFileClaims(t *testing.T) {
	// boxSized writes only a header, for the sizes a hostile file can claim.
	boxSized := func(name string, size uint32) []byte {
		head := u32(size)
		return append(head, []byte(name)...)
	}

	t.Run("sixty-four bit size", func(t *testing.T) {
		file := append(box("ftyp", []byte("mif1")), box64("meta", metaOf(1, 1, turn(3)))...)
		file = append(file, box("mdat", []byte("the pixels follow"))...)
		payload, err := boxPayload(bytes.NewReader(file), "meta")
		if err != nil {
			t.Fatal(err)
		}
		if d := metaDisplay(payload); d.orientation != 6 {
			t.Errorf("got %+v, want the turn out of a large box", d)
		}
	})
	t.Run("a box that runs to the end of the file", func(t *testing.T) {
		file := append(boxSized("uuid", 0), []byte("everything, apparently")...)
		file = append(file, box("meta", metaOf(1, 1, turn(3)))...)
		if _, err := boxPayload(bytes.NewReader(file), "meta"); err == nil {
			t.Error("a box claiming the rest of the file leaves nothing to search")
		}
	})
	t.Run("a header claiming more than metadata weighs", func(t *testing.T) {
		file := append(boxSized("meta", maxMeta+64), []byte("short")...)
		if _, err := boxPayload(bytes.NewReader(file), "meta"); err == nil {
			t.Error("got no error, want the read refused before the allocation")
		}
	})
	t.Run("a size shorter than its own header", func(t *testing.T) {
		file := append(boxSized("junk", 4), []byte("no")...)
		file = append(file, box("meta", metaOf(1, 1, turn(3)))...)
		if _, err := boxPayload(bytes.NewReader(file), "meta"); err == nil {
			t.Error("got no error, want a broken header to stop the walk")
		}
	})
}

func TestCropToKeepsOnlyThePicture(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := range 3 {
		for x := range 4 {
			src.Set(x, y, tag(x, y))
		}
	}

	t.Run("nothing to do", func(t *testing.T) {
		for _, r := range []image.Rectangle{{}, image.Rect(0, 0, 4, 3), image.Rect(2, 2, 9, 9)} {
			if got := cropTo(src, r); got != src {
				t.Errorf("crop %v: got a copy, want the buffer back", r)
			}
		}
	})
	t.Run("the padding goes", func(t *testing.T) {
		got := cropTo(src, image.Rect(1, 0, 4, 2))
		if want := image.Rect(0, 0, 3, 2); got.Bounds() != want {
			t.Fatalf("bounds: got %v, want %v", got.Bounds(), want)
		}
		if got.At(0, 0) != src.At(1, 0) || got.At(2, 1) != src.At(3, 1) {
			t.Error("the cropped pixels did not come from the aperture")
		}
	})
}

func TestEachBoxStopsWhereTheFileStopsMakingSense(t *testing.T) {
	names := func(data []byte) (got []string) {
		eachBox(data, func(name string, _ []byte) { got = append(got, name) })
		return got
	}

	t.Run("a box taking the rest of the container", func(t *testing.T) {
		data := append(box("box1", []byte("x")), box64("box2", []byte("y"))...)
		data = append(data, boxSized("tail", 0)...)
		data = append(data, []byte("and everything after")...)
		if want := []string{"box1", "box2", "tail"}; !slices.Equal(names(data), want) {
			t.Errorf("got %v, want %v", names(data), want)
		}
	})
	t.Run("a child bigger than the container", func(t *testing.T) {
		data := append(box("box1", []byte("x")), boxSized("huge", 1<<20)...)
		data = append(data, []byte("junk")...)
		if want := []string{"box1"}; !slices.Equal(names(data), want) {
			t.Errorf("got %v, want %v", names(data), want)
		}
	})
	t.Run("a header with no room for its own size", func(t *testing.T) {
		data := append(box("box1", []byte("x")), u32(1)...)
		data = append(data, []byte("next")...)
		if want := []string{"box1"}; !slices.Equal(names(data), want) {
			t.Errorf("got %v, want %v", names(data), want)
		}
	})
}
