package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_BulkCreate_CreatesTasksAndDependencies(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	result, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{
			{Ref: "t1", CreateParams: task.CreateParams{Title: "Design"}},
			{Ref: "t2", CreateParams: task.CreateParams{Title: "Implement", Priority: "high", Size: "l"}},
			{Ref: "t3", CreateParams: task.CreateParams{Title: "Ship"}, AIContext: &task.AIContextParams{AcceptanceCriteria: "shipped"}},
		},
		Dependencies: []task.BulkDependencyParams{
			{PredecessorRef: "t1", SuccessorRef: "t2"},
			{PredecessorRef: "t2", SuccessorRef: "t3"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Tasks, 3)
	require.Len(t, result.Dependencies, 2)

	byRef := make(map[string]task.Task, 3)
	for _, bt := range result.Tasks {
		byRef[bt.Ref] = bt.Task
	}
	assert.Equal(t, "Design", byRef["t1"].Title)
	assert.Equal(t, "high", byRef["t2"].Priority)
	assert.Equal(t, "l", byRef["t2"].Size)
	assert.Equal(t, "shipped", byRef["t3"].AIContext.AcceptanceCriteria)

	assert.Equal(t, byRef["t1"].ID, result.Dependencies[0].PredecessorTaskID)
	assert.Equal(t, byRef["t2"].ID, result.Dependencies[0].SuccessorTaskID)
}

func TestService_BulkCreate_RejectsEmptyTasks(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrBulkTasksEmpty)
}

func TestService_BulkCreate_RejectsTooManyTasks(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	tasks := make([]task.BulkTaskParams, 101)
	for i := range tasks {
		tasks[i] = task.BulkTaskParams{Ref: string(rune('a' + i%26)), CreateParams: task.CreateParams{Title: "T"}}
	}
	_, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{Tasks: tasks})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrBulkTooManyTasks)
}

func TestService_BulkCreate_RejectsDuplicateRef(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{
			{Ref: "t1", CreateParams: task.CreateParams{Title: "A"}},
			{Ref: "t1", CreateParams: task.CreateParams{Title: "B"}},
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrBulkDuplicateRef)
	var bulkErr *task.BulkError
	require.True(t, errors.As(err, &bulkErr))
	assert.Equal(t, "t1", bulkErr.Ref)
}

func TestService_BulkCreate_RejectsInvalidTaskField(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{
			{Ref: "t1", CreateParams: task.CreateParams{Title: ""}},
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrInvalidTitle)

	// Nothing should have been written: no tasks in the project.
	tasks, err := svc.List(context.Background(), owner, p.ID, task.ListFilter{})
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestService_BulkCreate_RejectsUnknownDependencyRef(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{
			{Ref: "t1", CreateParams: task.CreateParams{Title: "A"}},
		},
		Dependencies: []task.BulkDependencyParams{
			{PredecessorRef: "t1", SuccessorRef: "ghost"},
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrBulkUnknownRef)
}

func TestService_BulkCreate_RejectsSelfDependency(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{
			{Ref: "t1", CreateParams: task.CreateParams{Title: "A"}},
		},
		Dependencies: []task.BulkDependencyParams{
			{PredecessorRef: "t1", SuccessorRef: "t1"},
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrBulkSelfDependency)
}

func TestService_BulkCreate_RejectsCycleWithinBatch(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{
			{Ref: "t1", CreateParams: task.CreateParams{Title: "A"}},
			{Ref: "t2", CreateParams: task.CreateParams{Title: "B"}},
			{Ref: "t3", CreateParams: task.CreateParams{Title: "C"}},
		},
		Dependencies: []task.BulkDependencyParams{
			{PredecessorRef: "t1", SuccessorRef: "t2"},
			{PredecessorRef: "t2", SuccessorRef: "t3"},
			{PredecessorRef: "t3", SuccessorRef: "t1"},
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrBulkCyclicDependency)
}

func TestService_BulkCreate_ForeignProjectReturnsNotFound(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(other, "Alpha")

	_, err := svc.BulkCreate(context.Background(), owner, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{{Ref: "t1", CreateParams: task.CreateParams{Title: "A"}}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrNotFound)
}
