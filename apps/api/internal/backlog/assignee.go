package backlog

import (
	"context"

	"github.com/flowlens/api/internal/assignee"
	"github.com/google/uuid"
)

// ErrAssigneeNotMember re-exports internal/assignee's sentinel so handlers can
// map it without importing that package directly.
var ErrAssigneeNotMember = assignee.ErrNotMember

// attachAssigneeNames fills in AssigneeUsername/AssigneeDisplayName across
// backlogs in one query, skipping any already resolved. A backlog has no
// GitLab assignee axis, so this is the only assignee resolution it needs —
// contrast internal/task's, which also bridges to GitLab.
func (s *Service) attachAssigneeNames(ctx context.Context, backlogs []*Backlog) error {
	ids := make([]*uuid.UUID, 0, len(backlogs))
	for _, b := range backlogs {
		if b.AssigneeUserID == nil || b.AssigneeUsername != "" {
			continue
		}
		ids = append(ids, b.AssigneeUserID)
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := assignee.Names(ctx, s.q, ids)
	if err != nil {
		return err
	}
	for _, b := range backlogs {
		if b.AssigneeUserID == nil {
			continue
		}
		if n, ok := names[*b.AssigneeUserID]; ok {
			b.AssigneeUsername, b.AssigneeDisplayName = n.Username, n.DisplayName
		}
	}
	return nil
}

// attachAssigneeName is attachAssigneeNames for a single backlog.
func (s *Service) attachAssigneeName(ctx context.Context, b *Backlog) error {
	return s.attachAssigneeNames(ctx, []*Backlog{b})
}

// attachAssigneeNamesToList is attachAssigneeNames over a value slice.
func (s *Service) attachAssigneeNamesToList(ctx context.Context, backlogs []Backlog) error {
	ptrs := make([]*Backlog, len(backlogs))
	for i := range backlogs {
		ptrs[i] = &backlogs[i]
	}
	return s.attachAssigneeNames(ctx, ptrs)
}
