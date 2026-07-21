// Package http wires the HTTP router, middleware and handlers together.
package http

import (
	"time"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/config"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/github"
	"github.com/flowlens/api/internal/user"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

// Server holds handler dependencies and builds the router.
type Server struct {
	pool       *pgxpool.Pool
	oauth      *oauth2.Config
	github     github.Client
	users      *user.Service
	sessions   *auth.SessionService
	cookies    cookieManager
	webBaseURL string
	sessionTTL time.Duration
}

// NewServer constructs a Server from configuration and a database pool.
func NewServer(cfg *config.Config, pool *pgxpool.Pool) (*Server, error) {
	cipher, err := auth.NewAESGCMCipher(cfg.TokenEncryptionKey)
	if err != nil {
		return nil, err
	}
	queries := db.New(pool)

	return &Server{
		pool:       pool,
		oauth:      auth.NewGitHubOAuthConfig(cfg.GitHubClientID, cfg.GitHubClientSecret, cfg.APIBaseURL),
		github:     github.NewHTTPClient(),
		users:      user.NewService(queries, cipher),
		sessions:   auth.NewSessionService(queries, cfg.SessionTTL),
		cookies:    cookieManager{secure: cfg.IsProduction()},
		webBaseURL: cfg.WebBaseURL,
		sessionTTL: cfg.SessionTTL,
	}, nil
}

// Router builds the chi router with all routes and middleware.
func (s *Server) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(requestLogger)
	r.Use(corsMiddleware(s.webBaseURL))

	// Health check (unauthenticated).
	r.Get("/healthz", s.handleHealth)

	// OAuth endpoints (browser redirects, unauthenticated).
	r.Get("/auth/github", s.handleGitHubLogin)
	r.Get("/auth/github/callback", s.handleGitHubCallback)
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
