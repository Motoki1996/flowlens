package http

import "net/http"

// handleVersion reports the running build, unauthenticated and next to
// /healthz. A self-hosted operator following an upgrade in
// docs/self-hosting.md needs to confirm which release actually came up,
// and an operator who cannot get past the login page still needs that
// answer — so, like /healthz, it is deliberately not behind a session.
// It exposes nothing an attacker could not learn from the release notes.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}
