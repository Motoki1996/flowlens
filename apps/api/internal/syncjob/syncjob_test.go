package syncjob_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/syncjob"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListFailed_ReturnsOnlyFailedJobsForOwnedProject(t *testing.T) {
	q := dbtest.New()
	svc := syncjob.NewService(q, project.NewService(q))

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	other := q.SeedUser("other", "other@example.com")
	otherProject := q.SeedProject(other.ID, "Beta")

	failed := q.SeedSyncJob(p.ID, "issue.update", "failed", time.Now())
	q.SeedSyncJob(p.ID, "issue.update", "succeeded", time.Now())
	q.SeedSyncJob(otherProject.ID, "issue.update", "failed", time.Now())

	jobs, err := svc.ListFailed(context.Background(), owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, failed.ID, jobs[0].ID)
	assert.Equal(t, "failed", jobs[0].Status)
}

func TestListFailed_UnknownOrForeignProject_ReturnsErrNotFound(t *testing.T) {
	q := dbtest.New()
	svc := syncjob.NewService(q, project.NewService(q))

	owner := q.SeedUser("octocat", "octocat@example.com")
	other := q.SeedUser("other", "other@example.com")
	otherProject := q.SeedProject(other.ID, "Beta")

	tests := []struct {
		name      string
		projectID uuid.UUID
	}{
		{"unknown project", uuid.New()},
		{"foreign project", otherProject.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ListFailed(context.Background(), owner.ID, tt.projectID)
			assert.ErrorIs(t, err, syncjob.ErrNotFound)
		})
	}
}

func TestRetry_FailedJob_ResetsToPending(t *testing.T) {
	q := dbtest.New()
	svc := syncjob.NewService(q, project.NewService(q))

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	job := q.SeedSyncJob(p.ID, "issue.update", "failed", time.Now())

	got, err := svc.Retry(context.Background(), owner.ID, job.ID)
	require.NoError(t, err)
	assert.Equal(t, "pending", got.Status)
	assert.Equal(t, int32(0), got.Attempts)

	stored, ok := q.GetSyncJob(job.ID)
	require.True(t, ok)
	assert.Equal(t, "pending", stored.Status)
}

func TestRetry_NotFailed_ReturnsErrNotFailed(t *testing.T) {
	q := dbtest.New()
	svc := syncjob.NewService(q, project.NewService(q))

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	job := q.SeedSyncJob(p.ID, "issue.update", "pending", time.Now())

	_, err := svc.Retry(context.Background(), owner.ID, job.ID)
	assert.ErrorIs(t, err, syncjob.ErrNotFailed)
}

func TestRetry_UnknownOrForeignJob_ReturnsErrNotFound(t *testing.T) {
	q := dbtest.New()
	svc := syncjob.NewService(q, project.NewService(q))

	owner := q.SeedUser("octocat", "octocat@example.com")
	other := q.SeedUser("other", "other@example.com")
	otherProject := q.SeedProject(other.ID, "Beta")
	foreignJob := q.SeedSyncJob(otherProject.ID, "issue.update", "failed", time.Now())

	tests := []struct {
		name  string
		jobID uuid.UUID
	}{
		{"unknown job", uuid.New()},
		{"foreign job", foreignJob.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Retry(context.Background(), owner.ID, tt.jobID)
			assert.ErrorIs(t, err, syncjob.ErrNotFound)
		})
	}
}
