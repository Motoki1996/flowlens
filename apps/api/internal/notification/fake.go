package notification

import "context"

// SentDigest records one FakeSender.Send call.
type SentDigest struct {
	WebhookURL string
	Digest     Digest
}

// FakeSender is an in-memory Sender for tests: it records every digest it
// would have sent instead of making a network call. See docs/testing.md —
// prefer this stateful fake over a call-expectation mock.
type FakeSender struct {
	// Err, when set, is returned by every Send call instead of recording it.
	Err error
	// Sent accumulates every successful Send call, in order.
	Sent []SentDigest
}

// Send records digest and webhookURL, or returns Err if set.
func (f *FakeSender) Send(_ context.Context, webhookURL string, digest Digest) error {
	if f.Err != nil {
		return f.Err
	}
	f.Sent = append(f.Sent, SentDigest{WebhookURL: webhookURL, Digest: digest})
	return nil
}
