package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
)

// maxBulkTasks bounds how many tasks one BulkCreate request may create, so a
// single request can't tie up a transaction (or the outbox) indefinitely.
const maxBulkTasks = 100

// Sentinel errors specific to BulkCreate. Handlers map these to HTTP status
// codes the same way as the package-level ones above; a BulkError wraps
// whichever of these (or the normal per-task sentinels, e.g.
// ErrInvalidTitle) actually failed, adding the request-scoped ref it
// applies to.
var (
	ErrBulkTasksEmpty       = errors.New("task: bulk request must include at least one task")
	ErrBulkTooManyTasks     = errors.New("task: bulk request exceeds the maximum of 100 tasks")
	ErrBulkRefEmpty         = errors.New("task: bulk task ref must not be empty")
	ErrBulkDuplicateRef     = errors.New("task: bulk request has a duplicate ref")
	ErrBulkUnknownRef       = errors.New("task: bulk dependency references a ref not present in tasks")
	ErrBulkSelfDependency   = errors.New("task: bulk dependency cannot reference the same ref twice")
	ErrBulkCyclicDependency = errors.New("task: bulk request would create a cyclic dependency")
)

// BulkError wraps a BulkCreate validation or write failure with the
// request-scoped ref (see BulkTaskParams.Ref) it applies to, so a caller can
// tell the client which task or dependency was rejected. Ref is empty for
// request-level errors that aren't about one particular ref (e.g. too many
// tasks).
type BulkError struct {
	Ref string
	Err error
}

func (e *BulkError) Error() string {
	if e.Ref == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s (ref %q)", e.Err, e.Ref)
}

func (e *BulkError) Unwrap() error { return e.Err }

// BulkTaskParams is one task in a BulkCreateParams request: the same fields
// as CreateParams, plus Ref (this request's only handle on a task that has
// no UUID yet — see BulkDependencyParams) and an optional AI context to
// create alongside it.
type BulkTaskParams struct {
	Ref string
	CreateParams
	AIContext *AIContextParams
}

// BulkDependencyParams declares a predecessor/successor edge between two
// tasks in the same BulkCreateParams request, by ref.
//
// Unlike the single-dependency endpoint (POST .../task-dependencies), a ref
// here must name a task created in this same request — it cannot point at
// an already-existing task's UUID. The single-dependency endpoint already
// covers existing-to-existing edges; bulk's whole purpose is wiring
// together the batch it just created, so that's the only case worth
// supporting here.
type BulkDependencyParams struct {
	PredecessorRef string
	SuccessorRef   string
}

// BulkCreateParams is the input to BulkCreate.
type BulkCreateParams struct {
	Tasks        []BulkTaskParams
	Dependencies []BulkDependencyParams
}

// BulkCreatedTask pairs a created Task with the ref its request used, so a
// caller can resolve its own local ref to the task's real ID.
type BulkCreatedTask struct {
	Ref  string `json:"ref"`
	Task Task   `json:"task"`
}

// BulkCreatedDependency is the API-facing representation of a dependency
// created by BulkCreate. It mirrors internal/taskdependency.TaskDependency's
// shape; a separate type here avoids task importing taskdependency, which
// already imports task (see wouldCycleAmong's doc comment).
type BulkCreatedDependency struct {
	ID                uuid.UUID `json:"id"`
	PredecessorTaskID uuid.UUID `json:"predecessorTaskId"`
	SuccessorTaskID   uuid.UUID `json:"successorTaskId"`
	CreatedAt         time.Time `json:"createdAt"`
}

// BulkCreateResult is BulkCreate's return value.
type BulkCreateResult struct {
	Tasks        []BulkCreatedTask       `json:"tasks"`
	Dependencies []BulkCreatedDependency `json:"dependencies"`
}

// depEdge is the minimal predecessor/successor pair wouldCycleAmong needs.
type depEdge struct {
	PredecessorTaskID uuid.UUID
	SuccessorTaskID   uuid.UUID
}

// wouldCycleAmong reports whether adding predecessorID -> successorID to
// edges would create a cycle, by BFS from successorID looking for a path
// back to predecessorID. It mirrors
// internal/taskdependency.wouldCycle's algorithm exactly; it is duplicated
// here rather than imported because internal/taskdependency already imports
// internal/task; task importing back would cycle. Keep the two in sync if
// either changes.
func wouldCycleAmong(edges []depEdge, predecessorID, successorID uuid.UUID) bool {
	if predecessorID == successorID {
		return true
	}
	successorsOf := make(map[uuid.UUID][]uuid.UUID, len(edges))
	for _, e := range edges {
		successorsOf[e.PredecessorTaskID] = append(successorsOf[e.PredecessorTaskID], e.SuccessorTaskID)
	}
	visited := map[uuid.UUID]bool{successorID: true}
	queue := []uuid.UUID{successorID}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == predecessorID {
			return true
		}
		for _, n := range successorsOf[next] {
			if !visited[n] {
				visited[n] = true
				queue = append(queue, n)
			}
		}
	}
	return false
}

// normalizedBulkTask holds one task's post-validation fields, computed
// before the transaction opens so a bad task never partially writes.
type normalizedBulkTask struct {
	params                          BulkTaskParams
	title, priority, progress, size string
}

