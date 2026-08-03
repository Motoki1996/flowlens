package project_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/issuesync"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_Create_ValidatesName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  My Project  ", "My Project", nil},
		{"rejects empty after trim", "   ", "", project.ErrInvalidName},
		{"rejects too long", strings.Repeat("a", 101), "", project.ErrInvalidName},
		{"accepts max length", strings.Repeat("a", 100), strings.Repeat("a", 100), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := project.NewService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID

			p, err := svc.Create(context.Background(), owner, tt.input, "")
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.Name)
		})
	}
}

func TestService_Create_RejectsDuplicateNameForSameOwner(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	_, err := svc.Create(context.Background(), owner, "Alpha", "")
	require.NoError(t, err)

	_, err = svc.Create(context.Background(), owner, "Alpha", "")
	assert.ErrorIs(t, err, project.ErrNameTaken)
}

func TestService_Create_AllowsSameNameForDifferentOwners(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner1 := q.SeedUser("octocat", "octocat@example.com").ID
	owner2 := q.SeedUser("hubot", "hubot@example.com").ID

	_, err := svc.Create(context.Background(), owner1, "Alpha", "")
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), owner2, "Alpha", "")
	assert.NoError(t, err)
}

// Ownership is enforced inside every method, so a non-owner is told the
// project does not exist for reads and is refused for writes.
func TestService_ScopesEveryOperationToOwner(t *testing.T) {
	ctx := context.Background()

	t.Run("get", func(t *testing.T) {
		q := dbtest.New()
		svc := project.NewService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")

		_, err := svc.Get(ctx, other, p.ID)
		assert.ErrorIs(t, err, project.ErrNotFound)
	})

	t.Run("update leaves the project untouched", func(t *testing.T) {
		q := dbtest.New()
		svc := project.NewService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")

		_, err := svc.Update(ctx, other, p.ID, "Hijacked", "")
		require.ErrorIs(t, err, project.ErrNotFound)

		still, err := svc.Get(ctx, owner, p.ID)
		require.NoError(t, err)
		assert.Equal(t, "Alpha", still.Name)
	})

	t.Run("delete leaves the project in place", func(t *testing.T) {
		q := dbtest.New()
		svc := project.NewService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")

		err := svc.Delete(ctx, other, p.ID)
		require.ErrorIs(t, err, project.ErrNotFound)

		_, err = svc.Get(ctx, owner, p.ID)
		assert.NoError(t, err)
	})
}

func TestService_Get_ReturnsNotFoundForMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	_, err := svc.Get(context.Background(), owner, uuid.New())
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_List_ScopesToOwner(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	q.SeedProject(owner, "Alpha")
	q.SeedProject(owner, "Beta")
	q.SeedProject(other, "Gamma")

	projects, err := svc.List(context.Background(), owner)
	require.NoError(t, err)
	assert.Len(t, projects, 2)
}

func TestService_ListFailedSync_ReturnsOnlyProjectsWithFailures(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID

	clean := q.SeedProject(owner, "Clean")
	q.SeedTask(clean.ID, owner, "Fine")

	broken := q.SeedProject(owner, "Broken")
	failedTask := q.SeedTask(broken.ID, owner, "Broken task")
	q.SeedSyncJobForTask(failedTask.ID, broken.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")

	otherBroken := q.SeedProject(other, "Other's broken project")
	otherFailedTask := q.SeedTask(otherBroken.ID, other, "Not mine")
	q.SeedSyncJobForTask(otherFailedTask.ID, otherBroken.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")

	projects, err := svc.ListFailedSync(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "Broken", projects[0].Name)
	assert.Equal(t, int64(1), projects[0].FailedSyncTaskCount)
}

func TestService_Update_RejectsDuplicateName(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	q.SeedProject(owner, "Alpha")
	beta := q.SeedProject(owner, "Beta")

	_, err := svc.Update(context.Background(), owner, beta.ID, "Alpha", "")
	assert.ErrorIs(t, err, project.ErrNameTaken)
}

func TestService_Update_ChangesNameAndDescription(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	updated, err := svc.Update(context.Background(), owner, p.ID, "Renamed", "new description")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Equal(t, "new description", updated.Description)
}

func TestService_Delete_RemovesProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	require.NoError(t, svc.Delete(context.Background(), owner, p.ID))

	_, err := svc.Get(context.Background(), owner, p.ID)
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_Delete_ReturnsNotFoundForMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	assert.ErrorIs(t, svc.Delete(context.Background(), owner, uuid.New()), project.ErrNotFound)
}

func TestCreate_ReturnsDomainProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	p, err := svc.Create(context.Background(), owner, "Alpha", "desc")
	require.NoError(t, err)

	assert.Equal(t, "Alpha", p.Name)
	assert.Equal(t, "desc", p.Description)
	assert.NotEqual(t, uuid.Nil, p.ID)
	assert.False(t, p.CreatedAt.IsZero())
}
