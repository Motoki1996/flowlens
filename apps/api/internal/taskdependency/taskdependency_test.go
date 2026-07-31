package taskdependency_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/task"
	"github.com/flowlens/api/internal/taskdependency"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *taskdependency.Service {
	projects := project.NewService(q)
	backlogs := backlog.NewService(q, projects)
	tasks := task.NewService(q, dbtest.FakeTxRunner{Q: q}, projects, backlogs)
	return taskdependency.NewService(q, projects, tasks)
}

func TestService_Create_LinksPredecessorAndSuccessor(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	design := q.SeedTask(p.ID, owner, "Design")
	build := q.SeedTask(p.ID, owner, "Build")

	d, err := svc.Create(ctx, owner, p.ID, design.ID, build.ID)
	require.NoError(t, err)
	assert.Equal(t, design.ID, d.PredecessorTaskID)
	assert.Equal(t, build.ID, d.SuccessorTaskID)
}

func TestService_Create_RejectsSelfDependency(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	design := q.SeedTask(p.ID, owner, "Design")

	_, err := svc.Create(ctx, owner, p.ID, design.ID, design.ID)
	assert.ErrorIs(t, err, taskdependency.ErrSelfDependency)
}

func TestService_Create_RejectsTaskFromAnotherProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p1 := q.SeedProject(owner, "Alpha")
	p2 := q.SeedProject(owner, "Beta")
	design := q.SeedTask(p1.ID, owner, "Design")
	other := q.SeedTask(p2.ID, owner, "Other")

	_, err := svc.Create(ctx, owner, p1.ID, design.ID, other.ID)
	assert.ErrorIs(t, err, taskdependency.ErrTaskNotInProject)
}

func TestService_Create_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	design := q.SeedTask(p.ID, owner, "Design")
	build := q.SeedTask(p.ID, owner, "Build")

	_, err := svc.Create(ctx, other, p.ID, design.ID, build.ID)
	assert.ErrorIs(t, err, taskdependency.ErrNotFound)
}

func TestService_Create_ReturnsNotFoundForForeignTask(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	otherProject := q.SeedProject(other, "Other")
	design := q.SeedTask(p.ID, owner, "Design")
	foreignTask := q.SeedTask(otherProject.ID, other, "Foreign")

	_, err := svc.Create(ctx, owner, p.ID, design.ID, foreignTask.ID)
	assert.ErrorIs(t, err, taskdependency.ErrNotFound)
}

// A direct cycle: Design -> Build already exists, so Build -> Design would
// loop back to Design.
func TestService_Create_RejectsDirectCycle(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	design := q.SeedTask(p.ID, owner, "Design")
	build := q.SeedTask(p.ID, owner, "Build")

	_, err := svc.Create(ctx, owner, p.ID, design.ID, build.ID)
	require.NoError(t, err)

	_, err = svc.Create(ctx, owner, p.ID, build.ID, design.ID)
	assert.ErrorIs(t, err, taskdependency.ErrCyclicDependency)
}

// A transitive cycle: Design -> Build -> Test already exists, so
// Test -> Design would close a longer loop, not just a direct one.
func TestService_Create_RejectsTransitiveCycle(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	design := q.SeedTask(p.ID, owner, "Design")
	build := q.SeedTask(p.ID, owner, "Build")
	test := q.SeedTask(p.ID, owner, "Test")

	_, err := svc.Create(ctx, owner, p.ID, design.ID, build.ID)
	require.NoError(t, err)
	_, err = svc.Create(ctx, owner, p.ID, build.ID, test.ID)
	require.NoError(t, err)

	_, err = svc.Create(ctx, owner, p.ID, test.ID, design.ID)
	assert.ErrorIs(t, err, taskdependency.ErrCyclicDependency)
}

// Two independent predecessors of the same successor is not a cycle: it is
// the ordinary "fan-in" shape a real schedule needs.
func TestService_Create_AllowsSharedSuccessorWithoutCycle(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	design := q.SeedTask(p.ID, owner, "Design")
	review := q.SeedTask(p.ID, owner, "Review")
	launch := q.SeedTask(p.ID, owner, "Launch")

	_, err := svc.Create(ctx, owner, p.ID, design.ID, launch.ID)
	require.NoError(t, err)
	_, err = svc.Create(ctx, owner, p.ID, review.ID, launch.ID)
	assert.NoError(t, err)
}

func TestService_List_ReturnsOnlyDependenciesInProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p1 := q.SeedProject(owner, "Alpha")
	p2 := q.SeedProject(owner, "Beta")
	design := q.SeedTask(p1.ID, owner, "Design")
	build := q.SeedTask(p1.ID, owner, "Build")
	other1 := q.SeedTask(p2.ID, owner, "Other1")
	other2 := q.SeedTask(p2.ID, owner, "Other2")

	_, err := svc.Create(ctx, owner, p1.ID, design.ID, build.ID)
	require.NoError(t, err)
	_, err = svc.Create(ctx, owner, p2.ID, other1.ID, other2.ID)
	require.NoError(t, err)

	deps, err := svc.List(ctx, owner, p1.ID)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, design.ID, deps[0].PredecessorTaskID)
}

func TestService_List_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.List(ctx, other, p.ID)
	assert.ErrorIs(t, err, taskdependency.ErrNotFound)
}

func TestService_Delete_RemovesDependency(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	design := q.SeedTask(p.ID, owner, "Design")
	build := q.SeedTask(p.ID, owner, "Build")
	d, err := svc.Create(ctx, owner, p.ID, design.ID, build.ID)
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctx, owner, d.ID))

	deps, err := svc.List(ctx, owner, p.ID)
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestService_Delete_ReturnsNotFoundForForeignDependency(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	design := q.SeedTask(p.ID, owner, "Design")
	build := q.SeedTask(p.ID, owner, "Build")
	d, err := svc.Create(ctx, owner, p.ID, design.ID, build.ID)
	require.NoError(t, err)

	err = svc.Delete(ctx, other, d.ID)
	assert.ErrorIs(t, err, taskdependency.ErrNotFound)
}

func TestService_Delete_ReturnsNotFoundForMissingDependency(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	err := svc.Delete(context.Background(), owner, uuid.New())
	assert.ErrorIs(t, err, taskdependency.ErrNotFound)
}
