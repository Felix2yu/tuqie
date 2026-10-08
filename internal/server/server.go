// Package server wires the HTTP API and the embedded frontend together.
package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"tuqie/internal/axis"
	"tuqie/internal/detect"
	"tuqie/internal/exif"
	"tuqie/internal/preview"
	"tuqie/internal/split"
	"tuqie/internal/store"
)

const maxUpload = 250 << 20

// Config is how the server starts up. Its zero value is the tool as it has
// always behaved: open, and with no ceiling on uploads.
type Config struct {
	// Password, when set, is asked of every request through the browser's own
	// basic-auth prompt. Empty leaves the instance open to whoever can reach it.
	Password string
	// UploadsPerMinute is the sustained rate one client may post new screenshots,
	// with a short burst on top. Zero switches the ceiling off.
	UploadsPerMinute int
}

type Server struct {
	store    *store.Store
	web      fs.FS
	password string
	budget   *uploadBudget
}

func New(s *store.Store, web fs.FS, cfg Config) *Server {
	return &Server{store: s, web: web, password: cfg.Password, budget: newUploadBudget(cfg.UploadsPerMinute)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/analyze", s.handleAnalyze)
	mux.HandleFunc("GET /api/image", s.handleImage)
	mux.HandleFunc("GET /api/preview", s.handlePreview)
	mux.HandleFunc("GET /api/slice", s.handleSlice)
	mux.HandleFunc("POST /api/export", s.handleExport)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.Handle("/", http.FileServer(http.FS(s.web)))

	var h http.Handler = mux
	h = s.limitUploads(h)
	if s.password != "" {
		h = requirePassword(s.password, h)
	}
	return &logged{next: h}
}

type analyzeResp struct {
	ID         string             `json:"id"`
	Filename   string             `json:"filename"`
	Mime       string             `json:"mime"`
	Width      int                `json:"width"`
	Height     int                `json:"height"`
	Axis       string             `json:"axis"`
	URL        string             `json:"url"`
	Candidates []detect.Candidate `json:"candidates"`
	Signal     detect.Signal      `json:"signal"`
	// Taken is the capture date the slices will carry, as an epoch in milliseconds.
	Taken int64 `json:"taken,omitempty"`
}

// takenMillis is that date for the browser, which needs an epoch to hand the share
// sheet a File whose timestamp is the shot rather than the moment it was saved.
func takenMillis(taken exif.Date) int64 {
	when, err := taken.Time()
	if err != nil {
		return 0
	}
	return when.UnixMilli()
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "上传失败：文件过大或表单格式不正确")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少 file 字段")
		return
	}
	defer file.Close()

	p, err := s.store.Put(file, header.Filename, uploadTime(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	img, err := p.Image()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	start := time.Now()
	res := detect.Analyze(img, detect.DefaultOptions())
	log.Printf("analyze: %s %dx%d axis=%s -> %d candidates in %s", p.ID, p.Width, p.Height, res.Axis, len(res.Candidates), time.Since(start))

	// The browser works from the small copy when there is one; the slices it
	// exports still come from the original pixels.
	imgURL := "/api/image?id=" + p.ID
	if _, _, needed := previewFor(p); needed {
		imgURL = "/api/preview?id=" + p.ID
	}

	writeJSON(w, analyzeResp{
		ID:         p.ID,
		Filename:   p.Filename,
		Mime:       p.Mime,
		Width:      p.Width,
		Height:     p.Height,
		Axis:       res.Axis.String(),
		URL:        imgURL,
		Candidates: res.Candidates,
		Signal:     res.Signal,
		Taken:      takenMillis(p.Taken),
	})
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.picture(w, r)
	if !ok {
		return
	}
	serveFile(w, p.Path, p.Mime)
}

// previewFor decides whether the browser works from a JPEG rendition: the upload
// is either too big to hand over whole, or a format too many browsers fail to
// draw in an <img> tag to be shown as it is.
func previewFor(p *store.Picture) (width, height int, needed bool) {
	width, height, big := preview.Fit(p.Width, p.Height)
	return width, height, big || !drawsInline(p.Mime)
}

// drawsInline lists what a bare <img> shows in every current browser. HEIF needs
// an OS decoder a given Chrome may not reach for, and JPEG XL support keeps
// being added and reverted, so both are drawn from the rendition instead.
func drawsInline(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif":
		return true
	}
	return false
}

