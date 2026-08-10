// Package http wires the HTTP router, middleware and handlers together.
package http

import (
	"context"
	"net/http"
	"time"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/config"
	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/flowlens/api/internal/linkedproject"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/projectsync"
	"github.com/flowlens/api/internal/syncjob"
	"github.com/flowlens/api/internal/task"
	"github.com/flowlens/api/internal/taskdependency"
	"github.com/flowlens/api/internal/user"
	"github.com/flowlens/api/internal/webhookevent"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Pinger reports whether the database is reachable. It is the only thing
// this package needs from the connection pool, so it takes the narrow
// interface rather than the pool type.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Server holds handler dependencies and builds the router.
type Server struct {
	health           Pinger
	users            *user.Service
	projects         *project.Service
	backlogs         *backlog.Service
	apiTokens        *apitoken.Service
	tasks            *task.Service
	taskDependencies *taskdependency.Service
	gitlabConns      *gitlabconn.Service
	linkedProjects   *linkedproject.Service
	projectSync      *projectsync.Service
	webhookEvents    *webhookevent.Service
	syncJobs         *syncjob.Service
	webhookLimiter   *simpleRateLimiter
	tokenLimiter     *simpleRateLimiter
	authLimiter      *simpleRateLimiter
	sessions         *auth.SessionService
	cookies          cookieManager
	webBaseURL       string
	sessionTTL       time.Duration
	cipher           *crypto.Cipher
}

// NewServer constructs a Server from configuration, the generated queries, a
// database health probe, a TxRunner (so task writes that must enqueue an
// outbound sync job commit atomically, per docs/plans/issue-sync.md), and a
// Cipher for encrypting secrets at rest (GitLab access tokens, webhook
// secrets).
func NewServer(cfg *config.Config, queries database.Querier, health Pinger, txRunner database.TxRunner, cipher *crypto.Cipher) (*Server, error) {
	projects := project.NewService(queries)
	backlogs := backlog.NewService(queries, projects)
	apiTokens := apitoken.NewService(queries, projects)
	clientFactory := func(baseURL string) gitlab.Client { return gitlab.NewHTTPClient(baseURL) }
	gitlabConns := gitlabconn.NewService(queries, projects, cipher, clientFactory)
	tasks := task.NewService(queries, txRunner, projects, backlogs)
	return &Server{
		health:           health,
		users:            user.NewService(queries),
		projects:         projects,
		backlogs:         backlogs,
		apiTokens:        apiTokens,
		tasks:            tasks,
		taskDependencies: taskdependency.NewService(queries, projects, tasks),
		gitlabConns:      gitlabConns,
		linkedProjects:   linkedproject.NewService(queries, txRunner, projects, gitlabConns, cipher, cfg.AppPublicURL),
		projectSync:      projectsync.NewService(queries, txRunner, projects, cipher, clientFactory),
		webhookEvents:    webhookevent.NewService(queries, cipher),
		syncJobs:         syncjob.NewService(queries, projects),
		webhookLimiter:   newSimpleRateLimiter(webhookRateLimit, webhookRateLimitWindow),
		tokenLimiter:     newSimpleRateLimiter(tokenRateLimit, tokenRateLimitWindow),
		authLimiter:      newSimpleRateLimiter(authRateLimit, authRateLimitWindow),
		sessions:         auth.NewSessionService(queries, cfg.SessionTTL),
		cookies:          cookieManager{secure: cfg.IsProduction()},
		webBaseURL:       cfg.WebBaseURL,
		sessionTTL:       cfg.SessionTTL,
		cipher:           cipher,
	}, nil
}

// Router builds the chi router with all routes and middleware.
func (s *Server) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(requestLogger)
	r.Use(metricsMiddleware)
	r.Use(corsMiddleware(s.webBaseURL))

	// Health check and Prometheus metrics (both unauthenticated, both
	// operational endpoints scraped by infrastructure rather than used by
	// callers — issue #96).
	r.Get("/healthz", s.handleHealth)
	r.Handle("/metrics", metricsHandler)

	// Local auth endpoints (JSON, unauthenticated).
	r.Post("/auth/signup", s.handleSignup)
	r.Post("/auth/login", s.handleLogin)
	r.Post("/auth/logout", s.handleLogout)

	// GitLab webhook receiver (token-header auth, not session; ADR-0008).
	r.Post("/webhooks/gitlab/{linkID}", s.handleGitlabWebhook)

	// Authenticated API.
	r.Route("/api/v1", func(api chi.Router) {
		// Session-only: never reachable by a project API token (issue #66's
		// route allowlist — everything else is default-denied to a token).
		// gitlab-connection* holds an encrypted GitLab PAT, /api-tokens can
		// mint more tokens (privilege escalation), POST /projects and
		// DELETE /projects/{projectID} let a token create or destroy its
		// own footing, and linked-gitlab-projects' management/
		// webhook-registration routes are all left out for the same
		// "a token must not be able to reshape its own trust boundary"
		// reason.
		api.Group(func(protected chi.Router) {
			protected.Use(s.requireAuth)
			protected.Use(s.requireCSRF)
			protected.Get("/me", s.handleMe)

			// Cross-project task collection (issue #76): every task across
			// every project the caller owns, "what should I be doing right
			// now" without opening each project in turn. Deliberately
			// session-only, not part of the read/write bearer allowlist
			// below — a project API token is scoped to a single project
			// (ADR-0009) and has no notion of "every project I own", so
			// there is no token-shaped version of this route to add.
			protected.Get("/tasks", s.handleListAllTasks)

			protected.Route("/projects", func(projects chi.Router) {
				projects.Post("/", s.handleCreateProject)
				projects.Patch("/{projectID}", s.handleUpdateProject)
				projects.Delete("/{projectID}", s.handleDeleteProject)

				projects.Put("/{projectID}/gitlab-connection", s.handlePutGitlabConnection)
				projects.Get("/{projectID}/gitlab-connection", s.handleGetGitlabConnection)
				projects.Delete("/{projectID}/gitlab-connection", s.handleDeleteGitlabConnection)
				projects.Post("/{projectID}/gitlab-connection/test", s.handleTestGitlabConnection)
				projects.Get("/{projectID}/gitlab-connection/available-projects", s.handleListAvailableGitlabProjects)

				projects.Get("/{projectID}/linked-gitlab-projects", s.handleListLinkedGitlabProjects)
				projects.Post("/{projectID}/linked-gitlab-projects", s.handleCreateLinkedGitlabProject)
				// The single-link read is project-nested, mirroring the web
				// route, while the mutations below stay flat: see
				// handleGetLinkedGitlabProject for why this one needs the
				// project in its URL.
				projects.Get("/{projectID}/linked-gitlab-projects/{linkID}", s.handleGetLinkedGitlabProject)

				projects.Get("/{projectID}/api-tokens", s.handleListAPITokens)
				projects.Post("/{projectID}/api-tokens", s.handleCreateAPIToken)

				// Failed sync jobs (issue #97): a permanently-failed
				// sync_jobs row is otherwise invisible outside the
				// database. Session-only, like the rest of this group —
				// not on the bearer-token route allowlist.
				projects.Get("/{projectID}/sync-jobs", s.handleListFailedSyncJobs)
			})

			protected.Route("/api-tokens", func(tokens chi.Router) {
				tokens.Delete("/{tokenID}", s.handleDeleteAPIToken)
			})

			protected.Route("/sync-jobs", func(jobs chi.Router) {
				jobs.Post("/{jobID}/retry", s.handleRetrySyncJob)
			})

			protected.Route("/linked-gitlab-projects", func(linked chi.Router) {
				linked.Patch("/{linkID}", s.handleUpdateLinkedGitlabProject)
				linked.Delete("/{linkID}", s.handleDeleteLinkedGitlabProject)
				linked.Post("/{linkID}/webhook", s.handleRegisterLinkedGitlabProjectWebhook)
				linked.Post("/{linkID}/sync-runs", s.handleCreateSyncRun)
				linked.Get("/{linkID}/webhook-events", s.handleListWebhookEvents)
				linked.Get("/{linkID}/webhook-events/{eventID}", s.handleGetWebhookEvent)
				linked.Post("/{linkID}/webhook-events/{eventID}/retry", s.handleRetryWebhookEvent)
				// Members/labels back a task's assignee/label pickers (issue
				// #80) — session-only like the rest of this route group,
				// since only the web app's editing UI needs them.
				linked.Get("/{linkID}/members", s.handleListLinkedGitlabProjectMembers)
				linked.Get("/{linkID}/labels", s.handleListLinkedGitlabProjectLabels)
			})
		})

		// Session OR `Authorization: Bearer <project token>`
		// (docs/plans/issue-sync.md "AI-facing"; internal/apitoken). Every
		// route mounted here is on issue #66's explicit read/write
		// allowlist — this is the only place a bearer token can reach app
		// data at all. requireTokenScope(apitoken.ScopeWrite) gates
		// mutations (a no-op for session auth, and for any token that
		// already has write — write always implies read,
		// apitoken.normalizeScopes). requireTokenProjectMatch and
		// requireTokenResourceProject enforce that a token can only ever
		// reach its own project's resources, whether the URL carries a
		// {projectID} directly or a single-resource ID that must first be
		// resolved to its project — see bearer_middleware.go's doc
		// comments for why both checks exist on top of the ownership check
		// every service method already performs.
		//
		// Every route below is registered as a flat, full-path leaf (never
		// through a nested shared.Route(prefix, ...) sub-mount) precisely
		// because `protected` above already owns a chi.Mount() at each of
		// these same prefixes (/projects, /backlogs, /tasks,
		// /task-dependencies, /linked-gitlab-projects) for its own
		// session-only routes; a second, independent sub-mount at an
		// identical prefix is untested territory, whereas a flat leaf route
		// coexisting with a sibling group's mount at the same prefix is
		// exactly what the original AI-facing routes already did safely
		// (GET /tasks/{taskID}/context next to protected's /tasks mount).
		api.Group(func(shared chi.Router) {
			shared.Use(s.requireAuthOrBearer)
			shared.Use(s.requireCSRF)

			shared.Get("/projects", s.handleListProjects)
			shared.With(requireTokenProjectMatch).Get("/projects/{projectID}", s.handleGetProject)

			shared.With(requireTokenProjectMatch).Get("/projects/{projectID}/backlogs", s.handleListBacklogs)
			shared.With(requireTokenScope(apitoken.ScopeWrite), requireTokenProjectMatch).Post("/projects/{projectID}/backlogs", s.handleCreateBacklog)
			// /backlogs/order is a flat leaf next to the {backlogID} mount
			// below, the same coexistence this file's own doc comment
			// already relies on for /tasks and /tasks/{taskID}.
			shared.With(requireTokenScope(apitoken.ScopeWrite), requireTokenProjectMatch).Patch("/projects/{projectID}/backlogs/order", s.handleReorderBacklogs)

			shared.With(requireTokenProjectMatch).Get("/projects/{projectID}/tasks", s.handleListTasks)
			shared.With(requireTokenScope(apitoken.ScopeWrite), requireTokenProjectMatch).Post("/projects/{projectID}/tasks", s.handleCreateTask)
			shared.With(requireTokenScope(apitoken.ScopeWrite), requireTokenProjectMatch).Patch("/projects/{projectID}/tasks/order", s.handleReorderTasks)

			shared.With(requireTokenProjectMatch).Get("/projects/{projectID}/task-dependencies", s.handleListTaskDependencies)
			shared.With(requireTokenScope(apitoken.ScopeWrite), requireTokenProjectMatch).Post("/projects/{projectID}/task-dependencies", s.handleCreateTaskDependency)

			shared.With(requireTokenProjectMatch).Get("/projects/{projectID}/tasks/context", s.handleListTaskContexts)

			backlogResource := requireTokenResourceProject("backlogID", backlog.ErrNotFound, s.backlogs.ProjectID)
			shared.With(backlogResource).Get("/backlogs/{backlogID}", s.handleGetBacklog)
			shared.With(requireTokenScope(apitoken.ScopeWrite), backlogResource).Patch("/backlogs/{backlogID}", s.handleUpdateBacklog)
			shared.With(requireTokenScope(apitoken.ScopeWrite), backlogResource).Delete("/backlogs/{backlogID}", s.handleDeleteBacklog)

			depResource := requireTokenResourceProject("dependencyID", taskdependency.ErrNotFound, s.taskDependencies.ProjectID)
			shared.With(requireTokenScope(apitoken.ScopeWrite), depResource).Delete("/task-dependencies/{dependencyID}", s.handleDeleteTaskDependency)

			taskResource := requireTokenResourceProject("taskID", task.ErrNotFound, s.tasks.ProjectID)
			shared.With(taskResource).Get("/tasks/{taskID}", s.handleGetTask)
			shared.With(requireTokenScope(apitoken.ScopeWrite), taskResource).Patch("/tasks/{taskID}", s.handleUpdateTask)
			shared.With(requireTokenScope(apitoken.ScopeWrite), taskResource).Delete("/tasks/{taskID}", s.handleDeleteTask)
			shared.With(requireTokenScope(apitoken.ScopeWrite), taskResource).Post("/tasks/{taskID}/close", s.handleCloseTask)
			shared.With(requireTokenScope(apitoken.ScopeWrite), taskResource).Post("/tasks/{taskID}/reopen", s.handleReopenTask)
			shared.With(requireTokenScope(apitoken.ScopeWrite), taskResource).Post("/tasks/{taskID}/assign-backlog", s.handleAssignTaskBacklog)
			shared.With(requireTokenScope(apitoken.ScopeWrite), taskResource).Post("/tasks/{taskID}/sync-retry", s.handleRetryTaskSync)
			shared.With(requireTokenScope(apitoken.ScopeWrite), taskResource).Put("/tasks/{taskID}/ai-context", s.handleUpsertTaskAIContext)

			// handleGetTaskContext is already project-scoped through the
			// token itself (task.Service.ContextForProject, driven by
			// tokenProjectFromContext) rather than through this URL's
			// {taskID}, so it needs no requireTokenResourceProject on top.
			shared.Get("/tasks/{taskID}/context", s.handleGetTaskContext)

			linkResource := requireTokenResourceProject("linkID", linkedproject.ErrNotFound, s.linkedProjects.ProjectID)
			shared.With(linkResource).Get("/linked-gitlab-projects/{linkID}/sync-runs", s.handleListSyncRuns)
		})
	})

	return r
}

// startSession issues a session for userID and sets the session cookie,
// alongside a fresh CSRF cookie (issue #92) with the same lifetime.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	token, err := s.sessions.Create(r.Context(), userID)
	if err != nil {
		return err
	}
	s.cookies.setSession(w, token, int(s.sessionTTL.Seconds()))

	csrfToken, err := auth.NewOpaqueToken()
	if err != nil {
		return err
	}
	s.cookies.setCSRF(w, csrfToken, int(s.sessionTTL.Seconds()))
	return nil
}
