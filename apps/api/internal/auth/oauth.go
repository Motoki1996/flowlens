package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// NewGitHubOAuthConfig builds the oauth2 config for the GitHub login flow.
// The redirect URL points back at this API's callback endpoint.
func NewGitHubOAuthConfig(clientID, clientSecret, apiBaseURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     github.Endpoint,
		RedirectURL:  apiBaseURL + "/auth/github/callback",
		// read:user for profile, read:org so later phases can list orgs.
		Scopes: []string{"read:user", "read:org", "repo"},
	}
}

// NewState returns a random anti-CSRF state value for the OAuth flow.
func NewState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read random state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
