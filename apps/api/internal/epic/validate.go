package epic

import (
	"time"

	"github.com/flowlens/api/internal/fieldnorm"
)

// normalizeInput is the set of free-form fields Create and Update both have
// to validate. Update resolves each Optional against the stored row before
// calling normalize, so both paths hand over plain values and the rules are
// applied in exactly one place.
type normalizeInput struct {
	Name           string
	StartDate      *time.Time
	DueOn          *time.Time
	Priority       string
	Progress       string
	BaseBranch     string
	AllowedScope   string
	ForbiddenScope string
}

// normalizedFields are normalizeInput's values after defaulting and
// trimming. The dates are absent: validateSchedule only rejects them, it
// never rewrites them.
type normalizedFields struct {
	Name           string
	Priority       string
	Progress       string
	BaseBranch     string
	AllowedScope   string
	ForbiddenScope string
}

// normalize applies internal/fieldnorm's rules — the same ones
// internal/backlog applies, since an epic and a backlog share these fields by
// design (000032 migration) — and maps each failure to this package's own
// sentinel so handlers can key their error mapping on internal/epic alone.
func normalize(in normalizeInput) (normalizedFields, error) {
	name, err := fieldnorm.Name(in.Name)
	if err != nil {
		return normalizedFields{}, ErrInvalidName
	}
	if err := fieldnorm.Schedule(in.StartDate, in.DueOn); err != nil {
		return normalizedFields{}, ErrInvalidSchedule
	}
	priority, err := fieldnorm.Priority(in.Priority)
	if err != nil {
		return normalizedFields{}, ErrInvalidPriority
	}
	progress, err := fieldnorm.Progress(in.Progress)
	if err != nil {
		return normalizedFields{}, ErrInvalidProgress
	}
	baseBranch, err := fieldnorm.BaseBranch(in.BaseBranch)
	if err != nil {
		return normalizedFields{}, ErrInvalidBaseBranch
	}
	allowedScope, err := fieldnorm.Scope(in.AllowedScope)
	if err != nil {
		return normalizedFields{}, ErrInvalidScope
	}
	forbiddenScope, err := fieldnorm.Scope(in.ForbiddenScope)
	if err != nil {
		return normalizedFields{}, ErrInvalidScope
	}
	return normalizedFields{
		Name:           name,
		Priority:       priority,
		Progress:       progress,
		BaseBranch:     baseBranch,
		AllowedScope:   allowedScope,
		ForbiddenScope: forbiddenScope,
	}, nil
}
