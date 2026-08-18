package metricsperiod_test

import (
	"testing"
	"time"

	"github.com/flowlens/api/internal/metricsperiod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInterval(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want metricsperiod.Interval
		ok   bool
	}{
		{"week", "week", metricsperiod.Week, true},
		{"month", "month", metricsperiod.Month, true},
		{"year", "year", metricsperiod.Year, true},
		{"empty", "", "", false},
		{"unknown", "decade", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := metricsperiod.ParseInterval(tt.in)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBucketStart(t *testing.T) {
	tests := []struct {
		name     string
		in       time.Time
		interval metricsperiod.Interval
		want     time.Time
	}{
		{
			"week starts on Monday",
			time.Date(2026, 3, 12, 15, 30, 0, 0, time.UTC), // Thursday
			metricsperiod.Week,
			time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), // Monday
		},
		{
			"Sunday belongs to the previous Monday's week",
			time.Date(2026, 3, 15, 23, 0, 0, 0, time.UTC), // Sunday
			metricsperiod.Week,
			time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
		},
		{
			"Monday is its own week start",
			time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
			metricsperiod.Week,
			time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
		},
		{
			"month truncates to the 1st",
			time.Date(2026, 3, 12, 15, 30, 0, 0, time.UTC),
			metricsperiod.Month,
			time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			"year truncates to Jan 1",
			time.Date(2026, 3, 12, 15, 30, 0, 0, time.UTC),
			metricsperiod.Year,
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			"non-UTC input is normalized to UTC first",
			time.Date(2026, 3, 1, 2, 0, 0, 0, time.FixedZone("JST", 9*3600)), // 2026-02-28 17:00 UTC
			metricsperiod.Month,
			time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, metricsperiod.BucketStart(tt.in, tt.interval))
		})
	}
}

func TestBucketEnd(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		interval metricsperiod.Interval
		want     time.Time
	}{
		{"week", time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), metricsperiod.Week, time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)},
		{"month", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), metricsperiod.Month, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		{"year", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), metricsperiod.Year, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, metricsperiod.BucketEnd(tt.start, tt.interval))
		})
	}
}

func TestTimeline_ExplicitBoundsFillGaps(t *testing.T) {
	from := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	starts, truncated := metricsperiod.Timeline(metricsperiod.Month, &from, &to, nil)

	require.Len(t, starts, 3)
	assert.False(t, truncated)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), starts[0])
	assert.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), starts[1])
	assert.Equal(t, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), starts[2])
}

func TestTimeline_YearBoundarySplitsBuckets(t *testing.T) {
	from := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	starts, truncated := metricsperiod.Timeline(metricsperiod.Year, &from, &to, nil)

	require.Len(t, starts, 2)
	assert.False(t, truncated)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), starts[0])
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), starts[1])
}

func TestTimeline_NilBoundsFallBackToObserved(t *testing.T) {
	observed := []time.Time{
		time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
	}

	starts, truncated := metricsperiod.Timeline(metricsperiod.Month, nil, nil, observed)

	require.Len(t, starts, 3)
	assert.False(t, truncated)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), starts[0])
	assert.Equal(t, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), starts[2])
}

func TestTimeline_NilBoundNoObserved_ReturnsEmpty(t *testing.T) {
	starts, truncated := metricsperiod.Timeline(metricsperiod.Month, nil, nil, nil)

	assert.Nil(t, starts)
	assert.False(t, truncated)
}

func TestTimeline_OverCapTruncatesToNewest(t *testing.T) {
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) // 85 months apart, well over the 52 cap

	starts, truncated := metricsperiod.Timeline(metricsperiod.Month, &from, &to, nil)

	require.Len(t, starts, metricsperiod.MaxPeriods)
	assert.True(t, truncated)
	assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), starts[len(starts)-1])
}
