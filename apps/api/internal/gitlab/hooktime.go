package gitlab

import "time"

// hookTimeLayouts are the timestamp layouts GitLab has been observed to send
// in webhook payloads (unlike the REST API, which always returns RFC3339).
// Hook payloads go through Gitlab::HookData::*Builder, which serialises a
// Ruby Time via #to_s rather than #iso8601, so the layout varies by GitLab
// version and event type. Tried in order, first match wins.
var hookTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05 -0700 MST",
}

// ParseHookTime parses a timestamp from a GitLab webhook payload field such
// as object_attributes.updated_at. ok is false when s is empty or matches
// none of the known layouts — callers must surface that rather than
// silently treating it as "no timestamp", since a zero time is
// indistinguishable from "field absent" and would otherwise disable any
// staleness check built on it.
func ParseHookTime(s string) (t time.Time, ok bool) {
	for _, layout := range hookTimeLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
