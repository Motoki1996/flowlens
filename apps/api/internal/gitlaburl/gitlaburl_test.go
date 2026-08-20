package gitlaburl_test

import (
	"errors"
	"testing"

	"github.com/flowlens/api/internal/gitlaburl"
	"github.com/stretchr/testify/assert"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{"trims trailing slash", "https://gitlab.example.com/", "https://gitlab.example.com", nil},
		{"trims trailing slashes and path slash", "https://gitlab.example.com/gitlab/", "https://gitlab.example.com/gitlab", nil},
		{"accepts http", "http://gitlab.internal", "http://gitlab.internal", nil},
		{"strips query and fragment", "https://gitlab.example.com?x=1#y", "https://gitlab.example.com", nil},
		{"trims surrounding whitespace", "  https://gitlab.example.com  ", "https://gitlab.example.com", nil},
		{"rejects missing scheme", "gitlab.example.com", "", gitlaburl.ErrInvalid},
		{"rejects non-http(s) scheme", "ftp://gitlab.example.com", "", gitlaburl.ErrInvalid},
		{"rejects empty", "", "", gitlaburl.ErrInvalid},
		{"rejects whitespace only", "   ", "", gitlaburl.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gitlaburl.Normalize(tt.raw)
			if tt.wantErr != nil {
				assert.True(t, errors.Is(err, tt.wantErr))
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
