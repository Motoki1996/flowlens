package gitlab_test

import (
	"testing"
	"time"

	"github.com/flowlens/api/internal/gitlab"
	"github.com/stretchr/testify/assert"
)

func TestParseHookTime(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		{
			name: "REST API RFC3339",
			in:   "2013-12-03T17:15:43Z",
			want: time.Date(2013, 12, 3, 17, 15, 43, 0, time.UTC),
			ok:   true,
		},
		{
			name: "hook zone abbreviation",
			in:   "2017-09-15 16:50:55 UTC",
			want: time.Date(2017, 9, 15, 16, 50, 55, 0, time.UTC),
			ok:   true,
		},
		{
			name: "hook numeric offset",
			in:   "2015-05-17 18:08:09 +0000",
			want: time.Date(2015, 5, 17, 18, 8, 9, 0, time.UTC),
			ok:   true,
		},
		{
			name: "hook numeric offset and zone abbreviation",
			in:   "2015-05-17 18:08:09 +0000 UTC",
			want: time.Date(2015, 5, 17, 18, 8, 9, 0, time.UTC),
			ok:   true,
		},
		{
			name: "empty",
			in:   "",
			ok:   false,
		},
		{
			name: "unrecognised layout",
			in:   "not a time",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := gitlab.ParseHookTime(tt.in)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.True(t, tt.want.Equal(got), "got %v, want %v", got, tt.want)
			}
		})
	}
}
