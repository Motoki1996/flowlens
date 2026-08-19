package http

import (
	"log/slog"
	"net/http"

	"github.com/flowlens/api/openapi"
	"gopkg.in/yaml.v3"
)

// handleOpenAPIYAML serves the bundled OpenAPI 3.1 document as-is.
func handleOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapi.Bundled)
}

// handleOpenAPIJSON serves the same document converted to JSON. The
// conversion happens per request rather than at build time — this endpoint
// is unauthenticated but not high-traffic, so the simplicity is worth more
// than the saved CPU.
func handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	var doc any
	if err := yaml.Unmarshal(openapi.Bundled, &doc); err != nil {
		slog.Error("decode bundled openapi document", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}
