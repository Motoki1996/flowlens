// Package gitlaburl normalizes a GitLab CE base URL so the same instance
// always maps to the same stored string. It is shared by internal/gitlabconn
// (a project's GitLab connection) and internal/gitlabidentity (a user's
// registered GitLab identity, issue #102): ?assignee=me joins the two on
// base_url equality, so the two callers must normalize identically or the
// join silently misses.
package gitlaburl

import (
	"errors"
	"net/url"
	"strings"
)

// ErrInvalid is returned when raw is not an absolute http(s) URL.
var ErrInvalid = errors.New("gitlaburl: base URL must be an absolute http(s) URL")

// Normalize trims raw, requires an http(s) scheme and host, and strips any
// trailing slash, query, or fragment so the same instance always normalizes
// to the same stored value.
func Normalize(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrInvalid
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrInvalid
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}
