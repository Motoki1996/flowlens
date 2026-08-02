package http

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// simpleRateLimiter is a per-key fixed-window request counter. It exists to
// bound abusive webhook delivery volume (docs/plans/issue-sync.md,
// Security: "the endpoint is rate-limited and bounded in body size") — it is
// intentionally simple (single-process, in-memory), matching the issue's
// "simple rate limiting" scope rather than a general-purpose limiter.
type simpleRateLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*rateLimitBucket
}

type rateLimitBucket struct {
	windowStart time.Time
	count       int
}

// newSimpleRateLimiter returns a limiter allowing up to limit requests per
// key within each window.
func newSimpleRateLimiter(limit int, window time.Duration) *simpleRateLimiter {
	return &simpleRateLimiter{limit: limit, window: window, buckets: map[string]*rateLimitBucket{}}
}

// Allow reports whether key is still under limit for the current window,
// starting a fresh window (and count) for key if none is open yet or the
// previous one has elapsed.
func (l *simpleRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok || now.Sub(b.windowStart) >= l.window {
		b = &rateLimitBucket{windowStart: now}
		l.buckets[key] = b
	}
	b.count++
	return b.count <= l.limit
}

// writeTooManyRequests writes a 429 response with a Retry-After header set to
// window's length in seconds, so a well-behaved caller knows how long to
// back off before its bucket resets.
func writeTooManyRequests(w http.ResponseWriter, window time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
}

// clientIP returns the request's remote address without its port, for use
// as a rate-limit key. It falls back to the raw RemoteAddr on the rare
// request where RemoteAddr carries no port to split.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
