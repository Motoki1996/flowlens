package epic

import (
	"context"

	"github.com/flowlens/api/internal/assignee"
	"github.com/google/uuid"
)

// attachAssigneeNames fills in AssigneeUsername/AssigneeDisplayName across
// epics in one query, skipping any already resolved. An epic has no GitLab
// assignee axis, so this is the only assignee resolution it needs — the same
// as internal/backlog's, and unlike internal/task's, which also bridges to
// GitLab.
func (s *Service) attachAssigneeNames(ctx context.Context, epics []*Epic) error {
	ids := make([]*uuid.UUID, 0, len(epics))
	for _, e := range epics {
		if e.AssigneeUserID == nil || e.AssigneeUsername != "" {
			continue
		}
		ids = append(ids, e.AssigneeUserID)
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := assignee.Names(ctx, s.q, ids)
	if err != nil {
		return err
	}
	for _, e := range epics {
		if e.AssigneeUserID == nil {
			continue
		}
		if n, ok := names[*e.AssigneeUserID]; ok {
			e.AssigneeUsername, e.AssigneeDisplayName = n.Username, n.DisplayName
		}
	}
	return nil
}

// attachAssigneeName is attachAssigneeNames for a single epic.
func (s *Service) attachAssigneeName(ctx context.Context, e *Epic) error {
	return s.attachAssigneeNames(ctx, []*Epic{e})
}

// attachAssigneeNamesToList is attachAssigneeNames over a value slice.
func (s *Service) attachAssigneeNamesToList(ctx context.Context, epics []Epic) error {
	ptrs := make([]*Epic, len(epics))
	for i := range epics {
		ptrs[i] = &epics[i]
	}
	return s.attachAssigneeNames(ctx, ptrs)
}
