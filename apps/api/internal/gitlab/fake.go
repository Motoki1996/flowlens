package gitlab

import "context"

// FakeClient is an in-memory Client for tests. It records calls and
// returns canned data, letting use cases be tested without network access.
type FakeClient struct {
	// AuthenticatedUser is returned by GetAuthenticatedUser when set.
	AuthenticatedUser *User
	// Err, when set, is returned by every method.
	Err error

	// Calls counts GetAuthenticatedUser invocations.
	Calls int
}

// GetAuthenticatedUser implements Client.
func (f *FakeClient) GetAuthenticatedUser(ctx context.Context, personalAccessToken string) (*User, error) {
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	return f.AuthenticatedUser, nil
}
