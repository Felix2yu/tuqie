package exif

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"
)

// ifdTag is one IFD record in the hand-built fixtures.
type ifdTag struct {
	tag     uint16
	text    string // ASCII value; values that fit in 4 bytes go inline, as cameras write them
	long    uint32 // LONG value, unless isRef
	short   uint16 // SHORT value when isShort
	ref     int    // index of the IFD to point at when isRef
	isRef   bool
	isShort bool
}

// buildTIFF writes a header followed by the IFDs in order, chaining each to the
// next, with a string pool past all of them.
func buildTIFF(t *testing.T, order binary.ByteOrder, ifds ...[]ifdTag) []byte {
	t.Helper()
	sizes := make([]int, len(ifds))
	offs := make([]int, len(ifds))
	pos := 8
	for i, ifd := range ifds {
		sizes[i] = 2 + 12*len(ifd) + 4
		offs[i] = pos
		pos += sizes[i]
	}
	pool := pos

	valOff := make([][]int, len(ifds))
	for i, ifd := range ifds {
		valOff[i] = make([]int, len(ifd))
		for j, e := range ifd {
			valOff[i][j] = -1
			if e.text != "" && len(e.text)+1 > 4 {
				valOff[i][j] = pool
				pool += len(e.text) + 1
			}
		}
	}

	var buf bytes.Buffer
	if order == binary.BigEndian {
		buf.Write([]byte{'M', 'M', 0x2a, 0x00})
	} else {
		buf.Write([]byte{'I', 'I', 0x2a, 0x00})
	}
	u32 := func(v uint32) {
		var b [4]byte
		order.PutUint32(b[:], v)
		buf.Write(b[:])
	}
	u16 := func(v uint16) {
		var b [2]byte
		order.PutUint16(b[:], v)
		buf.Write(b[:])
	}
	u32(uint32(offs[0]))

	for i, ifd := range ifds {
		u16(uint16(len(ifd)))
		for j, e := range ifd {
			u16(e.tag)
			switch {
			case e.text != "":
				u16(typeASCII)
				u32(uint32(len(e.text) + 1))
				if valOff[i][j] >= 0 {
					u32(uint32(valOff[i][j]))
				} else {
					padded := append(append([]byte{}, e.text...), 0, 0, 0, 0)
					buf.Write(padded[:4])
				}
			case e.isRef:
				if e.ref < 0 || e.ref >= len(ifds) {
					t.Fatalf("tag %x points at IFD %d of %d", e.tag, e.ref, len(ifds))
				}
				u16(typeLong)
				u32(1)
				u32(uint32(offs[e.ref]))
			case e.isShort:
				u16(typeShort)
				u32(1)
				b := make([]byte, 4)
				order.PutUint16(b, e.short)
				buf.Write(b)
			default:
				u16(typeLong)
				u32(1)
				u32(e.long)
			}
		}
		next := uint32(0)
		if i+1 < len(ifds) {
			next = uint32(offs[i+1])
		}
		u32(next)
	}

	for i, ifd := range ifds {
		for j, e := range ifd {
			if valOff[i][j] >= 0 {
				buf.WriteString(e.text)
				buf.WriteByte(0)
			}
		}
	}
	return buf.Bytes()
}

// framed returns a complete JPEG marker segment: marker byte, length, body.
func framed(marker byte, body []byte) []byte {
	size := len(body) + 2
	out := []byte{0xff, marker, byte(size >> 8), byte(size)}
	return append(out, body...)
}

// jpegFile assembles a JPEG: SOI, each already-framed segment, EOI.
func jpegFile(parts ...[]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 0xd8})
	for _, p := range parts {
		buf.Write(p)
	}
	buf.Write([]byte{0xff, 0xd9})
	return buf.Bytes()
}

// exifApp1 frames a TIFF as the APP1 segment readers look for. Date.App1 does the
// same job for the writer's own output.
func exifApp1(tiff []byte) []byte {
	return framed(0xe1, append([]byte("Exif\x00\x00"), tiff...))
}

func jfif() []byte {
	return framed(0xe0, []byte("JFIF\x00\x01\x02\x00\x00\x01\x00\x01\x00\x00"))
}

func solid(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	return img
}

func stdlibJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solid(8, 8), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func stdlibPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solid(4, 4)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// cameraJPEG is a hand-made file in the shapes readers hit in the wild: big endian,
// an XMP APP1 ahead of the Exif one, a GPS IFD, a thumbnail IFD whose offsets point
// beyond the buffer, and an IFD0 DateTime that must lose to the sub-IFD's
// DateTimeOriginal.
func cameraJPEG(t *testing.T) []byte {
	t.Helper()
	tiff := buildTIFF(t, binary.BigEndian,
		[]ifdTag{ // IFD0
			{tag: 0x010f, text: "Apple iPhone 15"},
			{tag: tagDateTime, text: "1999:01:01 00:00:00"},
			{tag: 0x8825, isRef: true, ref: 2},
			{tag: tagExifIFD, isRef: true, ref: 1},
		},
		[]ifdTag{ // Exif sub-IFD
			{tag: tagDateDigitized, text: "2026:10:07 09:15:31"},
			{tag: tagDateOriginal, text: "2026:10:07 09:15:30"},
			{tag: tagOffsetOriginal, text: "+08:00"},
			{tag: tagSubSecOrig, text: "482"},
		},
		[]ifdTag{ // GPS IFD, must not be read as a date source
			{tag: tagDateTime, text: "1800:01:01 00:00:00"},
		},
		[]ifdTag{ // thumbnail, pointing past the end of the buffer
			{tag: 0x0201, long: 1 << 20},
			{tag: 0x0202, long: 4096},
		},
	)
	return jpegFile(
		jfif(),
		framed(0xe1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta/>")),
		exifApp1(tiff),
	)
}

func TestScanReadsDateOriginal(t *testing.T) {
	d := Scan(cameraJPEG(t))
	if !d.Valid() {
		t.Fatalf("no date found, got %+v", d)
	}
	if d.Wall != "2026:10:07 09:15:30" {
		t.Errorf("Wall = %q, want DateTimeOriginal over the IFD0 DateTime", d.Wall)
	}
	if d.Subsec != "482" {
		t.Errorf("Subsec = %q, want the inline short value", d.Subsec)
	}
	if d.Offset != "+08:00" {
		t.Errorf("Offset = %q", d.Offset)
	}
	got, err := d.Time()
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 7, 1, 15, 30, 482_000_000, time.UTC); !got.Equal(want) {
		t.Errorf("Time() = %v, want %v", got, want)
	}
}

func TestScanPrefersDigitizedWhenOriginalMissing(t *testing.T) {
	tiff := buildTIFF(t, binary.LittleEndian,
		[]ifdTag{{tag: tagExifIFD, isRef: true, ref: 1}},
		[]ifdTag{{tag: tagDateDigitized, text: "2026:10:07 09:15:31"}},
	)
	if d := Scan(jpegFile(exifApp1(tiff))); d.Wall != "2026:10:07 09:15:31" {
		t.Fatalf("Wall = %q, got %+v", d.Wall, d)
	}
}

func TestScanFallsBackToIFD0DateTime(t *testing.T) {
	tiff := buildTIFF(t, binary.LittleEndian,
		[]ifdTag{{tag: tagDateTime, text: "2025:03:01 08:00:00"}},
	)
	d := Scan(jpegFile(exifApp1(tiff)))
	if d.Wall != "2025:03:01 08:00:00" {
		t.Fatalf("Wall = %q, got %+v", d.Wall, d)
	}
	got, err := d.Time()
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2025, 3, 1, 8, 0, 0, 0, time.Local); !got.Equal(want) {
		t.Errorf("Time() = %v, want %v", got, want)
	}
}

func TestScanOnStdlibImages(t *testing.T) {
	if d := Scan(stdlibJPEG(t)); d.Valid() {
		t.Fatalf("stdlib JPEG should carry no date, got %+v", d)
	}
	if d := Scan(stdlibPNG(t)); d.Valid() {
		t.Fatalf("stdlib PNG should carry no date, got %+v", d)
	}
}

