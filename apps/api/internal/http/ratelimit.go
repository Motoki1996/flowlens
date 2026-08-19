package http

import (
	"net"
	"net/http"
	"strconv"
	"strings"
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

// clientIP resolves the address the per-IP rate limiters key on, honouring
// X-Forwarded-For when the API is behind a known number of trusted proxies.
//
// This matters for the default self-hosted topology: the bundled compose
// file proxies the browser through the Next.js server, so every request
// arrives from one container address and, keyed on RemoteAddr alone, the
// login limiter would be shared by every user at once — one person's failed
// attempts would lock out the whole instance.
//
// Counting hops from the right is what makes the header safe to use. Each
// trusted proxy appends the address it saw, so the entry trustedProxyHops
// from the end is the one written by the outermost trusted proxy, and
// anything a client forged sits to the left of it and is ignored. A hop
// count that does not match the real topology is therefore the failure mode
// to avoid, which is why it is configured explicitly and defaults to 0 —
// no proxy trusted, RemoteAddr only.
func (s *Server) clientIP(r *http.Request) string {
	if s.trustedProxyHops <= 0 {
		return clientIP(r)
	}

	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		return clientIP(r)
	}

	parts := strings.Split(forwarded, ",")
	idx := len(parts) - s.trustedProxyHops
	if idx < 0 {
		// Fewer entries than configured hops: the request did not traverse
		// the expected chain, so trust none of it.
		return clientIP(r)
	}
	if addr := strings.TrimSpace(parts[idx]); addr != "" {
		return addr
	}
	return clientIP(r)
}
