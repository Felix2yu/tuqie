// static.go shapes how the embedded frontend is answered. Go's file server
// supplies none of this and the published container has no proxy in front of it
// to add it: the shell that names a build has to be re-asked so a deploy is
// noticed, the files that carry their own hash in their name may be kept forever,
// and the 250KB of JavaScript the first visit pays for is under a third of that
// once compressed.

package server

import (
	"compress/gzip"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

func static(fsys fs.FS) http.Handler {
	return compress(controls(http.FileServer(http.FS(fsys))))
}

// controls sets how long each kind of file may be held. What sits under /assets
// is named after its own contents, so a stored copy of one cannot grow stale.
// index.html, sw.js and the manifest keep one name across every build and are
// what decides which build a visitor gets, so they are asked again each time —
// a heuristically cached sw.js in particular is a deploy the browser will not
// look at for a while.
//
// private rather than public because -password puts these behind a shared
// secret: a cache in front of such an instance must not hand the app to whoever
// arrives without it.
func controls(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// compress gzips a text response. The pictures this server sends are already
// compressed, so passing them through a second one costs time and buys nothing.
func compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Accept-Encoding")
		// A ranged read is served as a slice of the stored bytes, which a
		// compressed body would no longer be.
		if !acceptsGzip(r) || r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}
		gz := &gzipWriter{ResponseWriter: w}
		next.ServeHTTP(gz, r)
		gz.finish()
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, token := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, _, _ := strings.Cut(token, ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			return true
		}
	}
	return false
}

// gzipWriter holds the status line back until the first byte arrives, since that
// is the first moment there is a body to say anything about. An empty one — a
// redirect, a HEAD — must not go out claiming a compression it never applied.
type gzipWriter struct {
	http.ResponseWriter
	code    int
	hasCode bool
	started bool
	zip     *gzip.Writer
}

func (g *gzipWriter) WriteHeader(code int) {
	if !g.hasCode {
		g.code, g.hasCode = code, true
	}
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	if !g.started {
		if !g.hasCode {
			g.code = http.StatusOK
		}
		g.start()
	}
	if g.zip != nil {
		return g.zip.Write(p)
	}
	return g.ResponseWriter.Write(p)
}

// start is where the two header sets part: a text body gets its declared length
// taken away, because the length the file server worked out is the uncompressed
// one, and gets the encoding the bytes will now carry.
func (g *gzipWriter) start() {
	g.started = true
	h := g.Header()
	if g.code >= 200 && g.code < 300 && compressible(h.Get("Content-Type")) {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		g.zip = gzip.NewWriter(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(g.code)
}

// finish closes the stream: the footer is part of the body, and a gzipped
// response without it is a truncated file to whatever reads it.
func (g *gzipWriter) finish() {
	if g.zip != nil {
		if err := g.zip.Close(); err != nil {
			log.Printf("gzip: %v", err)
		}
		return
	}
	if !g.started && g.hasCode {
		g.ResponseWriter.WriteHeader(g.code)
	}
}

func compressible(contentType string) bool {
	base, _, _ := strings.Cut(contentType, ";")
	base = strings.ToLower(strings.TrimSpace(base))
	switch {
	case base == "", strings.HasPrefix(base, "image/"):
		return false
	case strings.HasPrefix(base, "text/"),
		base == "application/json",
		strings.HasSuffix(base, "+json"),
		strings.HasSuffix(base, "/javascript"),
		strings.HasSuffix(base, "/ecmascript"):
		return true
	}
	return false
}