func TestRoundTripThroughApp1(t *testing.T) {
	for _, src := range []Date{
		{Wall: "2026:10:07 09:15:30"},
		{Wall: "2026:10:07 09:15:30", Offset: "-05:00"},
		{Wall: "2026:10:07 09:15:30", Subsec: "482", Offset: "+08:00"},
		{Wall: "2026:10:07 09:15:30.482"}, // trailing junk is cut, not carried
	} {
		block := src.App1()
		if block == nil {
			t.Fatalf("App1 = nil for %+v", src)
		}
		got := Scan(jpegFile(jfif(), block))
		want := src
		want.Wall = want.Wall[:19]
		if got != want {
			t.Errorf("round trip of %+v gave %+v", src, got)
		}
	}
}

func TestApp1IsDateOnlyAndCompact(t *testing.T) {
	got := (Date{Wall: "2026:10:07 09:15:30", Subsec: "482", Offset: "+08:00"}).App1()
	// Copying the source block over would carry its camera make and a thumbnail tens
	// of kilobytes wide; rebuilding keeps every slice near its original size.
	if len(got) > 200 {
		t.Errorf("APP1 segment is %d bytes, want a date-only block", len(got))
	}
	if strings.Contains(string(got), "iPhone") {
		t.Error("camera make leaked into the rebuilt block")
	}
}

func TestApp1NilForInvalidDate(t *testing.T) {
	if got := (Date{Wall: "junk"}).App1(); got != nil {
		t.Fatalf("App1 = %x", got)
	}
}

func TestTimeHonoursOffset(t *testing.T) {
	got, err := (Date{Wall: "2026:10:07 09:15:30", Offset: "-05:00"}).Time()
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 7, 14, 15, 30, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Without an offset the wall clock is read in the server's zone.
	got, err = (Date{Wall: "2026:10:07 09:15:30"}).Time()
	if err != nil {
		t.Fatal(err)
	}
	if got.Hour() != 9 || got.Location() != time.Local {
		t.Fatalf("got %v, want 09:15:30 in %v", got, time.Local)
	}
}

func TestValidRejectsGarbage(t *testing.T) {
	for _, d := range []Date{
		{},
		{Wall: "not a date"},
		{Wall: "2026:10:07"},
		{Wall: "2026-01-07 14:23:45"},
		{Wall: "2026:13:07 14:23:45"},
		{Wall: "2026:10:07 25:00:00"},
		{Wall: "2026:10:07 09:15:60"},
	} {
		if d.Valid() {
			t.Errorf("%+v accepted", d)
		}
	}
	for _, d := range []Date{
		{Wall: "2026:10:07 09:15:30"},
		{Wall: "2026:10:07 09:15:30", Offset: "GMT+8"}, // unusable offset, usable date
		{Wall: " 2026:10:07 09:15:30 "},
		{Wall: "2026:10:07 09:15:30", Subsec: "1234567"},
	} {
		if !d.Valid() {
			t.Errorf("%+v rejected", d)
		}
	}
}

func TestScanStopsAtStartOfScan(t *testing.T) {
	tiff := buildTIFF(t, binary.LittleEndian,
		[]ifdTag{{tag: tagDateTime, text: "2025:03:01 08:00:00"}},
	)
	if d := Scan(jpegFile(framed(0xda, []byte{1, 2, 3}), exifApp1(tiff))); d.Valid() {
		t.Fatal("metadata past the scan header was picked up")
	}
}

func TestScanSkipsFillBytes(t *testing.T) {
	tiff := buildTIFF(t, binary.LittleEndian,
		[]ifdTag{{tag: tagDateTime, text: "2025:03:01 08:00:00"}},
	)
	data := jpegFile(exifApp1(tiff))
	withFill := append([]byte{0xff, 0xd8, 0xff, 0xff}, data[2:]...)
	if d := Scan(withFill); !d.Valid() {
		t.Fatal("a leading fill byte hid the metadata")
	}
}

