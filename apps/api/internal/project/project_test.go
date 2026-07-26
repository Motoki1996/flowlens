package project_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/jackc/pgx/v5/pgtype"
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

			row, err := svc.Create(context.Background(), owner, tt.input, "")
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, row.Name)
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

func TestService_Get_ReturnsNotFoundForOtherOwner(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Get(context.Background(), other, p.ID)
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_Get_ReturnsNotFoundForMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	_, err := svc.Get(context.Background(), owner, pgtype.UUID{Valid: true})
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

	rows, err := svc.List(context.Background(), owner)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func TestService_Update_RejectsDuplicateName(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	q.SeedProject(owner, "Alpha")
	beta := q.SeedProject(owner, "Beta")

	_, err := svc.Update(context.Background(), beta.ID, "Alpha", "")
	assert.ErrorIs(t, err, project.ErrNameTaken)
}

func TestService_Update_ChangesNameAndDescription(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	row, err := svc.Update(context.Background(), p.ID, "Renamed", "new description")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", row.Name)
	assert.Equal(t, "new description", row.Description)
}

func TestService_Delete_RemovesProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	require.NoError(t, svc.Delete(context.Background(), p.ID))

	_, err := svc.Get(context.Background(), owner, p.ID)
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestFromDB_MapsFields(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	row, err := svc.Create(context.Background(), owner, "Alpha", "desc")
	require.NoError(t, err)

	dto := project.FromDB(row)
	assert.Equal(t, "Alpha", dto.Name)
	assert.Equal(t, "desc", dto.Description)
	assert.NotEmpty(t, dto.ID)
}
