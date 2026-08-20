package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/mergerequest"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// mergeRequestIDFromURL parses the {mergeRequestID} path parameter. A
// malformed ID is reported as "not found" so it is indistinguishable from
// an unknown one, the same convention taskIDFromURL uses.
func mergeRequestIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "mergeRequestID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// parseMergeRequestListFilter reads the ?state=, ?author=, ?taskId=,
// ?since=, ?until=, ?sort=, ?page= and ?per_page= query parameters. state accepts a GitLab MR
// state ("opened", "merged", "closed", "locked"); since/until are
// YYYY-MM-DD dates bounding gitlab_created_at, the same format
// parseDateQueryParam already uses for task due/start dates; sort accepts
// "updated" to rank by gitlab_updated_at instead of the default
// gitlab_created_at. taskId lets the Task single view fetch its own related
// merge requests through this same endpoint rather than a separate route.
// page/per_page are the same 1-based paging parameters
// handleListWebhookEvents takes, clamped in mergerequest.Service.
// Any may be omitted to mean "no filter"/"default order"/"first page".
func parseMergeRequestListFilter(r *http.Request) (mergerequest.ListFilter, error) {
	var filter mergerequest.ListFilter

	if v := r.URL.Query().Get("state"); v != "" {
		switch v {
		case "opened", "merged", "closed", "locked":
			filter.State = v
		default:
			return mergerequest.ListFilter{}, errors.New("state must be one of opened, merged, closed, locked")
		}
	}

	filter.Author = r.URL.Query().Get("author")

	if v := r.URL.Query().Get("taskId"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return mergerequest.ListFilter{}, errors.New("taskId must be a UUID")
		}
		filter.TaskID = &id
	}

	since, err := parseDateQueryParam(r, "since")
	if err != nil {
		return mergerequest.ListFilter{}, err
	}
	filter.Since = since

	until, err := parseDateQueryParam(r, "until")
	if err != nil {
		return mergerequest.ListFilter{}, err
	}
	filter.Until = until

	if v := r.URL.Query().Get("sort"); v != "" {
		if v != mergerequest.SortUpdated {
			return mergerequest.ListFilter{}, errors.New("sort must be \"updated\"")
		}
		filter.Sort = v
	}

	filter.Page = atoiOrZero(r.URL.Query().Get("page"))
	filter.PerPage = atoiOrZero(r.URL.Query().Get("per_page"))

	return filter, nil
}

// handleListMergeRequests returns one page of the project's merge requests
// matching the state/author/taskId/since/until query filters, scoped to the
// authenticated user. The response is the {mergeRequests, nextPage} envelope
// handleListWebhookEvents uses rather than a bare array: a long-lived
// repository holds far more merge requests than one response should carry.
func (s *Server) handleListMergeRequests(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	filter, err := parseMergeRequestListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}

	page, err := s.mergeRequests.List(r.Context(), u.ID, projectID, filter)
	if err != nil {
		writeMergeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		MergeRequests []mergerequest.MergeRequest `json:"mergeRequests"`
		NextPage      int                         `json:"nextPage"`
		TotalCount    int64                       `json:"totalCount"`
	}{MergeRequests: page.MergeRequests, NextPage: page.NextPage, TotalCount: page.TotalCount})
}

// handleGetMergeRequest returns a single merge request, scoped to the
// authenticated user.
func (s *Server) handleGetMergeRequest(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	mergeRequestID, ok := mergeRequestIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "merge request not found")
		return
	}

	mr, err := s.mergeRequests.Get(r.Context(), u.ID, mergeRequestID)
	if err != nil {
		writeMergeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mr)
}

func writeMergeRequestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mergerequest.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "merge request not found")
	case errors.Is(err, mergerequest.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	default:
		slog.Error("merge request request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
