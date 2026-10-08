// guard.go holds the two things a tool that writes 250MB uploads to disk wants
// when it stops living on localhost: a password, and a ceiling on how fast one
// client can fill that disk.

package server

import (
	"crypto/subtle"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// requirePassword gates every route behind HTTP Basic auth. The username is not
// checked: this is one shared secret for whoever runs the instance, not an
// account system.
//
// /api/health is the one exception. The container's own health check has no way
// to carry a password, and what it answers — that a process is up — says nothing
// about whatever is on the disk behind it.
func requirePassword(want string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		_, got, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="图切", charset="UTF-8"`)
			writeErr(w, http.StatusUnauthorized, "需要密码")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// uploadBudget is a token bucket per client address. Counting from zero each
// minute would let a client spend a whole minute's worth of uploads in the first
// second, so the budget refills continuously and a bucket starts full, which is
// what makes a short burst possible.
type uploadBudget struct {
	capacity float64
	perSec   float64
	now      func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	tokens float64
	seen   time.Time
}

// newUploadBudget returns nil when the limit is off, so a plain `if s.budget !=
// nil` reads as "no ceiling configured".
func newUploadBudget(perMinute int) *uploadBudget {
	if perMinute <= 0 {
		return nil
	}
	return &uploadBudget{
		// A burst of a quarter minute's worth, never fewer than six uploads.
		capacity: math.Max(6, float64(perMinute)/4),
		perSec:   float64(perMinute) / 60,
		now:      time.Now,
		buckets:  make(map[string]*bucket),
	}
}

// allow spends one token for key and reports how long to wait for the next one
// when the bucket has run dry.
func (b *uploadBudget) allow(key string) (bool, time.Duration) {
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()

	b.sweepLocked(now)
	tb := b.buckets[key]
	if tb == nil {
		tb = &bucket{tokens: b.capacity, seen: now}
		b.buckets[key] = tb
	}
	tb.tokens += now.Sub(tb.seen).Seconds() * b.perSec
	if tb.tokens > b.capacity {
		tb.tokens = b.capacity
	}
	tb.seen = now
	if tb.tokens >= 1 {
		tb.tokens--
		return true, 0
	}
	return false, time.Duration((1 - tb.tokens) / b.perSec * float64(time.Second))
}

// sweepLocked drops addresses that have been quiet for ten minutes, so the map
// tracks who is actually connected rather than everyone who ever was.
func (b *uploadBudget) sweepLocked(now time.Time) {
	if now.Sub(b.swept) < time.Minute {
		return
	}
	b.swept = now
	for key, tb := range b.buckets {
		if tb.tokens >= 1 && now.Sub(tb.seen) > 10*time.Minute {
			delete(b.buckets, key)
		}
	}
}

// limitUploads guards the one route that writes to disk. Clients are counted by
// the address the connection came from: behind a reverse proxy that is the proxy,
// so rate limiting belongs to the proxy too.
func (s *Server) limitUploads(next http.Handler) http.Handler {
	if s.budget == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/analyze" {
			next.ServeHTTP(w, r)
			return
		}
		if ok, wait := s.budget.allow(clientAddr(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			writeErr(w, http.StatusTooManyRequests, "上传太频繁，请稍后再试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
