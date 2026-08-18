package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The self-hosting surface (issue: OSS self-hosting): the operational
// endpoints an operator hits before or instead of logging in, and the two
// switches that let a public instance close registration and its metrics.

func TestHandleVersion(t *testing.T) {
	s, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"version":"test"`) {
		t.Errorf("body = %s, want the stamped version", got)
	}
}

func TestMetricsToken(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		authHeader string
		wantStatus int
	}{
		{"open when no token is configured", "", "", http.StatusOK},
		{"a configured token is required", "s3cret", "", http.StatusUnauthorized},
		{"a wrong token is rejected", "s3cret", "Bearer nope", http.StatusUnauthorized},
		{"a bare token without the scheme is rejected", "s3cret", "s3cret", http.StatusUnauthorized},
		{"the configured token is accepted", "s3cret", "Bearer s3cret", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			s.metricsToken = tt.token

			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			s.Router().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestSignupDisabled(t *testing.T) {
	signup := func(t *testing.T, s *Server, username string) int {
		t.Helper()
		body := strings.NewReader(`{"username":"` + username + `","email":"` + username + `@example.com","password":"password123"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/signup", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("the first account is allowed even with signup off, so a fresh instance can be bootstrapped", func(t *testing.T) {
		s, _ := newTestServer(t)
		s.allowSignup = false

		if got := signup(t, s, "first"); got != http.StatusCreated {
			t.Fatalf("first signup status = %d, want %d", got, http.StatusCreated)
		}
	})

	t.Run("a later account is refused once one exists", func(t *testing.T) {
		s, _ := newTestServer(t)
		s.allowSignup = false

		if got := signup(t, s, "first"); got != http.StatusCreated {
			t.Fatalf("first signup status = %d, want %d", got, http.StatusCreated)
		}
		if got := signup(t, s, "second"); got != http.StatusForbidden {
			t.Errorf("second signup status = %d, want %d", got, http.StatusForbidden)
		}
	})

	t.Run("signup stays open by default", func(t *testing.T) {
		s, _ := newTestServer(t)

		if got := signup(t, s, "first"); got != http.StatusCreated {
			t.Fatalf("first signup status = %d, want %d", got, http.StatusCreated)
		}
		if got := signup(t, s, "second"); got != http.StatusCreated {
			t.Errorf("second signup status = %d, want %d", got, http.StatusCreated)
		}
	})
}

func TestServerClientIP(t *testing.T) {
	tests := []struct {
		name      string
		hops      int
		remote    string
		forwarded string
		want      string
	}{
		{
			name:   "no proxy trusted: the socket address wins",
			hops:   0,
			remote: "10.0.0.9:5000", forwarded: "1.2.3.4",
			want: "10.0.0.9",
		},
		{
			name:   "one hop: the address the proxy appended",
			hops:   1,
			remote: "10.0.0.9:5000", forwarded: "203.0.113.7",
			want: "203.0.113.7",
		},
		{
			name:   "one hop: a client-forged entry sits to the left and is ignored",
			hops:   1,
			remote: "10.0.0.9:5000", forwarded: "1.2.3.4, 203.0.113.7",
			want: "203.0.113.7",
		},
		{
			name:   "two hops: the entry written by the outermost trusted proxy",
			hops:   2,
			remote: "10.0.0.9:5000", forwarded: "203.0.113.7, 10.0.0.5",
			want: "203.0.113.7",
		},
		{
			name:   "fewer entries than configured hops: trust none of it",
			hops:   2,
			remote: "10.0.0.9:5000", forwarded: "203.0.113.7",
			want: "10.0.0.9",
		},
		{
			name:   "a proxy is configured but no header arrived",
			hops:   1,
			remote: "10.0.0.9:5000", forwarded: "",
			want: "10.0.0.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			s.trustedProxyHops = tt.hops

			req := httptest.NewRequest(http.MethodGet, "/version", nil)
			req.RemoteAddr = tt.remote
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}

			if got := s.clientIP(req); got != tt.want {
				t.Errorf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