func TestScanHandlesTruncatedAndMalformed(t *testing.T) {
	tiff := buildTIFF(t, binary.LittleEndian,
		[]ifdTag{{tag: tagDateTime, text: "2025:03:01 08:00:00"}},
	)
	full := jpegFile(exifApp1(tiff))
	// Losing any part of the metadata segment itself must not yield a date. The
	// trailing EOI is not metadata, so the last two bytes are left out of the sweep.
	for i := 0; i < len(full)-2; i++ {
		if d := Scan(full[:i]); d.Valid() {
			t.Fatalf("truncated at %d yielded %+v", i, d)
		}
	}
	// A marker segment whose length runs past the end of the file.
	if d := Scan(full[:10]); d.Valid() {
		t.Fatal("short buffer accepted")
	}
	// An IFD offset pointing outside the TIFF.
	bad := append([]byte{}, tiff...)
	binary.LittleEndian.PutUint32(bad[4:8], 1<<24)
	if d := Scan(jpegFile(exifApp1(bad))); d.Valid() {
		t.Fatal("out-of-range IFD offset accepted")
	}
	// A string offset pointing outside the TIFF: IFD0 starts at 8, its single tag
	// at 10, and the value field of that entry at 18.
	bad = append([]byte{}, tiff...)
	binary.LittleEndian.PutUint32(bad[18:22], 1<<24)
	if d := Scan(jpegFile(exifApp1(bad))); d.Valid() {
		t.Fatal("out-of-range string offset accepted")
	}
	// An absurd tag count.
	bad = append([]byte{}, tiff...)
	binary.LittleEndian.PutUint16(bad[8:10], 60000)
	if d := Scan(jpegFile(exifApp1(bad))); d.Valid() {
		t.Fatal("oversized IFD accepted")
	}
	// A tag whose declared type is one this reader does not know.
	bad = append([]byte{}, tiff...)
	binary.LittleEndian.PutUint16(bad[12:14], 0x0009)
	if d := Scan(jpegFile(exifApp1(bad))); d.Valid() {
		t.Fatal("unknown tag type accepted")
	}
	for _, junk := range [][]byte{nil, {}, {0xff}, {0xff, 0xd8}, {0x89, 'P', 'N', 'G'}, []byte("Exif stuff")} {
		if d := Scan(junk); d.Valid() {
			t.Errorf("junk %x yielded %+v", junk, d)
		}
	}
}

func TestInjectJPEG(t *testing.T) {
	block := (Date{Wall: "2026:10:07 09:15:30"}).App1()
	out := InjectJPEG([]byte{0xff, 0xd8, 0xff, 0xd9}, block)
	if !bytes.HasPrefix(out, []byte{0xff, 0xd8, 0xff, 0xe1}) {
		t.Fatalf("APP1 not placed after SOI: %x", out)
	}
	if d := Scan(out); !d.Valid() {
		t.Fatal("injected date not readable back")
	}
	if got := InjectJPEG([]byte("PNG\x1a"), block); string(got) != "PNG\x1a" {
		t.Errorf("non-JPEG modified: %x", got)
	}
	if got := InjectJPEG([]byte{0xff, 0xd8, 0xff, 0xd9}, nil); len(got) != 4 {
		t.Errorf("empty segment modified the image: %x", got)
	}
	// A real encoded frame stays decodable after injection.
	injected := InjectJPEG(stdlibJPEG(t), block)
	if _, err := jpeg.Decode(bytes.NewReader(injected)); err != nil {
		t.Fatalf("injected JPEG no longer decodes: %v", err)
	}
	if d := Scan(injected); d.Wall != "2026:10:07 09:15:30" {
		t.Errorf("Wall = %q", d.Wall)
	}
}

// pngFile wraps chunks in a PNG signature; the CRC fields are placeholders because
// the reader does not check them.
func pngFile(chunks ...chunk) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	for _, c := range chunks {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(c.body)))
		buf.Write(lenBuf[:])
		buf.WriteString(c.kind)
		buf.Write(c.body)
		buf.Write([]byte{0, 0, 0, 0})
	}
	return buf.Bytes()
}

type chunk struct {
	kind string
	body []byte
}

func TestScanPNGeXIf(t *testing.T) {
	tiff := buildTIFF(t, binary.LittleEndian,
		[]ifdTag{{tag: tagDateTime, text: "2025:03:01 08:00:00"}},
	)
	ihdr := chunk{"IHDR", []byte("\x00\x00\x00\x04\x00\x00\x00\x04\x08\x06")}
	exif := chunk{"eXIf", tiff}
	idat := chunk{"IDAT", []byte("\x01\x02")}
	data := pngFile(ihdr, exif, idat)

	if d := Scan(data); d.Wall != "2025:03:01 08:00:00" {
		t.Fatalf("PNG eXIf not read: %+v", d)
	}
	// An eXIf chunk after the image data is not where metadata belongs.
	if d := Scan(pngFile(ihdr, idat, exif)); d.Valid() {
		t.Fatal("eXIf after IDAT picked up")
	}
	// Cutting the file inside the eXIf chunk leaves its declared length a lie.
	headerEnd := 8 + len(ihdr.body) + 12 + 8 + len(exif.body)
	if d := Scan(data[:headerEnd]); d.Valid() {
		t.Fatal("PNG truncated inside the eXIf chunk accepted")
	}
}

