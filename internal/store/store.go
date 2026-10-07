// Package store keeps uploaded screenshots on disk and their decoded form in RAM.
package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tuqie/internal/exif"

	// Register image decoders for the accepted upload types. The container
	// formats do it themselves, from meta.go.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// maxPixels rejects absurdly large uploads before they exhaust memory.
const maxPixels = 120_000_000

// headBytes is how much of an upload is kept around for metadata probing. EXIF
// precedes the pixel data, and this covers files carrying a large thumbnail.
const headBytes = 1 << 20

// hotWindow keeps recently used uploads decoded; beyond it the pixel buffer is
// dropped and the file is re-decoded on demand.
const hotWindow = 3 * time.Minute

type Picture struct {
	ID       string
	Filename string
	Mime     string
	// Width and Height are the dimensions as they are meant to be seen: for a
	// rotated upload they are already swapped from what the bytes store.
	Width  int
	Height int
	Path   string
	// Orientation is the EXIF tag telling how the stored pixels relate to the
	// shot. 0 and 1 both mean upright.
	Orientation uint16
	// Taken is the capture date of the upload: the one in its metadata, or the
	// uploader's file timestamp when the bytes carry none.
	Taken exif.Date

	// crop is the part of the decoded buffer that is picture, for a container
	// that codes the photo up to a codec's block grid.
	crop image.Rectangle

	mu   sync.Mutex
	rgba *image.RGBA
	used time.Time
}

// Image returns the decoded pixels, turned upright and normalised to a
// zero-origin RGBA buffer.
func (p *Picture) Image() (*image.RGBA, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.used = time.Now()
	if p.rgba != nil {
		return p.rgba, nil
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	b := img.Bounds()
	rgba, ok := img.(*image.RGBA)
	if !ok || rgba.Rect.Min != (image.Point{}) {
		buf := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(buf, buf.Bounds(), img, b.Min, draw.Src)
		rgba = buf
	}
	p.rgba = upright(cropTo(rgba, p.crop), p.Orientation)
	return p.rgba, nil
}

// cropTo keeps the part of a decoded picture the file says is image. A crop
// that does not sit inside the pixels is ignored: the header promised one
// buffer and the decoder handed over another, and dropping the middle of a
// photo costs more than the padding row that was meant to go.
func cropTo(src *image.RGBA, r image.Rectangle) *image.RGBA {
	if r.Empty() || r.Eq(src.Bounds()) || !r.In(src.Bounds()) {
		return src
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), src, r.Min, draw.Src)
	return out
}

// Release drops the decoded pixels; the file stays so it can be re-opened.
func (p *Picture) Release() {
	p.mu.Lock()
	p.rgba = nil
	p.mu.Unlock()
}

// PreviewPath is where the downscaled copy of this upload is kept, beside the
// original and gone with it.
func (p *Picture) PreviewPath() string {
	return strings.TrimSuffix(p.Path, filepath.Ext(p.Path)) + ".preview.jpg"
}

func (p *Picture) idleFor(now time.Time) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return now.Sub(p.used)
}

type Store struct {
	dir    string
	ttl    time.Duration
	mu     sync.Mutex
	items  map[string]*Picture
	stopCh chan struct{}
}

func New(dir string, ttl time.Duration) (*Store, error) {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "tuqie")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		dir:    dir,
		ttl:    ttl,
		items:  make(map[string]*Picture),
		stopCh: make(chan struct{}),
	}
	go s.reap()
	return s, nil
}

func (s *Store) Close() { close(s.stopCh) }

var extMime = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".heic": "image/heic",
	".heif": "image/heif",
	".avif": "image/avif",
	".jxl":  "image/jxl",
}

