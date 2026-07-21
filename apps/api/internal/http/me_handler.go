package http

import (
	"net/http"

	"github.com/flowlens/api/internal/user"
)

// handleMe returns the authenticated user's profile.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, user.FromDB(u))
}