// handlePreview serves the downscaled copy the working view is drawn from. It is
// rendered on the first request and kept beside the upload, so a 120MP stitch
// costs the browser one small JPEG instead of its whole pixel buffer. Pictures
// that already fit the budget, and a render that cannot be written, fall back to
// the original bytes.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	p, ok := s.picture(w, r)
	if !ok {
		return
	}
	width, height, needed := previewFor(p)
	file := p.PreviewPath()
	if needed && !exists(file) {
		if err := writePreview(file, p, width, height); err != nil {
			log.Printf("preview: %s: %v", p.ID, err)
		}
	}
	if needed && exists(file) {
		serveFile(w, file, "image/jpeg")
		return
	}
	serveFile(w, p.Path, p.Mime)
}

// writePreview renders through a temporary file so two first requests cannot
// leave a half-written preview behind.
func writePreview(file string, p *store.Picture, width, height int) error {
	img, err := p.Image()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "preview-*.jpg")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := preview.Encode(tmp, img, width, height); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}

func serveFile(w http.ResponseWriter, file, mime string) {
	f, err := os.Open(file)
	if err != nil {
		writeErr(w, http.StatusNotFound, "原图已过期，请重新上传")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=600")
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("image: %v", err)
	}
}

func exists(file string) bool {
	_, err := os.Stat(file)
	return err == nil
}

// handleSlice serves one band as an image the browser can turn into a File.
func (s *Server) handleSlice(w http.ResponseWriter, r *http.Request) {
	p, ok := s.picture(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	ax := axis.Parse(q.Get("axis"))
	from, err := atoi(q.Get("from"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "from 无效")
		return
	}
	to, err := atoi(q.Get("to"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "to 无效")
		return
	}
	format, quality := pickFormat(q.Get("format"), q.Get("quality"))
	index := 0
	if v, err := strconv.Atoi(q.Get("index")); err == nil && v >= 0 {
		index = v
	}

	img, err := p.Image()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	crop, err := split.Slice(img, ax, from, to)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var buf bytes.Buffer
	if err := split.Encode(&buf, crop, format, quality, p.Taken); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ext := split.Ext(format)
	w.Header().Set("Content-Type", mimeFor(ext))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="piece-%02d.%s"`, index+1, ext))
	w.Header().Set("Cache-Control", "private, max-age=600")
	w.Write(buf.Bytes())
}

type exportReq struct {
	ID     string `json:"id"`
	Axis   string `json:"axis"`
	Cuts   []int  `json:"cuts"`
	Skip   []int  `json:"skip"`
	Prefix string `json:"prefix"`
	// Start is a pointer so "number these from zero" stays distinguishable from a
	// request that never mentioned numbering.
	Start   *int   `json:"start"`
	Format  string `json:"format"`
	Quality int    `json:"quality"`
}

// safePrefix keeps a typed-in name from turning into a path: zip entry names are
// read back by extractors that follow "../" without complaining.
func safePrefix(raw string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '"' || r < 0x20:
			return '_'
		}
		return r
	}, strings.TrimSpace(raw))
	// The ends are stripped of dots before the ".." sweep, otherwise
	// "会话..." survives as "会话_" and the exported name reads "会话_-01.png".
	s = strings.ReplaceAll(strings.Trim(s, ". "), "..", "_")
	if runes := []rune(s); len(runes) > 80 {
		s = string(runes[:80])
	}
	return strings.Trim(s, ". ")
}

// namePad is the digit width the whole run needs: two at minimum, wider once the
// numbers outgrow that, so a 120-piece export sorts correctly on disk.
func namePad(start, count int) int {
	last := start
	if n := start + count - 1; n > last {
		last = n
	}
	if n := len(strconv.Itoa(last)); n > 2 {
		return n
	}
	return 2
}

// handleExport streams all bands as one zip, for desktop users.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var req exportReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	p, ok := s.store.Get(req.ID)
	if !ok {
		writeErr(w, http.StatusNotFound, "图片已过期，请重新上传")
		return
	}
	img, err := p.Image()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ax := axis.Parse(req.Axis)
	bands := BandsFromCuts(ax, req.Cuts, p.Width, p.Height)
	if len(bands) == 0 {
		writeErr(w, http.StatusBadRequest, "还没有可用的切割范围")
		return
	}
	skip := make(map[int]bool, len(req.Skip))
	for _, i := range req.Skip {
		skip[i] = true
	}
	kept := make([][2]int, 0, len(bands))
	for i, b := range bands {
		if !skip[i] {
			kept = append(kept, b)
		}
	}
	if len(kept) == 0 {
		writeErr(w, http.StatusBadRequest, "所有切片都被排除了")
		return
	}
	format, quality := pickFormat(req.Format, strconv.Itoa(req.Quality))

	base := safePrefix(req.Prefix)
	if base == "" {
		base = strings.TrimSuffix(path.Base(p.Filename), path.Ext(p.Filename))
	}
	if base == "" {
		base = "screenshot"
	}
	start := 1
	if req.Start != nil {
		start = max(*req.Start, 0)
	}
	pad := namePad(start, len(kept))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-slices.zip"`, base))

	zw := zip.NewWriter(w)
	var written int
	for i, b := range kept {
		crop, err := split.Slice(img, ax, b[0], b[1])
		if err != nil {
			log.Printf("export: skip band %d: %v", i, err)
			continue
		}
		var buf bytes.Buffer
		if err := split.Encode(&buf, crop, format, quality, p.Taken); err != nil {
			log.Printf("export: skip band %d: %v", i, err)
			continue
		}
		fh := &zip.FileHeader{
			Name:     fmt.Sprintf("%s-%0*d.%s", base, pad, start+i, split.Ext(format)),
			Method:   zip.Deflate,
			Modified: time.Now(),
		}
		// CreateHeader writes a zero Modified as a 1979 date, so the fallback above
		// is what keeps undated exports stamped with the export time.
		if when, ok := entryTime(p.Taken); ok {
			fh.Modified = when
		}
		fw, err := zw.CreateHeader(fh)
		if err != nil {
			log.Printf("export: %v", err)
			return
		}
		if _, err := fw.Write(buf.Bytes()); err != nil {
			log.Printf("export: write: %v", err)
			return
		}
		written++
	}
	if written == 0 {
		// Nothing has reached the stream yet, so this can still be an honest
		// error rather than an empty archive that downloads as if it worked.
		writeErr(w, http.StatusInternalServerError, "切片一张都没能生成，请换一种格式或重试")
		return
	}
	if err := zw.Close(); err != nil {
		log.Printf("export: close: %v", err)
	}
	log.Printf("export: %s -> %d of %d slices", p.ID, written, len(kept))
}

