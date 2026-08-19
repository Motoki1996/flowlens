package http

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/flowlens/api/openapi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// route is a (method, path) pair, comparable between chi's actual router
// and the OpenAPI document's paths map. chi's "{taskID}" path-parameter
// syntax is already OpenAPI-compatible, so no translation is needed either
// direction.
type route struct {
	method string
	path   string
}

func (r route) String() string {
	return r.method + " " + r.path
}

// actualRoutes walks r's registered routes into a route set.
//
// /metrics is excluded: it is a Prometheus-scraped operational endpoint
// authenticated by a metrics token rather than session/bearer/webhook auth,
// so it deliberately has no OpenAPI entry (see the comment beside its
// registration in server.go and the one on the /openapi.yaml routes next to
// it).
func actualRoutes(t *testing.T, r chi.Router) map[route]bool {
	t.Helper()
	routes := map[route]bool{}
	err := chi.Walk(r, func(method, path string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if path == "/metrics" {
			return nil
		}
		routes[route{method: method, path: path}] = true
		return nil
	})
	require.NoError(t, err)

	// chi.Walk has a blind spot verified against this exact router: when a
	// chi.Route(pattern, ...) sub-mount (here protected's
	// "/projects" -> POST "/") and a separate, later-registered plain method
	// route at the identical pattern (shared's GET "/projects") coexist, chi
	// serves both correctly at runtime (TestOpenAPIDriftDetection asserts
	// this directly) but Walk only reports the mount's own sub-path
	// ("/api/v1/projects/", trailing slash) — the plain GET is invisible to
	// Walk entirely. Rather than restructure this route (used elsewhere in
	// server.go's own doc comment as a deliberate, tested coexistence
	// pattern), the one known gap is added back by hand.
	routes[route{method: http.MethodGet, path: "/api/v1/projects"}] = true

	return routes
}

// specRoutes parses the embedded, bundled OpenAPI document's paths map into
// a route set.
func specRoutes(t *testing.T) map[route]bool {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(openapi.Bundled, &doc))

	httpMethods := map[string]bool{
		http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
		http.MethodPatch: true, http.MethodDelete: true, http.MethodHead: true,
		http.MethodOptions: true,
	}

	routes := map[route]bool{}
	for path, operations := range doc.Paths {
		for key := range operations {
			method := strings.ToUpper(key)
			if !httpMethods[method] {
				continue // not an HTTP method key (e.g. "parameters", "summary")
			}
			routes[route{method: method, path: path}] = true
		}
	}
	return routes
}

// TestOpenAPIDriftDetection fails whenever the router (server.go's
// Router()) and the OpenAPI document (apps/api/openapi/) disagree on the
// set of routes — the whole point of issue #200: a route added or removed
// in one place without the other must break the build, not rot silently.
func TestOpenAPIDriftDetection(t *testing.T) {
	server, _ := newTestServer(t)
	router := server.Router()

	// Confirms the actualRoutes doc comment's claim: GET /api/v1/projects
	// really is reachable at runtime even though chi.Walk can't see it.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.NotEqual(t, http.StatusNotFound, rec.Code, "GET /api/v1/projects should be routed (401 unauthenticated, not 404 unmatched)")

	actual := actualRoutes(t, router)
	spec := specRoutes(t)

	var missingFromSpec, missingFromRoutes []string
	for r := range actual {
		if !spec[r] {
			missingFromSpec = append(missingFromSpec, r.String())
		}
	}
	for r := range spec {
		if !actual[r] {
			missingFromRoutes = append(missingFromRoutes, r.String())
		}
	}
	sort.Strings(missingFromSpec)
	sort.Strings(missingFromRoutes)

	if len(missingFromSpec) > 0 || len(missingFromRoutes) > 0 {
		t.Fatalf(
			"router and openapi/ have drifted.\nin router but missing from openapi/ (%d):\n  %s\nin openapi/ but missing from router (%d):\n  %s\n"+
				"if you added/removed a route in server.go, update apps/api/openapi/ (paths/*.yaml) to match, then `make generate` to rebundle.",
			len(missingFromSpec), strings.Join(missingFromSpec, "\n  "),
			len(missingFromRoutes), strings.Join(missingFromRoutes, "\n  "),
		)
	}
}

// TestOpenAPIDocumentServed checks the two serving endpoints themselves,
// unauthenticated, alongside the drift test above which only checks the
// route *set* matches — this checks the bytes actually come back.
func TestOpenAPIDocumentServed(t *testing.T) {
	server, _ := newTestServer(t)
	router := server.Router()

	t.Run("yaml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotEmpty(t, rec.Body.Bytes())
		assert.Contains(t, rec.Header().Get("Content-Type"), "yaml")
	})

	t.Run("json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotEmpty(t, rec.Body.Bytes())
		assert.Contains(t, rec.Header().Get("Content-Type"), "json")
	})
}
