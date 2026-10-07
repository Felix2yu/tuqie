package store

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tuqie/internal/exif"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// hugeGIFHeader is a GIF logical screen descriptor claiming w*h pixels with no
// image data behind it — enough for DecodeConfig, absurd enough for the limit.
func hugeGIFHeader(t *testing.T, w, h int) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	buf.WriteString("GIF89a")
	binary.Write(buf, binary.LittleEndian, uint16(w))
	binary.Write(buf, binary.LittleEndian, uint16(h))
	buf.WriteByte(0) // no global color table
	buf.WriteByte(0) // background index
	buf.WriteByte(0) // pixel aspect ratio
	return buf.Bytes()
}

func newStore(t *testing.T, ttl time.Duration) *Store {
	t.Helper()
	s, err := New(t.TempDir(), ttl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func put(t *testing.T, s *Store, data []byte, name string) *Picture {
	return putModified(t, s, data, name, time.Time{})
}

// putModified uploads along with the timestamp the reporter claims for the file.
func putModified(t *testing.T, s *Store, data []byte, name string, modified time.Time) *Picture {
	t.Helper()
	p, err := s.Put(bytes.NewReader(data), name, modified)
	if err != nil {
		t.Fatalf("Put(%s): %v", name, err)
	}
	return p
}

func TestPutValidPNG(t *testing.T) {
	s := newStore(t, time.Hour)
	data := pngBytes(t, 6, 10)
	p := put(t, s, data, "shot.png")

	if p.Width != 6 || p.Height != 10 {
		t.Fatalf("dims %dx%d, want 6x10", p.Width, p.Height)
	}
	if p.Mime != "image/png" {
		t.Fatalf("mime %q", p.Mime)
	}
	if len(p.ID) != 24 {
		t.Fatalf("id %q, want 24 hex chars", p.ID)
	}
	if _, err := hex.DecodeString(p.ID); err != nil {
		t.Fatalf("id not hex: %v", err)
	}
	if _, err := os.Stat(p.Path); err != nil {
		t.Fatalf("persisted file: %v", err)
	}
	got, ok := s.Get(p.ID)
	if !ok || got != p {
		t.Fatalf("Get(%s) = %v, %v", p.ID, got, ok)
	}
	if _, ok := s.Get("deadbeef"); ok {
		t.Fatal("unknown id should miss")
	}
}

func TestPutRejectsBadInput(t *testing.T) {
	s := newStore(t, time.Hour)

	if _, err := s.Put(bytes.NewReader(pngBytes(t, 2, 2)), "notes.txt", time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("wrong extension: %v", err)
	}
	if _, err := s.Put(strings.NewReader(""), "empty.png", time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "empty upload") {
		t.Fatalf("empty body: %v", err)
	}
	if _, err := s.Put(strings.NewReader("definitely not an image"), "fake.png", time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "not a readable image") {
		t.Fatalf("garbage bytes: %v", err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected uploads should leave no files, dir has %d", len(entries))
	}
}

func TestPutRejectsOverPixelLimit(t *testing.T) {
	s := newStore(t, time.Hour)
	// 30000x30000 = 900 MP, well past the 120 MP cap.
	_, err := s.Put(bytes.NewReader(hugeGIFHeader(t, 30000, 30000)), "big.gif", time.Time{})
	if err == nil || !strings.Contains(err.Error(), "image too large") {
		t.Fatalf("want pixel-limit rejection, got %v", err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized upload should be removed, dir has %d files", len(entries))
	}
	// Under the cap the same header-only GIF passes the config check.
	if _, err := s.Put(bytes.NewReader(hugeGIFHeader(t, 200, 200)), "ok.gif", time.Time{}); err != nil {
		t.Fatalf("40000 px gif should be accepted: %v", err)
	}
}

func TestPutSanitizesFilename(t *testing.T) {
	s := newStore(t, time.Hour)
	long := strings.Repeat("a", 60)
	p := put(t, s, pngBytes(t, 2, 2), "/some/dir/"+long+" b.png")
	if strings.ContainsAny(p.Filename, "/ ") || len(p.Filename) > 120 {
		t.Fatalf("filename not sanitized: %q", p.Filename)
	}
	if !strings.HasSuffix(p.Filename, ".png") {
		t.Fatalf("extension lost: %q", p.Filename)
	}
}

func TestImageCachesThenRedecodes(t *testing.T) {
	s := newStore(t, time.Hour)
	// JPEG decodes to *image.YCbCr, so this also covers RGBA normalisation.
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}

	p := put(t, s, buf.Bytes(), "shot.jpg")
	if p.Mime != "image/jpeg" {
		t.Fatalf("mime %q", p.Mime)
	}
	first, err := p.Image()
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Image()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("hot window should serve the cached buffer")
	}
	if first.Rect.Min != (image.Point{}) || first.Rect.Dx() != 8 {
		t.Fatalf("unexpected rect %v", first.Rect)
	}
	if got := first.RGBAAt(4, 4); got.G != img.RGBAAt(4, 4).G {
		t.Fatalf("pixel (4,4) = %v", got)
	}

	// After eviction the same file must decode to identical pixels.
	p.Release()
	third, err := p.Image()
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("Release should have dropped the cached buffer")
	}
	if third.RGBAAt(4, 4) != first.RGBAAt(4, 4) {
		t.Fatal("re-decoded pixels differ")
	}

	// A vanished file surfaces as an error, not a panic.
	p.Release()
	os.Remove(p.Path)
	if _, err := p.Image(); err == nil {
		t.Fatal("Image should fail once the file is gone")
	}
}

func TestSweepExpiresByTTL(t *testing.T) {
	s := newStore(t, time.Minute)
	p := put(t, s, pngBytes(t, 4, 4), "a.png")

	// Backdate past the TTL and reap manually.
	stale := time.Now().Add(-2 * time.Minute)
	p.mu.Lock()
	p.used = stale
	p.mu.Unlock()

	s.sweep(time.Now())

	if _, ok := s.Get(p.ID); ok {
		t.Fatal("expired upload should be gone")
	}
	if _, err := os.Stat(p.Path); !os.IsNotExist(err) {
		t.Fatalf("expired file should be removed: %v", err)
	}
	// Fresh uploads survive the same sweep.
	q := put(t, s, pngBytes(t, 4, 4), "b.png")
	s.sweep(time.Now())
	if _, ok := s.Get(q.ID); !ok {
		t.Fatal("fresh upload should survive")
	}
}

func TestSweepEvictsHotWindowPixelsOnly(t *testing.T) {
	s := newStore(t, time.Hour)
	p := put(t, s, pngBytes(t, 4, 4), "a.png")
	if _, err := p.Image(); err != nil {
		t.Fatal(err)
	}

	// Idle beyond the hot window but inside the TTL: pixels dropped, entry kept.
	idle := time.Now().Add(-(hotWindow + time.Minute))
	p.mu.Lock()
	p.used = idle
	p.mu.Unlock()
	s.sweep(time.Now())

	p.mu.Lock()
	cached := p.rgba
	p.mu.Unlock()
	if cached != nil {
		t.Fatal("hot-window upload should have lost its decoded pixels")
	}
	if _, err := os.Stat(p.Path); err != nil {
		t.Fatalf("file must stay for re-decode: %v", err)
	}
	if _, err := p.Image(); err != nil {
		t.Fatalf("should re-decode after eviction: %v", err)
	}
}

func TestNewDefaultsToTempDir(t *testing.T) {
	s, err := New("", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := filepath.Join(os.TempDir(), "tuqie")
	if s.dir != want {
		t.Fatalf("dir %q, want %q", s.dir, want)
	}
	if fi, err := os.Stat(want); err != nil || !fi.IsDir() {
		t.Fatalf("default dir missing: %v", err)
	}
}

func TestFormatMime(t *testing.T) {
	cases := map[string]string{
		"png":  "image/png",
		"jpeg": "image/jpeg",
		"gif":  "image/gif",
		"webp": "image/webp",
		"tiff": "application/octet-stream",
	}
	for format, want := range cases {
		if got := formatMime(format); got != want {
			t.Fatalf("formatMime(%q) = %q, want %q", format, got, want)
		}
	}
}

func TestNewRejectsUnusableDir(t *testing.T) {
	// A regular file standing where the data directory should be.
	blocker := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(blocker, "tuqie"), time.Minute); err == nil {
		t.Fatal("MkdirAll failure should reach the caller")
	}
}

// errReader fails after handing out a few bytes, the way a dropped upload does.
type errReader struct{ left int }

func (r *errReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, os.ErrClosed
	}
	n := min(r.left, len(p))
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	r.left -= n
	return n, nil
}

