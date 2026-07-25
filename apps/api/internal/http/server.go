// Package http wires the HTTP router, middleware and handlers together.
package http

import (
	"net/http"
	"time"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/config"
	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/user"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server holds handler dependencies and builds the router.
type Server struct {
	pool       *pgxpool.Pool
	users      *user.Service
	sessions   *auth.SessionService
	cookies    cookieManager
	webBaseURL string
	sessionTTL time.Duration
	cipher     *crypto.Cipher
}

// NewServer constructs a Server from configuration, a database pool, and a
// Cipher for encrypting secrets at rest (GitLab access tokens, webhook
// secrets — not yet used by any handler).
func NewServer(cfg *config.Config, pool *pgxpool.Pool, cipher *crypto.Cipher) (*Server, error) {
	queries := db.New(pool)

	return &Server{
		pool:       pool,
		users:      user.NewService(queries),
		sessions:   auth.NewSessionService(queries, cfg.SessionTTL),
		cookies:    cookieManager{secure: cfg.IsProduction()},
		webBaseURL: cfg.WebBaseURL,
		sessionTTL: cfg.SessionTTL,
		cipher:     cipher,
	}, nil
}

// Router builds the chi router with all routes and middleware.
func (s *Server) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(requestLogger)
	r.Use(corsMiddleware(s.webBaseURL))

	// Health check (unauthenticated).
	r.Get("/healthz", s.handleHealth)

	// Local auth endpoints (JSON, unauthenticated).
	r.Post("/auth/signup", s.handleSignup)
	r.Post("/auth/login", s.handleLogin)
	r.Post("/auth/logout", s.handleLogout)

	// Authenticated API.
	r.Route("/api/v1", func(api chi.Router) {
		api.Group(func(protected chi.Router) {
			protected.Use(s.requireAuth)
			protected.Get("/me", s.handleMe)
		})
	})

	return r
}

// startSession issues a session for userID and sets the session cookie.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID pgtype.UUID) error {
	token, err := s.sessions.Create(r.Context(), userID)
	if err != nil {
		return err
	}
	s.cookies.setSession(w, token, int(s.sessionTTL.Seconds()))
	return nil
}
