package gitlab

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Fingerprint returns a deterministic hash of the issue fields FlowLens
// pushes to GitLab on create/update, stored as
// task_gitlab_links.last_pushed_fingerprint so a later inbound webhook
// carrying the same content can be recognised as FlowLens's own echo rather
// than an external edit (docs/plans/issue-sync.md, "Inbound" — loop
// prevention). Label and assignee order never affect the result.
func Fingerprint(title, description string, labels []string, dueDate *time.Time, assigneeIDs []int64) string {
	sortedLabels := append([]string(nil), labels...)
	sort.Strings(sortedLabels)

	sortedAssignees := append([]int64(nil), assigneeIDs...)
	sort.Slice(sortedAssignees, func(i, j int) bool { return sortedAssignees[i] < sortedAssignees[j] })

	due := ""
	if dueDate != nil {
		due = dueDate.UTC().Format("2006-01-02")
	}

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%v", title, description, strings.Join(sortedLabels, ","), due, sortedAssignees)
	return hex.EncodeToString(h.Sum(nil))
}