// BandsFromCuts turns interior cut lines into [start,end) bands along ax.
func BandsFromCuts(ax axis.Axis, cuts []int, width, height int) [][2]int {
	length := ax.Length(width, height)
	seen := map[int]bool{}
	var lines []int
	for _, c := range cuts {
		if c <= 0 || c >= length || seen[c] {
			continue
		}
		seen[c] = true
		lines = append(lines, c)
	}
	sort.Ints(lines)
	if len(lines) == 0 {
		return nil
	}
	bands := make([][2]int, 0, len(lines)+1)
	prev := 0
	for _, c := range lines {
		bands = append(bands, [2]int{prev, c})
		prev = c
	}
	return append(bands, [2]int{prev, length})
}

func (s *Server) picture(w http.ResponseWriter, r *http.Request) (*store.Picture, bool) {
	p, ok := s.store.Get(r.URL.Query().Get("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "图片已过期，请重新上传")
	}
	return p, ok
}

// formats are the outputs a request can name; anything else is treated as the
// default rather than refused, so an older client still gets pictures.
var formats = map[string]string{
	"jpeg": split.FormatJPEG,
	"png":  split.FormatPNG,
	"heic": split.FormatHEIC,
	"avif": split.FormatAVIF,
	"jxl":  split.FormatJXL,
}

func pickFormat(raw, rawQuality string) (string, int) {
	format, ok := formats[raw]
	if !ok {
		format = split.FormatJPEG
	}
	q := 92
	if rawQuality != "" {
		if v, err := strconv.Atoi(rawQuality); err == nil && v >= 50 && v <= 100 {
			q = v
		}
	}
	return format, q
}

// uploadTime is the timestamp the uploader reports for the file itself. Browsers
// leave it out for pasted images, and then there is nothing to stand in.
func uploadTime(r *http.Request) time.Time {
	ms, err := strconv.ParseInt(r.FormValue("lastModified"), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// entryTime is the modification time to stamp zip entries with.
func entryTime(taken exif.Date) (time.Time, bool) {
	if !taken.InZipRange() {
		return time.Time{}, false
	}
	when, err := taken.Time()
	if err != nil {
		return time.Time{}, false
	}
	return when, true
}

// mimeFor labels a slice by the suffix it was cut with.
func mimeFor(ext string) string {
	switch ext {
	case "png":
		return "image/png"
	case "heic":
		return "image/heic"
	case "avif":
		return "image/avif"
	case "jxl":
		return "image/jxl"
	}
	return "image/jpeg"
}

func atoi(v string) (int, error) {
	if v == "" {
		return 0, errors.New("empty")
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New("negative")
	}
	return n, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

type logged struct{ next http.Handler }

func (l *logged) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	l.next.ServeHTTP(w, r)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	}
}