func TestRoundTripThroughPngChunk(t *testing.T) {
	for _, src := range []Date{
		{Wall: "2026:10:07 09:15:30"},
		{Wall: "2026:10:07 09:15:30", Offset: "+08:00"},
		{Wall: "2026:10:07 09:15:30", Subsec: "482", Offset: "-05:00"},
	} {
		chunk := src.PngChunk()
		if chunk == nil {
			t.Fatalf("PngChunk = nil for %+v", src)
		}
		// Go's PNG decoder checks the CRC of every chunk, so decoding the result is
		// also proof the checksum covers the right bytes.
		out := InjectPNG(stdlibPNG(t), chunk)
		if _, err := png.Decode(bytes.NewReader(out)); err != nil {
			t.Fatalf("injected PNG no longer decodes: %v", err)
		}
		if got := Scan(out); got != src {
			t.Errorf("round trip of %+v gave %+v", src, got)
		}
	}
}

func TestPngChunkNilForInvalidDate(t *testing.T) {
	if got := (Date{Wall: "junk"}).PngChunk(); got != nil {
		t.Fatalf("PngChunk = %x", got)
	}
}

func TestInjectPNG(t *testing.T) {
	ihdr := chunk{"IHDR", []byte("\x00\x00\x00\x04\x00\x00\x00\x04\x08\x06")}
	body := pngFile(ihdr, chunk{"IDAT", []byte("\x01\x02")})
	chunkBytes := (Date{Wall: "2026:10:07 09:15:30"}).PngChunk()

	out := InjectPNG(body, chunkBytes)
	// The chunk lands between IHDR and IDAT, which is where the spec puts it.
	at := 8 + 12 + len(ihdr.body)
	if string(out[at+4:at+8]) != "eXIf" {
		t.Fatalf("eXIf not after IHDR: %x", out[at:at+8])
	}
	if !bytes.Equal(out[at+len(chunkBytes):], body[at:]) {
		t.Error("the bytes after the inserted chunk moved")
	}

	for _, notPng := range [][]byte{{0xff, 0xd8, 0xff, 0xd9}, []byte("PNG\x1a"), nil} {
		if got := InjectPNG(notPng, chunkBytes); !bytes.Equal(got, notPng) {
			t.Errorf("non-PNG %x modified: %x", notPng, got)
		}
	}
	// Too short to hold a complete IHDR header.
	if got := InjectPNG(body[:12], chunkBytes); !bytes.Equal(got, body[:12]) {
		t.Error("truncated PNG modified")
	}
	// An IHDR whose declared length runs past the end of the file.
	lying := append([]byte{}, body...)
	binary.BigEndian.PutUint32(lying[8:12], 1<<20)
	if got := InjectPNG(lying, chunkBytes); !bytes.Equal(got, lying) {
		t.Error("impossible IHDR length accepted")
	}
	if got := InjectPNG(body, nil); !bytes.Equal(got, body) {
		t.Error("empty chunk modified the image")
	}
}

func TestMetaReadsOrientationAlongsideTheDate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order binary.ByteOrder
		o     uint16
	}{
		{"little endian", binary.LittleEndian, 6},
		{"big endian", binary.BigEndian, 8},
	} {
		tiff := buildTIFF(t, tc.order,
			[]ifdTag{
				{tag: tagOrientation, isShort: true, short: tc.o},
				{tag: tagDateTime, text: "2026:10:07 09:15:30"},
			},
		)
		doc := Meta(jpegFile(exifApp1(tiff)))
		if doc.Orientation != tc.o {
			t.Errorf("%s: Orientation = %d, want %d", tc.name, doc.Orientation, tc.o)
		}
		if doc.Taken.Wall != "2026:10:07 09:15:30" {
			t.Errorf("%s: Taken = %+v", tc.name, doc.Taken)
		}
	}
}