// BulkCreate creates every task in params.Tasks and every dependency in
// params.Dependencies in a single transaction: either the whole batch
// commits (all tasks, all dependencies, all issue.create outbox jobs for
// linked tasks) or none of it does. See CreateParams/Create for what a
// single task write does; BulkCreate repeats exactly that per task via
// createTaskInTx.
//
// Every field is validated for every task and dependency before the
// transaction opens, so a request that will fail never touches the
// database. A validation or write failure returns a *BulkError naming the
// offending ref.
func (s *Service) BulkCreate(ctx context.Context, ownerID, projectID uuid.UUID, params BulkCreateParams) (BulkCreateResult, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleMember); err != nil {
		return BulkCreateResult{}, err
	}
	if len(params.Tasks) == 0 {
		return BulkCreateResult{}, &BulkError{Err: ErrBulkTasksEmpty}
	}
	if len(params.Tasks) > maxBulkTasks {
		return BulkCreateResult{}, &BulkError{Err: ErrBulkTooManyTasks}
	}

	normalized := make([]normalizedBulkTask, len(params.Tasks))
	refs := make(map[string]bool, len(params.Tasks))
	for i, tp := range params.Tasks {
		if tp.Ref == "" {
			return BulkCreateResult{}, &BulkError{Err: ErrBulkRefEmpty}
		}
		if refs[tp.Ref] {
			return BulkCreateResult{}, &BulkError{Ref: tp.Ref, Err: ErrBulkDuplicateRef}
		}
		refs[tp.Ref] = true

		title, err := normalizeTitle(tp.Title)
		if err != nil {
			return BulkCreateResult{}, &BulkError{Ref: tp.Ref, Err: err}
		}
		priority, err := normalizePriority(tp.Priority)
		if err != nil {
			return BulkCreateResult{}, &BulkError{Ref: tp.Ref, Err: err}
		}
		progress, err := normalizeProgress(tp.Progress)
		if err != nil {
			return BulkCreateResult{}, &BulkError{Ref: tp.Ref, Err: err}
		}
		size, err := normalizeSize(tp.Size)
		if err != nil {
			return BulkCreateResult{}, &BulkError{Ref: tp.Ref, Err: err}
		}
		if err := s.validateBacklog(ctx, ownerID, projectID, tp.BacklogID); err != nil {
			return BulkCreateResult{}, &BulkError{Ref: tp.Ref, Err: err}
		}
		if tp.AIContext != nil {
			if err := validateAIContextParams(*tp.AIContext); err != nil {
				return BulkCreateResult{}, &BulkError{Ref: tp.Ref, Err: err}
			}
		}
		normalized[i] = normalizedBulkTask{params: tp, title: title, priority: priority, progress: progress, size: size}
	}

	for _, dep := range params.Dependencies {
		if dep.PredecessorRef == dep.SuccessorRef {
			return BulkCreateResult{}, &BulkError{Ref: dep.PredecessorRef, Err: ErrBulkSelfDependency}
		}
		if !refs[dep.PredecessorRef] {
			return BulkCreateResult{}, &BulkError{Ref: dep.PredecessorRef, Err: ErrBulkUnknownRef}
		}
		if !refs[dep.SuccessorRef] {
			return BulkCreateResult{}, &BulkError{Ref: dep.SuccessorRef, Err: ErrBulkUnknownRef}
		}
	}

	var result BulkCreateResult
	err := s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		refToID := make(map[string]uuid.UUID, len(normalized))
		result.Tasks = make([]BulkCreatedTask, 0, len(normalized))
		for _, nt := range normalized {
			created, err := createTaskInTx(ctx, q, ownerID, projectID, nt.title, nt.priority, nt.progress, nt.size, nt.params.CreateParams)
			if err != nil {
				return &BulkError{Ref: nt.params.Ref, Err: err}
			}
			if nt.params.AIContext != nil {
				row, err := q.UpsertTaskAIContext(ctx, db.UpsertTaskAIContextParams{
					TaskID:             created.ID,
					AcceptanceCriteria: nt.params.AIContext.AcceptanceCriteria,
					AiContext:          nt.params.AIContext.AIContext,
					AllowedScope:       nt.params.AIContext.AllowedScope,
					ForbiddenScope:     nt.params.AIContext.ForbiddenScope,
				})
				if err != nil {
					return &BulkError{Ref: nt.params.Ref, Err: fmt.Errorf("task: bulk create: upsert ai context: %w", err)}
				}
				created.AIContext = aiContextFromRow(row)
			}
			refToID[nt.params.Ref] = created.ID
			result.Tasks = append(result.Tasks, BulkCreatedTask{Ref: nt.params.Ref, Task: created})
		}

		// No need to seed edges from ListTaskDependenciesByProject here:
		// every dependency in this request connects two refs from
		// params.Tasks (BulkDependencyParams' doc comment), i.e. two tasks
		// that didn't exist before this transaction, so no pre-existing
		// edge can touch either endpoint.
		edges := make([]depEdge, 0, len(params.Dependencies))
		result.Dependencies = make([]BulkCreatedDependency, 0, len(params.Dependencies))
		for _, dep := range params.Dependencies {
			predID, succID := refToID[dep.PredecessorRef], refToID[dep.SuccessorRef]
			if wouldCycleAmong(edges, predID, succID) {
				return &BulkError{Ref: dep.PredecessorRef, Err: ErrBulkCyclicDependency}
			}
			row, err := q.CreateTaskDependency(ctx, db.CreateTaskDependencyParams{
				PredecessorTaskID: predID,
				SuccessorTaskID:   succID,
			})
			if err != nil {
				return fmt.Errorf("task: bulk create: create dependency: %w", err)
			}
			edges = append(edges, depEdge{PredecessorTaskID: predID, SuccessorTaskID: succID})
			result.Dependencies = append(result.Dependencies, BulkCreatedDependency{
				ID:                row.ID,
				PredecessorTaskID: row.PredecessorTaskID,
				SuccessorTaskID:   row.SuccessorTaskID,
				CreatedAt:         row.CreatedAt.Time,
			})
		}
		return nil
	})
	if err != nil {
		return BulkCreateResult{}, err
	}
	return result, nil
}
