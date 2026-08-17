package gitlab_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flowlens/api/internal/gitlab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSelfSignedServer stands in for a self-hosted GitLab CE behind a
// certificate the API host does not trust, and returns its base URL plus the
// path to a PEM file holding its (otherwise unknown) CA.
func newSelfSignedServer(t *testing.T) (baseURL, caFile string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"username":"octocat"}`))
	}))
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "ca.pem")
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return srv.URL, path
}

// unrelatedCAFile writes a PEM certificate that signs nothing in this test,
// standing in for a CA bundle pointed at the wrong instance. httptest's TLS
// servers all share one built-in certificate, so a second server cannot play
// this part.
func unrelatedCAFile(t *testing.T) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unrelated-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "unrelated-ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}

// The on-prem case this whole seam exists for: a GitLab CE instance whose
// certificate the system roots cannot verify must be reachable by policy,
// and naming a CA must keep verification on rather than turning it off.
func TestNewClientFactory_TLSPolicy(t *testing.T) {
	baseURL, caFile := newSelfSignedServer(t)
	otherCAFile := unrelatedCAFile(t)

	tests := []struct {
		name       string
		policy     gitlab.TLSPolicy
		wantErr    bool
		wantVerify bool
	}{
		{
			name:       "default policy rejects an untrusted certificate",
			policy:     gitlab.TLSPolicy{},
			wantErr:    true,
			wantVerify: true,
		},
		{
			name:       "skip-verify reaches the instance",
			policy:     gitlab.TLSPolicy{InsecureSkipVerify: true},
			wantVerify: false,
		},
		{
			name:       "a named CA reaches the instance with verification on",
			policy:     gitlab.TLSPolicy{CACertFile: caFile},
			wantVerify: true,
		},
		{
			name:       "a named CA takes precedence over skip-verify",
			policy:     gitlab.TLSPolicy{CACertFile: otherCAFile, InsecureSkipVerify: true},
			wantErr:    true,
			wantVerify: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantVerify, tt.policy.Verifying())

			factory, err := gitlab.NewClientFactory(tt.policy)
			require.NoError(t, err)

			user, err := factory(baseURL).GetAuthenticatedUser(context.Background(), "glpat-token")
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, gitlab.IsCertificateError(err), "want a certificate error, got %v", err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "octocat", user.Username)
		})
	}
}

func TestNewTransport_RejectsAnUnusableCACertFile(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.pem")
	require.NoError(t, os.WriteFile(empty, []byte("not a certificate"), 0o600))

	_, err := gitlab.NewTransport(gitlab.TLSPolicy{CACertFile: empty})
	assert.Error(t, err)

	_, err = gitlab.NewTransport(gitlab.TLSPolicy{CACertFile: filepath.Join(t.TempDir(), "missing.pem")})
	assert.Error(t, err)
}

// roundTripperFunc counts how many times the client actually dialled.
type roundTripperFunc struct {
	calls int
	err   error
}

func (f *roundTripperFunc) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return nil, f.err
}

// A rejected certificate never becomes reachable by trying again, so it must
// not consume the retry budget the transient failures need.
func TestHTTPClient_DoesNotRetryCertificateErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCalls int
	}{
		{"certificate error fails fast", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, 1},
		{"transient network error is retried", assert.AnError, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &roundTripperFunc{err: tt.err}
			client := gitlab.NewHTTPClient("https://gitlab.example.com", gitlab.WithTransport(rt))

			_, err := client.GetAuthenticatedUser(context.Background(), "glpat-token")
			require.Error(t, err)
			assert.Equal(t, tt.wantCalls, rt.calls)
		})
	}
}
