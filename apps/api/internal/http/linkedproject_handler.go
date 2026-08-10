package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/linkedproject"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createLinkedGitlabProjectRequest struct {
	GitlabProjectID int64    `json:"gitlabProjectId"`
	SyncScope       string   `json:"syncScope"`
	SyncLabels      []string `json:"syncLabels"`
}

type updateLinkedGitlabProjectRequest struct {
	SyncScope  string   `json:"syncScope"`
	SyncLabels []string `json:"syncLabels"`
	IsDefault  bool     `json:"isDefault"`
}

// linkIDFromURL parses the {linkID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown one.
func linkIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListAvailableGitlabProjects lists the GitLab projects the project's
// connection can link, filtered by the optional ?search= query and paged
// with ?page=/?per_page=.
func (s *Server) handleListAvailableGitlabProjects(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	q := r.URL.Query()
	params := linkedproject.AvailableProjectsParams{
		Search:  q.Get("search"),
		Page:    atoiOrZero(q.Get("page")),
		PerPage: atoiOrZero(q.Get("per_page")),
	}

	projects, page, err := s.linkedProjects.ListAvailable(r.Context(), u.ID, projectID, params)
	if err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Projects []gitlabProjectDTO `json:"projects"`
		NextPage int                `json:"nextPage"`
	}{Projects: toGitlabProjectDTOs(projects), NextPage: page.NextPage})
}

