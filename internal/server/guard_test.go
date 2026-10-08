package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPasswordGate(t *testing.T) {
	handler, _ := newHandlerConfig(t, Config{Password: "sesame"})

	ask := func(path, user, pass string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if pass != "" {
			r.SetBasicAuth(user, pass)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	// A route that reads the store rather than the one route left open.
	rec := ask("/api/image?id=unknown", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("the browser prompt needs a challenge header")
	}
	if rec := ask("/api/image?id=unknown", "anyone", "sesame"); rec.Code == http.StatusUnauthorized {
		t.Fatal("the right password should reach the route")
	}
	if rec := ask("/api/image?id=unknown", "anyone", "sesamo"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	// The frontend is the reason the whole handler is gated, not just the API.
	if rec := ask("/", "u", "sesame"); rec.Code != http.StatusOK {
		t.Fatalf("index with the password: %d", rec.Code)
	}
	if rec := ask("/", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("index without it: %d", rec.Code)
	}
}

// TestHealthStaysReachable is what keeps the container's HEALTHCHECK from reading
// a 401 and calling a running instance unhealthy: wget has no way to answer a
// prompt. The one exception must not become a wider hole, so the route that
// writes to disk is asked about in the same breath.
func TestHealthStaysReachable(t *testing.T) {
	handler, _ := newHandlerConfig(t, Config{Password: "sesame"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous health: %d %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"ok"`) {
		t.Fatalf("health said %q", body)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/analyze", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous upload: %d", rec.Code)
	}
}

func TestUploadsAreMetered(t *testing.T) {
	handler, _ := newHandlerConfig(t, Config{UploadsPerMinute: 6})
	data := testPNG(t, 8, 8)

	for i := range 6 {
		rec := postMultipart(t, handler, "/api/analyze", "file", "shot.png", data)
		if rec.Code != http.StatusOK {
			t.Fatalf("upload %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := postMultipart(t, handler, "/api/analyze", "file", "shot.png", data)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("seventh upload: %d %s", rec.Code, rec.Body.String())
	}
	after, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || after < 1 {
		t.Fatalf("Retry-After = %q, want at least a second", rec.Header().Get("Retry-After"))
	}
	// Only the route that writes to disk is metered.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health after the limit: %d", rec.Code)
	}
}

// fakeClock lets the refill math be tested without sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) after(d time.Duration) { c.t = c.t.Add(d) }

func TestBudgetRefillsSteadily(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	b := &uploadBudget{capacity: 2, perSec: 0.5, now: clock.now, buckets: map[string]*bucket{}, swept: clock.t}

	for i := range 2 {
		if ok, _ := b.allow("10.0.0.1"); !ok {
			t.Fatalf("upload %d should fit in a fresh bucket", i+1)
		}
	}
	ok, wait := b.allow("10.0.0.1")
	if ok || wait != 2*time.Second {
		t.Fatalf("empty bucket: ok=%v wait=%v, want two seconds", ok, wait)
	}

	clock.after(time.Second)
	if ok, _ := b.allow("10.0.0.1"); ok {
		t.Fatal("one second of refill is half an upload")
	}
	clock.after(time.Second)
	if ok, _ := b.allow("10.0.0.1"); !ok {
		t.Fatal("two seconds at half a token per second should pay for one")
	}
}

func TestBudgetTracksClientsApart(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	b := &uploadBudget{capacity: 1, perSec: 1, now: clock.now, buckets: map[string]*bucket{}, swept: clock.t}

	if ok, _ := b.allow("10.0.0.1"); !ok {
		t.Fatal("the first client should be served")
	}
	if ok, _ := b.allow("10.0.0.2"); !ok {
		t.Fatal("a second address has its own budget")
	}
}

func TestBudgetForgetsIdleClients(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	b := &uploadBudget{capacity: 4, perSec: 1, now: clock.now, buckets: map[string]*bucket{}, swept: clock.t}

	b.allow("10.0.0.1")
	if len(b.buckets) != 1 {
		t.Fatalf("the address should be recorded, got %d", len(b.buckets))
	}
	clock.after(11 * time.Minute)
	b.allow("10.0.0.2")
	if _, live := b.buckets["10.0.0.1"]; live {
		t.Fatal("a client quiet for eleven minutes should be gone")
	}
	if _, live := b.buckets["10.0.0.2"]; !live {
		t.Fatal("the client that just asked should be there")
	}
}

func TestClientAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.5:53124"
	if got := clientAddr(r); got != "192.0.2.5" {
		t.Fatalf("clientAddr = %q, want the address without the port", got)
	}
	r.RemoteAddr = "not-a-hostport"
	if got := clientAddr(r); got != "not-a-hostport" {
		t.Fatalf("clientAddr = %q, want the raw value back", got)
	}
}
