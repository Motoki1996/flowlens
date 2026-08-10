package notification

import "time"

// WithNowForTest overrides a Worker's clock, exported for notification_test
// (an external test package) the same way internal/sync's export_test.go
// exposes its metrics vars for assertions tests would otherwise have no
// access to.
func WithNowForTest(now func() time.Time) WorkerOption {
	return withNow(now)
}