func TestMetaReportsOrientationWithoutADate(t *testing.T) {
	tiff := buildTIFF(t, binary.LittleEndian,
		[]ifdTag{{tag: tagOrientation, isShort: true, short: 3}},
	)
	doc := Meta(jpegFile(jfif(), exifApp1(tiff)))
	if doc.Orientation != 3 {
		t.Errorf("Orientation = %d, want 3", doc.Orientation)
	}
	if doc.Taken.Valid() {
		t.Errorf("Taken = %+v, want the zero Date", doc.Taken)
	}
	if doc := Meta(pngFile(chunk{"IHDR", nil}, chunk{"eXIf", tiff})); doc.Orientation != 3 {
		t.Errorf("PNG eXIf Orientation = %d, want 3", doc.Orientation)
	}
}

func TestMetaIgnoresValuesThatAreNotAnOrientation(t *testing.T) {
	// Wrong field type, and no metadata block at all, must not send the caller
	// off to turn pixels that were already upright.
	long := buildTIFF(t, binary.LittleEndian, []ifdTag{{tag: tagOrientation, long: 6}})
	if o := Meta(jpegFile(exifApp1(long))).Orientation; o != 0 {
		t.Errorf("LONG-typed tag read as %d", o)
	}
	if o := Meta(jpegFile(jfif())).Orientation; o != 0 {
		t.Errorf("EXIF-less JPEG read as %d", o)
	}
	if o := Meta(stdlibJPEG(t)).Orientation; o != 0 {
		t.Errorf("stdlib JPEG read as %d", o)
	}
	// A number outside the eight EXIF defines is passed straight through; deciding
	// what to do with it is the reader's job.
	bogus := buildTIFF(t, binary.LittleEndian, []ifdTag{{tag: tagOrientation, isShort: true, short: 200}})
	if o := Meta(jpegFile(exifApp1(bogus))).Orientation; o != 200 {
		t.Errorf("bogus value = %d, want 200", o)
	}
}

func TestFromTimeCarriesTheSameInstant(t *testing.T) {
	for _, zone := range []struct {
		name   string
		offset int
		want   string
	}{
		{"east", 8 * 3600, "+08:00"},
		{"west", -(5*3600 + 30*60), "-05:30"},
		{"utc", 0, "+00:00"},
	} {
		when := time.Date(2026, 10, 5, 21, 4, 9, 123_000_000, time.FixedZone(zone.name, zone.offset))
		d := FromTime(when)
		if d.Offset != zone.want {
			t.Fatalf("%s: offset = %q, want %q", zone.name, d.Offset, zone.want)
		}
		if d.Subsec != "123" {
			t.Fatalf("%s: subsec = %q", zone.name, d.Subsec)
		}
		got, err := d.Time()
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(when) {
			t.Fatalf("%s: %s, want %s", zone.name, got, when)
		}
		// Writing it into a file and reading it back has to land on the same instant.
		if !d.InZipRange() {
			t.Fatalf("%s: %v rejected from the zip window", zone.name, d)
		}
		back := Scan(jpegFile(jfif(), d.App1()))
		instant, err := back.Time()
		if err != nil {
			t.Fatalf("%s: rescanned %+v: %v", zone.name, back, err)
		}
		if !instant.Equal(when) {
			t.Fatalf("%s: after App1 %+v reads %s", zone.name, d, instant)
		}
	}
}

func TestInZipRange(t *testing.T) {
	for _, tc := range []struct {
		wall string
		want bool
	}{
		{"1979:12:31 23:59:59", false},
		{"1980:01:01 00:00:00", true},
		{"2107:12:31 23:59:59", true},
		{"2108:01:01 00:00:00", false},
		{"19:70:01 00:00:00", false}, // unparseable
		{"", false},
	} {
		if got := (Date{Wall: tc.wall}).InZipRange(); got != tc.want {
			t.Fatalf("InZipRange(%q) = %v, want %v", tc.wall, got, tc.want)
		}
	}
	if (Date{}).InZipRange() {
		t.Fatal("the zero date is in range")
	}
	if FromTime(time.UnixMilli(0)).InZipRange() {
		t.Fatal("epoch 0, what browsers report for an unknown time, is in range")
	}
}
