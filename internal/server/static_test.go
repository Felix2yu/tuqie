package server

import (
	"bytes"
	"compress/gzip"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/fstest"
)

// build is a frontend the way vite leaves it: a hashed bundle, an unhashed shell
// that names it, and a picture that must not be passed through a compressor
// twice.
var build = fstest.MapFS{
	"index.html":          &fstest.MapFile{Data: []byte("<html><script src=\"/assets/index-abc.js\"></script></html>")},
	"manifest.json":       &fstest.MapFile{Data: []byte(`{"name":"图切","start_url":"/"}`)},
	"sw.js":               &fstest.MapFile{Data: []byte("self.addEventListener('fetch', () => {})")},
	"assets/index-abc.js": &fstest.MapFile{Data: []byte(bytes.Repeat([]byte("console.log(1);"), 200))},
	"icons/icon-192.png":  &fstest.MapFile{Data: testPNGBytes},
}

// An image the decoder would still accept, since the point of leaving it alone is
// that a second pass would corrupt a body somebody else already packed.
var testPNGBytes = func() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		panic(err)
	}
	return buf.Bytes()
}()

func get(t *testing.T, h http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestBuildFilesAreHeldAndTheShellThatNamesThemIsNot(t *testing.T) {
	h := static(build)

	for _, tc := range []struct {
		path, want string
	}{
		{"/", "no-cache"},
		{"/sw.js", "no-cache"},
		{"/manifest.json", "no-cache"},
		{"/assets/index-abc.js", "private, max-age=31536000, immutable"},
	} {
		if got := get(t, h, tc.path, nil).Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("%s: Cache-Control = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// A stale sw.js is a deploy the browser refuses to look at, which is why the
// shell gets no freshness at all rather than a share of it.
func TestShellIsReaskedEvenWhenCompressed(t *testing.T) {
	rec := get(t, static(build), "/sw.js", map[string]string{"Accept-Encoding": "gzip"})
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want the one header a proxy has to key on", got)
	}
}

func TestTextGoesOutCompressed(t *testing.T) {
	h := static(build)
	plain := get(t, h, "/assets/index-abc.js", nil)

	rec := get(t, h, "/assets/index-abc.js", map[string]string{"Accept-Encoding": "br, gzip;q=0.5"})
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	// The length the file server worked out is the uncompressed one, and a client
	// that reads 400 bytes of the 4200 it was promised stops short of the footer.
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length = %q, want it gone in favour of the real size", got)
	}
	if raw := rec.Body.Bytes(); len(raw) >= plain.Body.Len() {
		t.Fatalf("compressed to %d bytes, plain is %d", len(raw), plain.Body.Len())
	}

	// Read it back the way the page would: what comes out has to be the file, not
	// a stream that happens to start correctly.
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("the body is not a gzip stream: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if err := zr.Close(); err != nil {
		t.Fatalf("the stream is truncated: %v", err)
	}
	if !bytes.Equal(got, plain.Body.Bytes()) {
		t.Fatal("the compressed body is not the file it came from")
	}
}

func TestPicturesAreLeftAlone(t *testing.T) {
	rec := get(t, static(build), "/icons/icon-192.png", map[string]string{"Accept-Encoding": "gzip"})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q on an image", got)
	}
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(len(testPNGBytes)); got != want {
		t.Fatalf("Content-Length = %q, want the size of the file %q", got, want)
	}
	if body := rec.Body.Bytes(); !bytes.HasPrefix(body, []byte("\x89PNG")) || len(body) != len(testPNGBytes) {
		t.Fatal("the bytes no compressor should have touched are not there")
	}
}

// A client that did not ask for gzip must get the plain file: a browser fetches
// an image with Accept-Encoding set all the same, and the body it reads has to
// match what the decoder expects.
func TestAnUncompressedAnswerIsStillTheFile(t *testing.T) {
	rec := get(t, static(build), "/", map[string]string{"Accept-Encoding": "identity"})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if rec.Body.String() == "" {
		t.Fatal("empty body")
	}
}

func TestRangedReadStaysARangedRead(t *testing.T) {
	rec := get(t, static(build), "/assets/index-abc.js", map[string]string{
		"Accept-Encoding": "gzip",
		"Range":           "bytes=0-99",
	})
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("code %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q on a slice of the stored bytes", got)
	}
	if rec.Body.Len() != 100 {
		t.Fatalf("body is %d bytes, want the 100 asked for", rec.Body.Len())
	}
}

func TestAnEmptyBodyClaimsNoCompression(t *testing.T) {
	// The file server redirects its own filename to the directory it lives in; the
	// redirect has no body, so it must not go out saying it is gzipped.
	rec := get(t, static(build), "/index.html", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("code %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q on an empty body", got)
	}
	if got := rec.Header().Get("Location"); got != "./" {
		t.Fatalf("Location = %q", got)
	}
}

// A HEAD has headers and no body, so the branch that only turns on once bytes
// arrive must leave the length the file server worked out intact and claim no
// compression.
func TestHeadKeepsItsLength(t *testing.T) {
	r := httptest.NewRequest(http.MethodHead, "/icons/icon-192.png", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	static(build).ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("code %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q for a head with nothing to compress", got)
	}
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(len(testPNGBytes)); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("a HEAD sent %d bytes of body", rec.Body.Len())
	}
}