func TestPutCleansUpAfterAFailedCopy(t *testing.T) {
	s := newStore(t, time.Hour)
	if _, err := s.Put(&errReader{left: 4096}, "shot.png", time.Time{}); err == nil {
		t.Fatal("a truncated upload should fail")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a partial upload should be removed, dir has %d files", len(entries))
	}
}

func TestPutCleansUpWhenTheDirIsGone(t *testing.T) {
	s := newStore(t, time.Hour)
	// The directory disappearing mid-life (an OS temp cleaner, a container
	// restart) has to surface as an error, not a stray file handle.
	os.RemoveAll(s.dir)
	if _, err := s.Put(bytes.NewReader(pngBytes(t, 2, 2)), "shot.png", time.Time{}); err == nil {
		t.Fatal("Create failure should reach the caller")
	}
}

func TestPutRejectsImageWithNoDimensions(t *testing.T) {
	s := newStore(t, time.Hour)
	_, err := s.Put(bytes.NewReader(hugeGIFHeader(t, 0, 0)), "zero.gif", time.Time{})
	if err == nil || !strings.Contains(err.Error(), "no dimensions") {
		t.Fatalf("want the no-dimensions rejection, got %v", err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected upload should be removed, dir has %d files", len(entries))
	}
}

func TestImageReportsUndecodableFile(t *testing.T) {
	s := newStore(t, time.Hour)
	// A GIF header is enough for DecodeConfig but not for a full decode, so Put
	// accepts it and Image is where the damage shows up.
	p := put(t, s, hugeGIFHeader(t, 200, 200), "header.gif")
	if _, err := p.Image(); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want a decode error, got %v", err)
	}
}

