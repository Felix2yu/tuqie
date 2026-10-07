package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"tuqie/internal/axis"
	"tuqie/internal/exif"
	"tuqie/internal/store"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jxl"
)

func TestBandsFromCuts(t *testing.T) {
	got := BandsFromCuts(axis.Y, []int{400, 100, 100, 0, 900, -5, 600}, 800, 600)
	want := [][2]int{{0, 100}, {100, 400}, {400, 600}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if BandsFromCuts(axis.Y, nil, 800, 600) != nil {
		t.Fatal("no cuts should produce no bands")
	}
	if BandsFromCuts(axis.Y, []int{700}, 800, 600) != nil {
		t.Fatal("out-of-range cut should be dropped")
	}
	// Along X the same list is measured against the width, not the height.
	if BandsFromCuts(axis.X, []int{700}, 800, 600) == nil {
		t.Fatal("a cut inside the width should survive on the X axis")
	}
	got = BandsFromCuts(axis.X, []int{100}, 800, 600)
	want = [][2]int{{0, 100}, {100, 800}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("X axis got %v, want %v", got, want)
	}
}

// ---- HTTP surface ----

func newHandler(t *testing.T) (http.Handler, *store.Store) {
	return newHandlerConfig(t, Config{})
}

func newHandlerConfig(t *testing.T, cfg Config) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	fsys := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>tuqie</html>")}}
	return New(st, fsys, cfg).Handler(), st
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func postMultipart(t *testing.T, handler http.Handler, path, field, filename string, data []byte, extra ...string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	// extra carries further form fields as key, value pairs.
	for i := 1; i < len(extra); i += 2 {
		if err := mw.WriteField(extra[i-1], extra[i]); err != nil {
			t.Fatal(err)
		}
	}
	if field != "" {
		fw, err := mw.CreateFormFile(field, filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	} else if err := mw.WriteField("note", "no file here"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var e map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body %q: %v", rec.Body.String(), err)
	}
	return e["error"]
}

func upload(t *testing.T, handler http.Handler, data []byte) analyzeResp {
	t.Helper()
	return uploadNamed(t, handler, "shot.png", data)
}

func uploadNamed(t *testing.T, handler http.Handler, name string, data []byte, extra ...string) analyzeResp {
	t.Helper()
	rec := postMultipart(t, handler, "/api/analyze", "file", name, data, extra...)
	if rec.Code != http.StatusOK {
		t.Fatalf("analyze %d: %s", rec.Code, rec.Body.String())
	}
	var res analyzeResp
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func TestHealth(t *testing.T) {
	handler, _ := newHandler(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Fatalf("body %v", body)
	}
}

func TestAnalyzeReturnsJSONAndImageRoundTrips(t *testing.T) {
	handler, _ := newHandler(t)
	data := testPNG(t, 12, 30)
	res := upload(t, handler, data)

	if res.ID == "" || res.Width != 12 || res.Height != 30 {
		t.Fatalf("meta wrong: %+v", res)
	}
	if res.Filename != "shot.png" || res.Mime != "image/png" {
		t.Fatalf("meta wrong: %+v", res)
	}
	if res.Axis != "y" && res.Axis != "x" {
		t.Fatalf("axis %q", res.Axis)
	}
	if res.URL != "/api/image?id="+res.ID {
		t.Fatalf("url %q", res.URL)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, res.URL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("image %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), data) {
		t.Fatal("served bytes differ from upload")
	}
}

func TestAnalyzeRejects(t *testing.T) {
	handler, _ := newHandler(t)

	rec := postMultipart(t, handler, "/api/analyze", "", "", nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(decodeError(t, rec), "file") {
		t.Fatalf("missing file field: %d %s", rec.Code, rec.Body.String())
	}

	rec = postMultipart(t, handler, "/api/analyze", "file", "notes.txt", []byte("hello"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad extension: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader("not a multipart body")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("garbage body: %d", rec.Code)
	}
}

func TestImageErrorsWhenMissingOrGone(t *testing.T) {
	handler, st := newHandler(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/image?id=unknown", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", rec.Code)
	}

	p, err := st.Put(bytes.NewReader(testPNG(t, 4, 4)), "shot.png", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(p.Path)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/image?id="+p.ID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted file: %d", rec.Code)
	}
}

func TestSliceServesBands(t *testing.T) {
	handler, _ := newHandler(t)
	res := upload(t, handler, testPNG(t, 12, 30))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/slice?id="+res.ID+"&axis=y&from=4&to=19&index=2&format=png&quality=80", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("slice %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != `inline; filename="piece-03.png"` {
		t.Fatalf("disposition %q", got)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if r := img.Bounds(); r.Dx() != 12 || r.Dy() != 15 {
		t.Fatalf("slice bounds %v, want 12x15", r)
	}

	// Defaults: no format/quality means JPEG, missing index means piece-01.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/slice?id="+res.ID+"&axis=y&from=0&to=30", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("default slice %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("default content-type %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `inline; filename="piece-01.jpg"` {
		t.Fatalf("default disposition %q", got)
	}
}

func TestSliceServesTheContainerFormats(t *testing.T) {
	handler, _ := newHandler(t)
	res := upload(t, handler, testPNG(t, 12, 30))

	for _, tc := range []struct{ format, mime string }{
		{"heic", "image/heic"},
		{"avif", "image/avif"},
		{"jxl", "image/jxl"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/api/slice?id="+res.ID+"&axis=y&from=4&to=19&format="+tc.format+"&quality=90", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.format, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != tc.mime {
			t.Errorf("%s content-type %q, want %q", tc.format, got, tc.mime)
		}
		want := `inline; filename="piece-01.` + tc.format + `"`
		if got := rec.Header().Get("Content-Disposition"); got != want {
			t.Errorf("disposition %q, want %q", got, want)
		}
		img, err := decodeAs(tc.format, rec.Body.Bytes())
		if err != nil {
			t.Errorf("%s body: %v", tc.format, err)
			continue
		}
		if r := img.Bounds(); r.Dx() != 12 || r.Dy() != 15 {
			t.Errorf("%s bounds %v, want 12x15", tc.format, r)
		}
	}

	// The ZIP names an entry by what it actually holds, so the album or the file
	// manager on the other end sees .jxl rather than a .jpg that is not one.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/export",
		strings.NewReader(`{"id":"`+res.ID+`","axis":"y","cuts":[15],"format":"avif"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("export %d: %s", rec.Code, rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("entries %d, want 2", len(zr.File))
	}
	for i, f := range zr.File {
		want := fmt.Sprintf("shot-%02d.avif", i+1)
		if f.Name != want {
			t.Errorf("entry %d is %q, want %q", i, f.Name, want)
		}
	}
}

// decodeAs reads a slice back with the decoder belonging to its format.
func decodeAs(format string, body []byte) (image.Image, error) {
	r := bytes.NewReader(body)
	switch format {
	case "heic":
		return heic.Decode(r)
	case "avif":
		return avif.Decode(r)
	}
	return jxl.Decode(r)
}

func TestSliceRejectsBadRequests(t *testing.T) {
	handler, st := newHandler(t)
	p, err := st.Put(bytes.NewReader(testPNG(t, 8, 8)), "shot.png", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/slice?id=" + p.ID + "&axis=y"

	cases := []struct {
		query string
		want  int
	}{
		{base + "&from=4", http.StatusBadRequest},       // to missing
		{base + "&from=x&to=4", http.StatusBadRequest},  // from invalid
		{base + "&from=-1&to=4", http.StatusBadRequest}, // from negative
		{base + "&from=4&to=y", http.StatusBadRequest},  // to invalid
		{base + "&from=5&to=5", http.StatusBadRequest},  // empty band
		{base + "&from=0&to=8", http.StatusOK},
		{"/api/slice?id=nope&axis=y&from=0&to=1", http.StatusNotFound},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.query, nil))
		if rec.Code != c.want {
			t.Fatalf("%s -> %d, want %d (%s)", c.query, rec.Code, c.want, rec.Body.String())
		}
	}

	// Decoding a vanished file is a server-side failure, not a bad request.
	p.Release()
	os.Remove(p.Path)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base+"&from=0&to=4", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("lost pixels: %d", rec.Code)
	}
}

func TestExportZipsBands(t *testing.T) {
	handler, _ := newHandler(t)
	res := upload(t, handler, testPNG(t, 10, 30))

	body, err := json.Marshal(map[string]any{
		"id": res.ID, "axis": "y",
		"cuts":   []int{4, 10, 4, -1, 0, 30, 100},
		"format": "png", "quality": 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/export", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("export %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("content-type %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="shot-slices.zip"` {
		t.Fatalf("disposition %q", got)
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := []string{"shot-01.png", "shot-02.png", "shot-03.png"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("entries %v, want %v", names, want)
	}
	f := zr.File[1]
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	img, err := png.Decode(rc)
	if err != nil {
		t.Fatal(err)
	}
	if r := img.Bounds(); r.Dx() != 10 || r.Dy() != 6 {
		t.Fatalf("band 02 bounds %v, want 10x6", r)
	}
}

func TestExportNamesAndSkips(t *testing.T) {
	handler, _ := newHandler(t)
	// Four bands: 0..4, 4..10, 10..20, 20..30.
	res := upload(t, handler, testPNG(t, 10, 30))

	export := func(t *testing.T, payload string) []string {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/export", strings.NewReader(payload)))
		if rec.Code != http.StatusOK {
			t.Fatalf("export %d: %s", rec.Code, rec.Body.String())
		}
		zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, f := range zr.File {
			names = append(names, f.Name)
		}
		return names
	}
	req := func(prefix, body string) string {
		return `{"id":"` + res.ID +
			`","axis":"y","cuts":[4,10,20],"format":"png","prefix":"` + prefix + `"` + body + `}`
	}

	got := export(t, req("会话", `,"skip":[0,2],"start":7`))
	if want := []string{"会话-07.png", "会话-08.png"}; !reflect.DeepEqual(got, want) {
		// Skipping must not leave holes behind: the album numbers what it received.
		t.Fatalf("entries %v, want %v", got, want)
	}
	// The digit width follows the last number, so a long run still sorts by name.
	got = export(t, req("会话", `,"start":98`))
	if want := []string{"会话-098.png", "会话-099.png", "会话-100.png", "会话-101.png"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	// Numbering from zero is a choice, not a missing field.
	got = export(t, req("会话", `,"start":0`))
	if want := []string{"会话-00.png", "会话-01.png", "会话-02.png", "会话-03.png"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	// A band index that no longer exists is ignored, not an error.
	got = export(t, req("会话", `,"skip":[9,-1]`))
	if want := []string{"会话-01.png", "会话-02.png", "会话-03.png", "会话-04.png"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	for _, name := range export(t, req("../../etc/passwd", "")) {
		if strings.Contains(name, "..") || strings.Contains(name, "/") {
			t.Fatalf("entry %q escapes the archive root", name)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/export",
		strings.NewReader(req("会话", `,"skip":[0,1,2,3]`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("excluding everything: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSafePrefix(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"会话截图", "会话截图"},
		{"  spaced  ", "spaced"},
		{`a/b\c:d`, "a_b_c_d"},
		{`quote"name`, "quote_name"},
		{"tab\tnewline\n", "tab_newline"},
		{"../../etc/passwd", "___etc_passwd"},
		{"trailing...", "trailing"},
		{".", ""},
		{"", ""},
		{strings.Repeat("长", 100), strings.Repeat("长", 80)},
	} {
		if got := safePrefix(tc.in); got != tc.want {
			t.Errorf("safePrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNamePad(t *testing.T) {
	for _, tc := range []struct {
		start, count, want int
	}{
		{1, 1, 2},
		{1, 99, 2},
		{1, 999, 3},
		{98, 4, 3},
		{0, 1, 2},
		{1000, 1, 4},
	} {
		if got := namePad(tc.start, tc.count); got != tc.want {
			t.Errorf("namePad(%d,%d) = %d, want %d", tc.start, tc.count, got, tc.want)
		}
	}
}

func TestExportRejectsBadRequests(t *testing.T) {
	handler, st := newHandler(t)
	p, err := st.Put(bytes.NewReader(testPNG(t, 8, 8)), "shot.png", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	post := func(payload string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/export", strings.NewReader(payload)))
		return rec
	}

	if rec := post("{not json"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", rec.Code)
	}
	if rec := post(`{"id":"nope","axis":"y","cuts":[4]}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", rec.Code)
	}
	if rec := post(`{"id":"` + p.ID + `","axis":"y","cuts":[]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no bands: %d %s", rec.Code, rec.Body.String())
	}

	p.Release()
	os.Remove(p.Path)
	if rec := post(`{"id":"` + p.ID + `","axis":"y","cuts":[4],"format":"jpeg"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("lost pixels: %d", rec.Code)
	}
}

func TestStaticFrontend(t *testing.T) {
	handler, _ := newHandler(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("static %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "tuqie") {
		t.Fatalf("body %q", body)
	}
}

// ---- failure paths ----

// failWriter stands in for a client that hangs up mid-response.
type failWriter struct {
	header http.Header
	writes int
}

func (f *failWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}

func (f *failWriter) Write(p []byte) (int, error) {
	f.writes++
	return 0, os.ErrClosed
}

func (f *failWriter) WriteHeader(int) {}

func TestAnalyzeRejectsUndecodableUpload(t *testing.T) {
	handler, _ := newHandler(t)
	// A GIF header is enough for the dimension probe, so Put takes it and the
	// full decode is where it falls apart.
	header := []byte("GIF89a" + string([]byte{8, 0, 8, 0, 0, 0, 0}))
	rec := postMultipart(t, handler, "/api/analyze", "file", "broken.gif", header)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("undecodable gif: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(decodeError(t, rec), "decode") {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestImageSurvivesADroppedClient(t *testing.T) {
	st, err := store.New(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, fstest.MapFS{}, Config{})

	p, err := st.Put(bytes.NewReader(testPNG(t, 8, 8)), "shot.png", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	w := &failWriter{}
	req := httptest.NewRequest(http.MethodGet, "/api/image?id="+p.ID, nil)
	s.handleImage(w, req) // must log and return, not panic
	if w.writes == 0 {
		t.Fatal("nothing was written, so the copy never ran")
	}
}

func TestExportNamesFilesAfterTheScreenshotWithoutAStem(t *testing.T) {
	handler, st := newHandler(t)
	p, err := st.Put(bytes.NewReader(testPNG(t, 10, 20)), ".png", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"id": p.ID, "axis": "y", "cuts": []int{8}, "format": "png"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/export", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("export %d: %s", rec.Code, rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := []string{"screenshot-01.png", "screenshot-02.png"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("entries %v, want %v", names, want)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="screenshot-slices.zip"` {
		t.Fatalf("disposition %q", got)
	}
}

func TestExportSurvivesADroppedClient(t *testing.T) {
	st, err := store.New(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, fstest.MapFS{}, Config{})

	// One band has to be big enough that the zip flushes while writing it,
	// which is when the dead client shows up.
	p, err := st.Put(bytes.NewReader(testPNG(t, 200, 2000)), "shot.png", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"id": p.ID, "axis": "y", "cuts": []int{100}, "format": "png",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &failWriter{}
	req := httptest.NewRequest(http.MethodPost, "/api/export", bytes.NewReader(body))
	s.handleExport(w, req) // must log and return, not panic
	if w.writes == 0 {
		t.Fatal("the zip never reached the writer")
	}
}

func TestWriteJSONReportsUnencodableValues(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]any{"ch": make(chan int)})
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}
	// The failure is logged, not returned; the caller has already committed 200.
	if w.Body.String() != "" {
		t.Fatalf("body %q", w.Body.String())
	}
}

func TestPickFormatAndHelpers(t *testing.T) {
	cases := []struct {
		raw, rawQ string
		format    string
		quality   int
	}{
		{"png", "80", "png", 80},
		{"jpeg", "150", "jpeg", 92}, // out of range falls back
		{"jpeg", "49", "jpeg", 92},  // below the floor falls back
		{"jpeg", "", "jpeg", 92},    // absent falls back
		{"webp", "abc", "jpeg", 92}, // anything not png is jpeg
		{"", "100", "jpeg", 100},
	}
	for _, c := range cases {
		f, q := pickFormat(c.raw, c.rawQ)
		if f != c.format || q != c.quality {
			t.Errorf("pickFormat(%q,%q) = (%q,%d), want (%q,%d)", c.raw, c.rawQ, f, q, c.format, c.quality)
		}
	}

	if got := mimeFor("png"); got != "image/png" {
		t.Errorf("mimeFor(png) = %q", got)
	}
	if got := mimeFor("jpg"); got != "image/jpeg" {
		t.Errorf("mimeFor(jpg) = %q", got)
	}

	for _, bad := range []string{"", "x", "-1"} {
		if _, err := atoi(bad); err == nil {
			t.Errorf("atoi(%q) should fail", bad)
		}
	}
	if n, err := atoi("42"); err != nil || n != 42 {
		t.Errorf("atoi(42) = %d, %v", n, err)
	}
}

// ---- capture dates ----

// datedJPEG returns a small JPEG carrying taken as its capture date.
func datedJPEG(t *testing.T, w, h int, taken exif.Date) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return exif.InjectJPEG(buf.Bytes(), taken.App1())
}

func exportZip(t *testing.T, handler http.Handler, id string, cuts []int, format string) *zip.Reader {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id": id, "axis": "y", "cuts": cuts, "format": format, "quality": 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/export", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("export %d: %s", rec.Code, rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestSliceKeepsTheSourceDate(t *testing.T) {
	handler, _ := newHandler(t)
	taken := exif.Date{Wall: "2026:10:07 09:15:30", Subsec: "482", Offset: "+08:00"}
	res := uploadNamed(t, handler, "shot.jpg", datedJPEG(t, 12, 30, taken))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/slice?id="+res.ID+"&axis=y&from=4&to=19&format=jpeg&quality=90", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("slice %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("dated slice no longer decodes: %v", err)
	}
	if got := exif.Scan(rec.Body.Bytes()); got != taken {
		t.Fatalf("slice date = %+v, want %+v", got, taken)
	}

	// PNG output carries the same date in an eXIf chunk, so the format choice does
	// not decide whether the album shows the right day.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/slice?id="+res.ID+"&axis=y&from=4&to=19&format=png", nil))
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("dated PNG slice no longer decodes: %v", err)
	}
	if got := exif.Scan(rec.Body.Bytes()); got != taken {
		t.Fatalf("PNG slice date = %+v, want %+v", got, taken)
	}
}

func TestSliceOfUndatedUploadStaysClean(t *testing.T) {
	handler, _ := newHandler(t)
	res := upload(t, handler, testPNG(t, 12, 30))
	for _, format := range []string{"jpeg", "png"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/api/slice?id="+res.ID+"&axis=y&from=0&to=30&format="+format, nil))
		if d := exif.Scan(rec.Body.Bytes()); d.Valid() {
			t.Fatalf("an undated source produced %+v as %s", d, format)
		}
	}
}

func TestAnalyzeFallsBackToTheReportedFileTime(t *testing.T) {
	handler, _ := newHandler(t)
	when := time.Date(2026, 10, 5, 21, 4, 9, 0, time.Local)
	res := uploadNamed(t, handler, "shot.png", testPNG(t, 12, 30),
		"lastModified", strconv.FormatInt(when.UnixMilli(), 10))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/slice?id="+res.ID+"&axis=y&from=0&to=30&format=jpeg", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("slice %d: %s", rec.Code, rec.Body.String())
	}
	if got, want := exif.Scan(rec.Body.Bytes()), exif.FromTime(when); got != want {
		t.Fatalf("slice date = %+v, want %+v", got, want)
	}

	zr := exportZip(t, handler, res.ID, []int{10, 20}, "png")
	if len(zr.File) != 3 {
		t.Fatalf("entries = %d, want 3", len(zr.File))
	}
	for _, f := range zr.File {
		if !f.Modified.Equal(when) {
			t.Fatalf("entry %s stamped %s, want %s", f.Name, f.Modified, when)
		}
	}
}

func TestAnalyzeReportsTheCaptureDate(t *testing.T) {
	handler, _ := newHandler(t)
	taken := exif.Date{Wall: "2026:10:07 09:15:30", Offset: "+08:00"}
	res := uploadNamed(t, handler, "shot.jpg", datedJPEG(t, 12, 30, taken))
	when, err := taken.Time()
	if err != nil {
		t.Fatal(err)
	}
	if res.Taken != when.UnixMilli() {
		t.Fatalf("taken = %d, want %d", res.Taken, when.UnixMilli())
	}

	if res := uploadNamed(t, handler, "shot.png", testPNG(t, 12, 30)); res.Taken != 0 {
		t.Fatalf("an undated upload reported %d", res.Taken)
	}
}

func TestAnalyzeIgnoresAnUnusableReportedFileTime(t *testing.T) {
	handler, _ := newHandler(t)
	// Browsers report 0 when they cannot tell, and this is untrusted input, so
	// anything that is not a plain timestamp leaves the upload undated.
	for _, value := range []string{"", "0", "-1", "abc", "1e9", "99999999999999999999", "1775000000"} {
		res := uploadNamed(t, handler, "shot.png", testPNG(t, 12, 30), "lastModified", value)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/api/slice?id="+res.ID+"&axis=y&from=0&to=30&format=jpeg", nil))
		if d := exif.Scan(rec.Body.Bytes()); d.Valid() {
			t.Fatalf("lastModified %q produced %+v", value, d)
		}
	}
}

func TestUploadTime(t *testing.T) {
	form := &url.Values{}
	form.Set("lastModified", "1791680679000")
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	got := uploadTime(req)
	if want := time.UnixMilli(1791680679000); !got.Equal(want) {
		t.Fatalf("uploadTime = %s, want %s", got, want)
	}
	if got := uploadTime(httptest.NewRequest(http.MethodPost, "/api/analyze", nil)); !got.IsZero() {
		t.Fatalf("missing field = %s", got)
	}
}

func TestExportStampsEntriesWithTheSourceDate(t *testing.T) {
	handler, _ := newHandler(t)
	taken := exif.Date{Wall: "2026:10:07 09:15:30", Offset: "+08:00"}
	res := uploadNamed(t, handler, "shot.jpg", datedJPEG(t, 10, 30, taken))

	zr := exportZip(t, handler, res.ID, []int{10, 20}, "jpeg")
	if len(zr.File) != 3 {
		t.Fatalf("entries %d, want 3", len(zr.File))
	}
	for _, f := range zr.File {
		// A zip entry holds a zone-less wall clock, so what has to survive the round
		// trip is the camera's own 09:15:30, whichever zone this process runs in.
		if got := f.Modified.Format("2006-01-02 15:04:05"); got != "2026-10-07 09:15:30" {
			t.Errorf("%s modified %s, want the source wall clock", f.Name, got)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if d := exif.Scan(data); d.Wall != taken.Wall {
			t.Errorf("%s carries %+v, want %s", f.Name, d, taken.Wall)
		}
	}
}

func TestExportWithoutSourceDateFallsBackToNow(t *testing.T) {
	// An upload that never carried a date keeps the old behaviour: the archive
	// writer stamps entries with the export time.
	start := time.Now().Add(-2 * time.Second)
	handler, _ := newHandler(t)
	res := upload(t, handler, testPNG(t, 10, 30))

	for _, f := range exportZip(t, handler, res.ID, []int{10}, "png").File {
		if f.Modified.Before(start) {
			t.Errorf("%s modified %s, want the export time", f.Name, f.Modified)
		}
	}
}

func TestEntryTimeBounds(t *testing.T) {
	if _, ok := entryTime(exif.Date{}); ok {
		t.Fatal("no date should leave the entry unstamped")
	}
	if _, ok := entryTime(exif.Date{Wall: "1979:12:31 23:59:59"}); ok {
		t.Fatal("DOS timestamps cannot hold a pre-1980 date")
	}
	when, ok := entryTime(exif.Date{Wall: "1980:01:01 00:00:00"})
	if !ok || when.Year() != 1980 {
		t.Fatalf("1980 should be stampable, got %v, %v", when, ok)
	}
}

func TestPreviewShrinksABigUpload(t *testing.T) {
	handler, st := newHandler(t)
	res := upload(t, handler, testPNG(t, 3000, 3000))
	if !strings.HasPrefix(res.URL, "/api/preview") {
		t.Fatalf("url = %q, want the preview endpoint", res.URL)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, res.URL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type %q, want image/jpeg", got)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" || cfg.Width != 2000 || cfg.Height != 2000 {
		t.Fatalf("preview is %s %dx%d, want jpeg 2000x2000", format, cfg.Width, cfg.Height)
	}

	p, ok := st.Get(res.ID)
	if !ok {
		t.Fatal("upload vanished")
	}
	if _, err := os.Stat(p.PreviewPath()); err != nil {
		t.Fatalf("rendered preview was not cached: %v", err)
	}
}

func TestPreviewFallsBackToTheOriginal(t *testing.T) {
	handler, _ := newHandler(t)
	data := testPNG(t, 60, 40)
	res := upload(t, handler, data)
	if want := "/api/image?id=" + res.ID; res.URL != want {
		t.Fatalf("url = %q, want %q", res.URL, want)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/preview?id="+res.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type %q, want the original image/png", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), data) {
		t.Fatal("a picture that needs no shrinking should be served untouched")
	}
}

// heicBytes is a small HEIC, the kind an iPhone writes for every photo it takes.
func heicBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := heic.Encode(&buf, img, heic.EncodeOptions{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAFormatTheBrowserCannotDrawGetsARendition(t *testing.T) {
	handler, _ := newHandler(t)
	res := uploadNamed(t, handler, "photo.heic", heicBytes(t, 60, 40))

	if res.Mime != "image/heic" {
		t.Fatalf("mime %q, want image/heic", res.Mime)
	}
	if res.Width != 60 || res.Height != 40 {
		t.Fatalf("dims %dx%d, want 60x40", res.Width, res.Height)
	}
	// Small enough to hand over whole, and still no JPEG the browser can show, so
	// the working view comes from a rendition made at the original size.
	if want := "/api/preview?id=" + res.ID; res.URL != want {
		t.Fatalf("url = %q, want %q", res.URL, want)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, res.URL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type %q, want image/jpeg", got)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" || cfg.Width != 60 || cfg.Height != 40 {
		t.Fatalf("rendition is %s %dx%d, want jpeg 60x40", format, cfg.Width, cfg.Height)
	}

	// The slices still come out of the container's own pixels, not the rendition.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/slice?id="+res.ID+"&axis=y&from=0&to=30&index=0&format=png&quality=80", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("slice: %d %s", rec.Code, rec.Body.String())
	}
	sliced, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatalf("slice did not decode: %v", err)
	}
	if b := sliced.Bounds(); b.Dx() != 60 || b.Dy() != 30 {
		t.Fatalf("slice is %dx%d, want 60x30", b.Dx(), b.Dy())
	}
}

func TestOnlyWhatBrowsersDrawStaysOriginal(t *testing.T) {
	for _, tc := range []struct {
		mime string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/avif", true},
		{"image/heic", false},
		{"image/heif", false},
		{"image/jxl", false},
		{"application/octet-stream", false},
	} {
		if got := drawsInline(tc.mime); got != tc.want {
			t.Errorf("drawsInline(%q) = %v, want %v", tc.mime, got, tc.want)
		}
	}
}
