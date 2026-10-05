// Package store keeps uploaded screenshots on disk and their decoded form in RAM.
package store

import (
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

	// Register image decoders for the accepted upload types.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// maxPixels rejects absurdly large uploads before they exhaust memory.
const maxPixels = 120_000_000

// hotWindow keeps recently used uploads decoded; beyond it the pixel buffer is
// dropped and the file is re-decoded on demand.
const hotWindow = 3 * time.Minute

type Picture struct {
	ID       string
	Filename string
	Mime     string
	Width    int
	Height   int
	Path     string

	mu   sync.Mutex
	rgba *image.RGBA
	used time.Time
}

// Image returns the decoded pixels, normalised to a zero-origin RGBA buffer.
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
	rgba, ok := img.(*image.RGBA)
	if !ok || rgba.Rect.Min != (image.Point{}) {
		buf := image.NewRGBA(image.Rect(0, 0, p.Width, p.Height))
		draw.Draw(buf, buf.Bounds(), img, img.Bounds().Min, draw.Src)
		rgba = buf
	}
	p.rgba = rgba
	return rgba, nil
}

// Release drops the decoded pixels; the file stays so it can be re-opened.
func (p *Picture) Release() {
	p.mu.Lock()
	p.rgba = nil
	p.mu.Unlock()
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
}

// Put persists an upload and reports its dimensions without holding the whole
// pixel buffer in memory.
func (s *Store) Put(r io.Reader, filename string) (*Picture, error) {
	id := newID()
	ext := strings.ToLower(filepath.Ext(sanitize(filename)))
	if _, ok := extMime[ext]; !ok {
		return nil, errors.New("unsupported file type, please upload a PNG, JPEG or GIF")
	}
	path := filepath.Join(s.dir, id+ext)

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(f, r)
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

	p := &Picture{
		ID:       id,
		Filename: sanitize(filename),
		Mime:     formatMime(format),
		Width:    cfg.Width,
		Height:   cfg.Height,
		Path:     path,
		used:     time.Now(),
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
			s.mu.Lock()
			for id, p := range s.items {
				idle := p.idleFor(now)
				switch {
				case idle > s.ttl:
					p.Release()
					os.Remove(p.Path)
					delete(s.items, id)
					log.Printf("store: expired %s", id)
				case idle > hotWindow:
					p.Release()
				}
			}
			s.mu.Unlock()
		}
	}
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
	}
	return "application/octet-stream"
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
