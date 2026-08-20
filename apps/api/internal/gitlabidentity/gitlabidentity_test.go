package gitlabidentity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/gitlabidentity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_Upsert(t *testing.T) {
	q := dbtest.New()
	user := q.SeedUser("alice", "alice@example.com")
	s := gitlabidentity.NewService(q)

	t.Run("registers a new identity", func(t *testing.T) {
		identity, err := s.Upsert(context.Background(), user.ID, gitlabidentity.UpsertInput{
			GitlabBaseURL:  "https://gitlab.example.com",
			GitlabUserID:   42,
			GitlabUsername: "alice-gl",
		})
		require.NoError(t, err)
		assert.Equal(t, "https://gitlab.example.com", identity.GitlabBaseURL)
		assert.Equal(t, int64(42), identity.GitlabUserID)
		assert.Equal(t, "alice-gl", identity.GitlabUsername)
	})

	t.Run("re-registering the same base URL replaces it", func(t *testing.T) {
		first, err := s.Upsert(context.Background(), user.ID, gitlabidentity.UpsertInput{
			GitlabBaseURL:  "https://gitlab2.example.com",
			GitlabUserID:   1,
			GitlabUsername: "old",
		})
		require.NoError(t, err)

		second, err := s.Upsert(context.Background(), user.ID, gitlabidentity.UpsertInput{
			GitlabBaseURL:  "https://gitlab2.example.com",
			GitlabUserID:   2,
			GitlabUsername: "new",
		})
		require.NoError(t, err)
		assert.Equal(t, first.ID, second.ID)
		assert.Equal(t, int64(2), second.GitlabUserID)
		assert.Equal(t, "new", second.GitlabUsername)
	})

	t.Run("rejects an empty base URL", func(t *testing.T) {
		_, err := s.Upsert(context.Background(), user.ID, gitlabidentity.UpsertInput{
			GitlabBaseURL: "  ",
			GitlabUserID:  1,
		})
		assert.True(t, errors.Is(err, gitlabidentity.ErrInvalidBaseURL))
	})
}

// TestService_Upsert_NormalizesBaseURL verifies gitlabidentity normalizes
// gitlabBaseUrl the same way internal/gitlabconn normalizes a project's
// connection base_url, since ?assignee=me joins the two on equality — a
// mismatch (e.g. a trailing slash the connection strips but the identity
// didn't) silently zeroes out the filter instead of erroring.
func TestService_Upsert_NormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
		wantErr error
	}{
		{"trims trailing slash", "https://gitlab.example.com/", "https://gitlab.example.com", nil},
		{"trims trailing slashes and path slash", "https://gitlab.example.com/gitlab/", "https://gitlab.example.com/gitlab", nil},
		{"accepts http", "http://gitlab.internal", "http://gitlab.internal", nil},
		{"rejects missing scheme", "gitlab.example.com", "", gitlabidentity.ErrInvalidBaseURL},
		{"rejects non-http(s) scheme", "ftp://gitlab.example.com", "", gitlabidentity.ErrInvalidBaseURL},
		{"rejects empty", "", "", gitlabidentity.ErrInvalidBaseURL},
		{"rejects whitespace only", "   ", "", gitlabidentity.ErrInvalidBaseURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			user := q.SeedUser("dana", "dana@example.com")
			s := gitlabidentity.NewService(q)

			identity, err := s.Upsert(context.Background(), user.ID, gitlabidentity.UpsertInput{
				GitlabBaseURL: tt.baseURL,
				GitlabUserID:  1,
			})
			if tt.wantErr != nil {
				assert.True(t, errors.Is(err, tt.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, identity.GitlabBaseURL)
		})
	}
}

func TestService_ListForUser(t *testing.T) {
	q := dbtest.New()
	user := q.SeedUser("bob", "bob@example.com")
	other := q.SeedUser("carol", "carol@example.com")
	s := gitlabidentity.NewService(q)

	t.Run("no identities registered", func(t *testing.T) {
		identities, err := s.ListForUser(context.Background(), user.ID)
		require.NoError(t, err)
		assert.Empty(t, identities)
	})

	_, err := s.Upsert(context.Background(), user.ID, gitlabidentity.UpsertInput{
		GitlabBaseURL: "https://b.example.com", GitlabUserID: 2,
	})
	require.NoError(t, err)
	_, err = s.Upsert(context.Background(), user.ID, gitlabidentity.UpsertInput{
		GitlabBaseURL: "https://a.example.com", GitlabUserID: 1,
	})
	require.NoError(t, err)
	_, err = s.Upsert(context.Background(), other.ID, gitlabidentity.UpsertInput{
		GitlabBaseURL: "https://a.example.com", GitlabUserID: 99,
	})
	require.NoError(t, err)

	t.Run("returns only the caller's identities, ordered by base URL", func(t *testing.T) {
		identities, err := s.ListForUser(context.Background(), user.ID)
		require.NoError(t, err)
		require.Len(t, identities, 2)
		assert.Equal(t, "https://a.example.com", identities[0].GitlabBaseURL)
		assert.Equal(t, "https://b.example.com", identities[1].GitlabBaseURL)
	})
}
