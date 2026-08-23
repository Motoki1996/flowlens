package task

import (
	"context"

	"github.com/flowlens/api/internal/assignee"
	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
)

// ErrAssigneeNotMember re-exports internal/assignee's sentinel so handlers
// can map it without importing that package directly, the same way
// ErrBacklogNotInProject surfaces a cross-package rule here.
var ErrAssigneeNotMember = assignee.ErrNotMember

// bridgeAssigneeToGitlab resolves what an explicitly-requested FlowLens
// assignee means for the two GitLab assignee columns — the one-way bridge the
// 000031 migration describes. It reports the GitLab user ID and username to
// write, and never reads the current values: callers apply the result on top
// of an already-resolved update.
//
// The rules, in order:
//
//   - Unassigning (userID nil) clears the GitLab assignee too. Leaving a
//     GitLab assignee behind on a task nobody owns is the misleading outcome.
//   - An assignee with a registered identity for this project's GitLab
//     connection sets both columns, which is what puts the change on the
//     issue (the mirrored-field diff in Update then enqueues issue.update).
//   - An assignee with no identity clears the GitLab columns rather than
//     leaving whoever was there: the work belongs to the new assignee now, and
//     a stale GitLab assignee would contradict that.
//
// Callers must have already rejected a non-member (ValidateMember).
func bridgeAssigneeToGitlab(ctx context.Context, q db.Querier, projectID uuid.UUID, userID *uuid.UUID) (*int64, string, error) {
	if userID == nil {
		return nil, "", nil
	}
	id, err := assignee.ResolveGitlab(ctx, q, projectID, *userID)
	if err != nil {
		return nil, "", err
	}
	if !id.Found {
		return nil, "", nil
	}
	gitlabID := id.UserID
	return &gitlabID, id.Username, nil
}

// applyAssigneeUpdate validates and applies params' FlowLens assignee onto
// resolved, including the GitLab bridge. An absent AssigneeUserID leaves both
// axes exactly as they were — that is what keeps a PATCH of some unrelated
// field from reassigning the GitLab issue as a side effect.
//
// An explicit assigneeGitlabUserId in the same request always wins over the
// bridge: the caller named a specific GitLab user, which is also how the
// GitLab-members picker (for someone with no FlowLens account) keeps working.
func applyAssigneeUpdate(ctx context.Context, q db.Querier, projectID uuid.UUID, params UpdateParams, resolved *resolvedUpdate) error {
	userID, present := params.AssigneeUserID.Get()
	if !present {
		return nil
	}
	if userID != nil {
		if err := assignee.ValidateMember(ctx, q, projectID, *userID); err != nil {
			return err
		}
	}
	if _, gitlabSet := params.AssigneeGitlabUserID.Get(); gitlabSet {
		return nil
	}
	gitlabID, gitlabUsername, err := bridgeAssigneeToGitlab(ctx, q, projectID, userID)
	if err != nil {
		return err
	}
	resolved.AssigneeGitlabUserID = gitlabID
	resolved.AssigneeGitlabUsername = gitlabUsername
	return nil
}

// attachAssigneeNames fills in AssigneeUsername/AssigneeDisplayName across
// tasks in one query. Read paths call it after building their result, so a
// list costs one extra round trip regardless of how many distinct assignees
// it holds.
func (s *Service) attachAssigneeNames(ctx context.Context, tasks []*Task) error {
	// Already-resolved tasks are skipped, which is what makes the call cheap
	// to repeat: contextForTask attaches names for the paths that hand it a
	// raw row, and costs nothing on the path that came through Get.
	ids := make([]*uuid.UUID, 0, len(tasks))
	for _, t := range tasks {
		if t.AssigneeUserID == nil || t.AssigneeUsername != "" {
			continue
		}
		ids = append(ids, t.AssigneeUserID)
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := assignee.Names(ctx, s.q, ids)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.AssigneeUserID == nil {
			continue
		}
		if n, ok := names[*t.AssigneeUserID]; ok {
			t.AssigneeUsername, t.AssigneeDisplayName = n.Username, n.DisplayName
		}
	}
	return nil
}

// attachAssigneeName is attachAssigneeNames for a single task.
func (s *Service) attachAssigneeName(ctx context.Context, t *Task) error {
	return s.attachAssigneeNames(ctx, []*Task{t})
}

// attachAssigneeNamesToTasks is attachAssigneeNames over a value slice, the
// shape every list path already holds.
func (s *Service) attachAssigneeNamesToTasks(ctx context.Context, tasks []Task) error {
	ptrs := make([]*Task, len(tasks))
	for i := range tasks {
		ptrs[i] = &tasks[i]
	}
	return s.attachAssigneeNames(ctx, ptrs)
}

// attachAssigneeNamesToCrossProject is attachAssigneeNamesToTasks for the
// cross-project collection's embedded Task.
func (s *Service) attachAssigneeNamesToCrossProject(ctx context.Context, tasks []TaskWithProject) error {
	ptrs := make([]*Task, len(tasks))
	for i := range tasks {
		ptrs[i] = &tasks[i].Task
	}
	return s.attachAssigneeNames(ctx, ptrs)
}
