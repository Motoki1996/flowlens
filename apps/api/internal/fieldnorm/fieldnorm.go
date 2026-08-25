// Package fieldnorm holds the field rules a Backlog and an Epic share.
//
// An epic (000032 migration) is deliberately shaped as "a backlog that lives
// inside a backlog": name, priority, progress, base branch, allowed/forbidden
// scope and the planned period all mean exactly what a backlog's do, and both
// tables carry the same CHECK constraints. Copying the validators into
// internal/epic would have left two sets of string literals free to drift
// from each other and from the schema, so they live here once and each
// package wraps them to attach its own sentinel error — the same reason
// internal/velocity's size weights live in one place, and the same shape as
// internal/assignee's shared member lookups.
//
// The functions here do not know about databases or requests: they take a raw
// value and return the normalized one, or an error from this package that the
// caller maps to its own.
package fieldnorm

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Errors returned by this package. Callers map these to their own sentinels
// (backlog.ErrInvalidPriority, epic.ErrInvalidPriority, ...) so an HTTP
// handler's error mapping stays keyed on the domain package it called.
var (
	ErrInvalidName       = errors.New("fieldnorm: name must be 1-100 characters")
	ErrInvalidSchedule   = errors.New("fieldnorm: start date must not be after due date")
	ErrInvalidPriority   = errors.New("fieldnorm: invalid priority")
	ErrInvalidProgress   = errors.New("fieldnorm: invalid progress")
	ErrInvalidBaseBranch = errors.New("fieldnorm: invalid base branch")
	ErrInvalidScope      = errors.New("fieldnorm: scope field too long")
)

// Priority values, app-only and never synced to GitLab. These are the
// canonical literals: internal/backlog and internal/epic alias them rather
// than redeclaring them, so they cannot drift from the CHECK constraint the
// two tables share.
const (
	PriorityLow    = "low"
	PriorityMedium = "medium"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"
)

// Progress values, the object's own four-stage work state — not a task's
// GitLab-derived status.
const (
	ProgressNotStarted = "not_started"
	ProgressInProgress = "in_progress"
	ProgressOnHold     = "on_hold"
	ProgressDone       = "done"
)

// Sort values a list filter accepts to switch its ORDER BY away from the
// manual position order.
const (
	SortPriority = "priority"
	SortProgress = "progress"
)

// MaxScopeFieldLength bounds an allowed/forbidden scope value, matching
// internal/task's cap on acceptanceCriteria/aiContext.
const MaxScopeFieldLength = 20000

// Name trims raw and enforces the 1-100 character rule.
func Name(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", ErrInvalidName
	}
	return name, nil
}

// Priority defaults an empty raw to PriorityMedium — a create call leaves it
// unset when the caller doesn't specify one, and an update's Optional
// resolves an explicit empty string the same way — and otherwise rejects
// anything outside the fixed set.
func Priority(raw string) (string, error) {
	switch raw {
	case "":
		return PriorityMedium, nil
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent:
		return raw, nil
	default:
		return "", ErrInvalidPriority
	}
}

// Progress defaults an empty raw to ProgressNotStarted, following the same
// absent/explicit-empty rule as Priority.
func Progress(raw string) (string, error) {
	switch raw {
	case "":
		return ProgressNotStarted, nil
	case ProgressNotStarted, ProgressInProgress, ProgressOnHold, ProgressDone:
		return raw, nil
	default:
		return "", ErrInvalidProgress
	}
}

// BaseBranch trims raw and, if non-empty, validates it as a git branch name
// (git-check-ref-format's rules, the subset relevant to a single branch
// component: no control characters or spaces, none of ~ ^ : ? * [ \, no
// "..", no "@{", doesn't start or end with "/", doesn't start with "." or
// end with "." or ".lock"). An empty raw means "not set" and is always
// valid — this is an optional field.
func BaseBranch(raw string) (string, error) {
	branch := strings.TrimSpace(raw)
	if branch == "" {
		return "", nil
	}
	if utf8.RuneCountInString(branch) > 255 {
		return "", ErrInvalidBaseBranch
	}
	if strings.ContainsAny(branch, " ~^:?*[\\") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") {
		return "", ErrInvalidBaseBranch
	}
	if strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") ||
		strings.HasPrefix(branch, ".") || strings.HasSuffix(branch, ".") ||
		strings.HasSuffix(branch, ".lock") {
		return "", ErrInvalidBaseBranch
	}
	for _, r := range branch {
		if r < 0x20 || r == 0x7f {
			return "", ErrInvalidBaseBranch
		}
	}
	return branch, nil
}

// Scope enforces the length cap on an allowed/forbidden scope value. Unlike
// BaseBranch this is free text (path globs, prose), not a git ref name, so
// there is no format restriction.
func Scope(raw string) (string, error) {
	if utf8.RuneCountInString(raw) > MaxScopeFieldLength {
		return "", ErrInvalidScope
	}
	return raw, nil
}

// Schedule rejects a period that ends before it starts. Either date alone is
// fine: an object with only a due date is a deadline without a committed
// start, and the timeline draws it as a single day.
func Schedule(startDate, dueOn *time.Time) error {
	if startDate != nil && dueOn != nil && startDate.After(*dueOn) {
		return ErrInvalidSchedule
	}
	return nil
}