func TestReadConfigMissingFile(t *testing.T) {
	if _, _, err := readConfig(filepath.Join(t.TempDir(), "gone.png")); err == nil {
		t.Fatal("reading a vanished file should fail")
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"shot.png", "shot.png"},
		{"/etc/passwd", "passwd"},
		{"..\\..\\evil.png", ".._.._evil.png"},
		{"中文截图.PNG", "____.PNG"},
		{"a b\tc.png", "a_b_c.png"},
		{strings.Repeat("x", 200) + ".png", strings.Repeat("x", 120)},
		{".", "screenshot"},
		{"", "screenshot"},
	}
	for _, c := range cases {
		got := sanitize(c.in)
		if got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
		if len(got) > 120 {
			t.Errorf("sanitize(%q) = %q, longer than 120", c.in, got)
		}
	}
}

func TestIdleFor(t *testing.T) {
	p := &Picture{used: time.Now().Add(-3 * time.Second)}
	if d := p.idleFor(time.Now()); d < 2*time.Second || d > 5*time.Second {
		t.Fatalf("idleFor = %s", d)
	}
}

// datedJPEG returns a small JPEG carrying taken as its capture date; the zero Date
// produces a plain file with no metadata.
func datedJPEG(t *testing.T, taken exif.Date) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 8)), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return exif.InjectJPEG(buf.Bytes(), taken.App1())
}

func TestPutReadsSourceDateAndKeepsBytes(t *testing.T) {
	s := newStore(t, time.Hour)
	taken := exif.Date{Wall: "2026:10:07 09:15:30", Subsec: "482", Offset: "+08:00"}
	data := datedJPEG(t, taken)
	p := put(t, s, data, "shot.jpg")

	if p.Taken != taken {
		t.Fatalf("Taken = %+v, want %+v", p.Taken, taken)
	}
	onDisk, err := os.ReadFile(p.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, data) {
		t.Fatal("reading the metadata probe changed what reached disk")
	}
}

func TestPutWithoutSourceDate(t *testing.T) {
	s := newStore(t, time.Hour)
	if p := put(t, s, datedJPEG(t, exif.Date{}), "a.jpg"); p.Taken.Valid() {
		t.Fatalf("undated JPEG got %+v", p.Taken)
	}
	if p := put(t, s, pngBytes(t, 2, 2), "b.png"); p.Taken.Valid() {
		t.Fatalf("PNG upload got %+v", p.Taken)
	}
}

func TestPutStreamsUploadsLargerThanTheProbe(t *testing.T) {
	// The probe read stops at headBytes and the remainder has to stream through
	// untouched, otherwise a big screenshot would land on disk truncated.
	s := newStore(t, time.Hour)
	img := image.NewRGBA(image.Rect(0, 0, 700, 700))
	for i := range img.Pix {
		img.Pix[i] = byte(rand.Intn(256))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if len(data) <= headBytes {
		t.Fatalf("test image is %d bytes, want more than the %d byte probe", len(data), headBytes)
	}

	p := put(t, s, data, "noise.png")
	onDisk, err := os.ReadFile(p.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, data) {
		t.Fatalf("stored %d of %d bytes", len(onDisk), len(data))
	}
	if p.Width != 700 || p.Height != 700 {
		t.Fatalf("dims %dx%d", p.Width, p.Height)
	}
}

func TestPutFallsBackToTheReportedFileTime(t *testing.T) {
	s := newStore(t, time.Hour)
	when := time.Date(2026, 10, 5, 21, 4, 9, 0, time.Local)

	p := putModified(t, s, pngBytes(t, 2, 2), "shot.png", when)
	if got, want := p.Taken, exif.FromTime(when); got != want {
		t.Fatalf("Taken = %+v, want %+v", got, want)
	}

	// A date inside the file outranks whatever the uploader claims for it.
	taken := exif.Date{Wall: "2026:10:07 09:15:30", Offset: "+08:00"}
	if p := putModified(t, s, datedJPEG(t, taken), "shot.jpg", when); p.Taken != taken {
		t.Fatalf("the metadata date was replaced by %+v", p.Taken)
	}

	// Browsers report 0 when they cannot tell, and anything the zip window cannot
	// hold is dropped instead of written as a wrong date.
	for _, ms := range []int64{0, -1, 1e9} {
		if p := putModified(t, s, pngBytes(t, 2, 2), "shot.png", time.UnixMilli(ms)); p.Taken.Valid() {
			t.Fatalf("reported time %d produced %+v", ms, p.Taken)
		}
	}
}