// Put persists an upload and reports its dimensions without holding the whole
// pixel buffer in memory. modified is the file's own timestamp as the uploader
// knows it; it stands in for the capture date only when the bytes carry none.
func (s *Store) Put(r io.Reader, filename string, modified time.Time) (*Picture, error) {
	id := newID()
	ext := strings.ToLower(filepath.Ext(sanitize(filename)))
	if _, ok := extMime[ext]; !ok {
		return nil, errors.New("unsupported file type, please upload a PNG, JPEG, GIF, HEIC, AVIF or JXL")
	}
	path := filepath.Join(s.dir, id+ext)

	head, err := io.ReadAll(io.LimitReader(r, headBytes))
	if err != nil {
		return nil, err
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(f, io.MultiReader(bytes.NewReader(head), r))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(path)
		if err != nil {
			return nil, err
		}
		return nil, closeErr
	}
	if n == 0 {
		os.Remove(path)
		return nil, errors.New("empty upload")
	}

	cfg, format, err := readConfig(path)
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	pix := int64(cfg.Width) * int64(cfg.Height)
	if pix > maxPixels {
		os.Remove(path)
		return nil, fmt.Errorf("image too large: %dx%d is %.0f MP, limit is %.0f MP", cfg.Width, cfg.Height, float64(pix)/1e6, float64(maxPixels)/1e6)
	}
	meta := metaFor(format, path, head)
	// A container that rotates the picture itself says so in its header, and
	// that is what every viewer obeys; an EXIF tag is the fallback.
	shown := displayFor(format, path)
	orientation := meta.Orientation
	if shown.orientation != 0 {
		orientation = shown.orientation
	}
	width, height := cfg.Width, cfg.Height
	if !shown.crop.Empty() {
		width, height = shown.crop.Dx(), shown.crop.Dy()
	}

	p := &Picture{
		ID:          id,
		Filename:    sanitize(filename),
		Mime:        formatMime(format),
		Width:       width,
		Height:      height,
		Path:        path,
		Orientation: orientation,
		crop:        shown.crop,
		Taken:       meta.Taken,
		used:        time.Now(),
	}
	// A quarter turn is also how a phone stores a portrait shot, so the caller
	// hears about the picture the way every viewer will show it.
	if turnsDims(orientation) {
		p.Width, p.Height = p.Height, p.Width
	}
	if !p.Taken.Valid() {
		if candidate := exif.FromTime(modified); candidate.InZipRange() {
			p.Taken = candidate
		}
	}
	s.mu.Lock()
	s.items[id] = p
	s.mu.Unlock()
	log.Printf("store: added %s %s %dx%d (%d bytes)", id, p.Mime, p.Width, p.Height, n)
	return p, nil
}

func (s *Store) Get(id string) (*Picture, bool) {
	s.mu.Lock()
	p, ok := s.items[id]
	s.mu.Unlock()
	if ok {
		p.mu.Lock()
		p.used = time.Now()
		p.mu.Unlock()
	}
	return p, ok
}

func (s *Store) reap() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-t.C:
			s.sweep(now)
		}
	}
}

// sweep drops uploads idle beyond the TTL and evicts the decoded pixels of
// ones past the hot window.
func (s *Store) sweep(now time.Time) {
	s.mu.Lock()
	for id, p := range s.items {
		idle := p.idleFor(now)
		switch {
		case idle > s.ttl:
			p.Release()
			os.Remove(p.Path)
			os.Remove(p.PreviewPath())
			delete(s.items, id)
			log.Printf("store: expired %s", id)
		case idle > hotWindow:
			p.Release()
		}
	}
	s.mu.Unlock()
}

func readConfig(path string) (image.Config, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Config{}, "", err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return image.Config{}, "", fmt.Errorf("not a readable image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return image.Config{}, "", errors.New("image has no dimensions")
	}
	return cfg, format, nil
}

func formatMime(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "heic":
		return "image/heic"
	case "avif":
		return "image/avif"
	case "jxl":
		return "image/jxl"
	}
	return "application/octet-stream"
}

// turnsDims reports whether o means the picture is stored on its side.
func turnsDims(o uint16) bool { return o >= 5 && o <= 8 }

// upright returns src the way its metadata says it should be seen. Orientation 1
// and any value outside the eight EXIF defines come back untouched, so a bogus
// tag costs nothing rather than turning the picture the wrong way.
func upright(src *image.RGBA, o uint16) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Rect.Dx(), src.Rect.Dy()
	ow, oh := w, h
	if turnsDims(o) {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < oh; y++ {
		for x := 0; x < ow; x++ {
			sx, sy := sourceOf(o, x, y, w, h)
			s := sy*src.Stride + sx*4
			d := y*out.Stride + x*4
			copy(out.Pix[d:d+4], src.Pix[s:s+4])
		}
	}
	return out
}

// sourceOf is where the pixel standing at x,y in the upright picture came from.
func sourceOf(o uint16, x, y, w, h int) (int, int) {
	switch o {
	case 2: // mirrored across the vertical
		return w - 1 - x, y
	case 3: // turned around
		return w - 1 - x, h - 1 - y
	case 4: // mirrored across the horizontal
		return x, h - 1 - y
	case 5:
		return y, x
	case 6: // stored a quarter turn anticlockwise
		return y, h - 1 - x
	case 7:
		return w - 1 - y, h - 1 - x
	case 8: // stored a quarter turn clockwise
		return w - 1 - y, x
	}
	return x, y
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func sanitize(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, name)
	if len(name) > 120 {
		name = name[:120]
	}
	if name == "" || name == "." {
		return "screenshot"
	}
	return name
}