// gitlabProjectDTO mirrors gitlab.Project's JSON shape with FlowLens's
// camelCase convention, since gitlab.Project itself uses GitLab's own
// snake_case tags.
type gitlabProjectDTO struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"pathWithNamespace"`
	WebURL            string `json:"webUrl"`
}

func toGitlabProjectDTOs(in []gitlab.Project) []gitlabProjectDTO {
	out := make([]gitlabProjectDTO, len(in))
	for i, p := range in {
		out[i] = gitlabProjectDTO{
			ID:                p.ID,
			Name:              p.Name,
			PathWithNamespace: p.PathWithNamespace,
			WebURL:            p.WebURL,
		}
	}
	return out
}

func atoiOrZero(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// gitlabUserDTO mirrors gitlab.User's JSON shape with FlowLens's camelCase
// convention, since gitlab.User itself uses GitLab's own snake_case tags.
type gitlabUserDTO struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

func toGitlabUserDTOs(in []gitlab.User) []gitlabUserDTO {
	out := make([]gitlabUserDTO, len(in))
	for i, u := range in {
		out[i] = gitlabUserDTO{ID: u.ID, Username: u.Username, Name: u.Name, AvatarURL: u.AvatarURL}
	}
	return out
}

// gitlabLabelDTO mirrors gitlab.Label's JSON shape.
type gitlabLabelDTO struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func toGitlabLabelDTOs(in []gitlab.Label) []gitlabLabelDTO {
	out := make([]gitlabLabelDTO, len(in))
	for i, l := range in {
		out[i] = gitlabLabelDTO{Name: l.Name, Color: l.Color}
	}
	return out
}

// handleListLinkedGitlabProjectMembers lists linkID's GitLab project's
// members, for a task's assignee picker, filtered by the optional ?search=
// query and paged with ?page=/?per_page=.
func (s *Server) handleListLinkedGitlabProjectMembers(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	q := r.URL.Query()
	params := linkedproject.AvailableProjectsParams{
		Search:  q.Get("search"),
		Page:    atoiOrZero(q.Get("page")),
		PerPage: atoiOrZero(q.Get("per_page")),
	}

	members, page, err := s.linkedProjects.ListMembers(r.Context(), u.ID, linkID, params)
	if err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Members  []gitlabUserDTO `json:"members"`
		NextPage int             `json:"nextPage"`
	}{Members: toGitlabUserDTOs(members), NextPage: page.NextPage})
}

// handleListLinkedGitlabProjectLabels lists linkID's GitLab project's
// existing labels, for a task's label picker.
func (s *Server) handleListLinkedGitlabProjectLabels(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	labels, err := s.linkedProjects.ListLabels(r.Context(), u.ID, linkID)
	if err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Labels []gitlabLabelDTO `json:"labels"`
	}{Labels: toGitlabLabelDTOs(labels)})
}

// handleListLinkedGitlabProjects returns every GitLab project linked to the
// project, scoped to the authenticated user.
func (s *Server) handleListLinkedGitlabProjects(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	links, err := s.linkedProjects.List(r.Context(), u.ID, projectID)
	if err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, links)
}

// handleGetLinkedGitlabProject returns one of the project's linked GitLab
// projects, scoped to the authenticated user. The web app's single view for a
// link reads this rather than picking its record out of the project's
// collection. The route carries {projectID} — unlike the flat PATCH/DELETE —
// because a link, having no project of its own in the response, would
// otherwise leave the caller unable to tell that it reached the right
// project's link.
func (s *Server) handleGetLinkedGitlabProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	link, err := s.linkedProjects.Get(r.Context(), u.ID, projectID, linkID)
	if err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// handleCreateLinkedGitlabProject links a GitLab project to the project's
// GitLab connection, scoped to the authenticated user.
func (s *Server) handleCreateLinkedGitlabProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req createLinkedGitlabProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	link, err := s.linkedProjects.Create(r.Context(), u.ID, projectID, linkedproject.CreateParams{
		GitlabProjectID: req.GitlabProjectID,
		SyncScope:       req.SyncScope,
		SyncLabels:      req.SyncLabels,
	})
	if err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

// handleUpdateLinkedGitlabProject changes a link's sync scope and,
// optionally, promotes it to be its connection's default, scoped to the
// authenticated user.
func (s *Server) handleUpdateLinkedGitlabProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	var req updateLinkedGitlabProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	link, err := s.linkedProjects.Update(r.Context(), u.ID, linkID, linkedproject.UpdateParams{
		SyncScope:  req.SyncScope,
		SyncLabels: req.SyncLabels,
		SetDefault: req.IsDefault,
	})
	if err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// handleRegisterLinkedGitlabProjectWebhook (re)registers linkID's FlowLens
// webhook on GitLab: creates it if none exists yet, or rotates its secret
// if one does. Retrying this endpoint never creates a duplicate hook.
func (s *Server) handleRegisterLinkedGitlabProjectWebhook(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	link, err := s.linkedProjects.RegisterWebhook(r.Context(), u.ID, linkID)
	if err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// handleDeleteLinkedGitlabProject unlinks a GitLab project, scoped to the
// authenticated user. It never deletes anything on the GitLab side and
// never deletes the app's own tasks — only this bookkeeping row.
func (s *Server) handleDeleteLinkedGitlabProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	if err := s.linkedProjects.Delete(r.Context(), u.ID, linkID); err != nil {
		writeLinkedProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeLinkedProjectError maps a linkedproject domain error to its HTTP
// response.
func writeLinkedProjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, linkedproject.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
	case errors.Is(err, linkedproject.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	case errors.Is(err, linkedproject.ErrInvalidSyncScope):
		writeError(w, http.StatusBadRequest, "invalid_sync_scope", `sync_scope must be "all" or "labels"`)
	case errors.Is(err, linkedproject.ErrSyncLabelsRequired):
		writeError(w, http.StatusUnprocessableEntity, "sync_labels_required", `sync_labels must have at least one label when sync_scope is "labels"`)
	case errors.Is(err, linkedproject.ErrAlreadyLinked):
		writeError(w, http.StatusConflict, "already_linked", "this gitlab project is already linked")
	case errors.Is(err, linkedproject.ErrGitlabProjectUnavailable):
		writeError(w, http.StatusUnprocessableEntity, "gitlab_project_unavailable", "could not fetch the gitlab project")
	default:
		slog.Error("linked gitlab project request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
