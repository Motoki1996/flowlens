// Package metricsperiod computes the calendar-bucket boundaries flowmetrics
// and deliverymetrics both need to break their aggregates into a week/month/
// year time series (issue #188): a bucket's UTC start/end, and the
// gap-filled, capped list of bucket starts a request's periods should cover.
// Everything here is pure boundary arithmetic — the aggregation itself (what
// goes in each bucket) stays in each package, same as their duplicated
// median/p90 helpers.
package metricsperiod

import "time"

// Interval is a bucket size a metrics request's periods can be grouped by.
type Interval string

const (
	Week  Interval = "week"
	Month Interval = "month"
	Year  Interval = "year"
)

// MaxPeriods is the most buckets a response's periods will ever hold.
// Unbounded from/to with interval=week could otherwise produce hundreds of
// rows; the newest MaxPeriods are kept and Truncated is reported instead.
const MaxPeriods = 52

// ParseInterval parses a raw ?interval= query value. ok is false for
// anything other than "week", "month", or "year", including "".
func ParseInterval(v string) (Interval, bool) {
	switch Interval(v) {
	case Week, Month, Year:
		return Interval(v), true
	default:
		return "", false
	}
}

// BucketStart returns the UTC start of the interval-sized bucket containing
// t: the ISO week's Monday 00:00 UTC, the month's 1st 00:00 UTC, or the
// year's Jan 1 00:00 UTC.
func BucketStart(t time.Time, interval Interval) time.Time {
	t = t.UTC()
	switch interval {
	case Week:
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		// time.Weekday is 0=Sunday..6=Saturday; ISO weeks start Monday, so
		// Sunday is 6 days into the week rather than the start of one.
		daysSinceMonday := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -daysSinceMonday)
	case Month:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default: // Year
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	}
}

// BucketEnd returns the exclusive end of the bucket starting at start (which
// must already be a BucketStart result for interval).
func BucketEnd(start time.Time, interval Interval) time.Time {
	switch interval {
	case Week:
		return start.AddDate(0, 0, 7)
	case Month:
		return start.AddDate(0, 1, 0)
	default: // Year
		return start.AddDate(1, 0, 0)
	}
}

// Timeline returns the ascending, gap-filled bucket starts a response's
// periods should cover, and whether that list was truncated to the newest
// MaxPeriods.
//
// The covered range is [from, to], each bucket-aligned; a nil bound falls
// back to the earliest/latest of observed (raw, not yet bucket-aligned
// timestamps of whatever the caller's buckets are keyed by). If a bound is
// nil and observed is empty, there is nothing to cover and Timeline returns
// (nil, false).
func Timeline(interval Interval, from, to *time.Time, observed []time.Time) ([]time.Time, bool) {
	start, ok := bound(from, interval, observed, false)
	if !ok {
		return nil, false
	}
	end, ok := bound(to, interval, observed, true)
	if !ok {
		return nil, false
	}
	if end.Before(start) {
		return nil, false
	}

	var starts []time.Time
	for cur := start; !cur.After(end); cur = BucketEnd(cur, interval) {
		starts = append(starts, cur)
	}

	truncated := false
	if len(starts) > MaxPeriods {
		starts = starts[len(starts)-MaxPeriods:]
		truncated = true
	}
	return starts, truncated
}

// bound resolves one end of the covered range: explicit if non-nil,
// otherwise the min (wantMax false) or max (wantMax true) of observed.
func bound(explicit *time.Time, interval Interval, observed []time.Time, wantMax bool) (time.Time, bool) {
	if explicit != nil {
		return BucketStart(*explicit, interval), true
	}
	if len(observed) == 0 {
		return time.Time{}, false
	}
	best := observed[0]
	for _, o := range observed[1:] {
		if (wantMax && o.After(best)) || (!wantMax && o.Before(best)) {
			best = o
		}
	}
	return BucketStart(best, interval), true
}
