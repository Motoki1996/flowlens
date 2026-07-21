// Package github wraps the parts of the GitHub REST API that FlowLens
// needs. It is defined as an interface so a fake implementation can be
// used in tests and so future concerns (pagination, rate limiting,
// retries) live behind a single seam.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// User is the subset of a GitHub user that FlowLens stores.
type User struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// Client is the seam over the GitHub API. Later phases add repository and
// pull-request methods here; the fake in fake.go implements the same set.
type Client interface {
	// GetAuthenticatedUser returns the user that owns the access token.
	GetAuthenticatedUser(ctx context.Context, accessToken string) (*User, error)
}

// APIError represents a non-2xx response from GitHub.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: unexpected status %d: %s", e.StatusCode, e.Body)
}

// HTTPClient is the real GitHub API client.
type HTTPClient struct {
	baseURL string
	http    *http.Client
}

// NewHTTPClient builds a client with a sensible timeout.
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		baseURL: "https://api.github.com",
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *HTTPClient) GetAuthenticatedUser(ctx context.Context, accessToken string) (*User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/user", nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, readAPIError(resp)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("github: decode user: %w", err)
	}
	if user.ID == 0 || user.Login == "" {
		return nil, fmt.Errorf("github: user response missing required fields")
	}
	return &user, nil
}

func readAPIError(resp *http.Response) error {
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	return &APIError{StatusCode: resp.StatusCode, Body: string(buf[:n])}
}
