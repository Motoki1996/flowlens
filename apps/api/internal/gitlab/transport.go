package gitlab

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
)

// TLSPolicy describes how a Client verifies the TLS certificate of the
// instance it talks to.
//
// It exists because FlowLens targets self-hosted GitLab CE, where a private
// CA or a self-signed certificate is the norm: Go's default (system roots
// only) fails the handshake with an x509 error the operator has no way to
// configure away, which surfaces as an unexplained "unreachable" connection.
//
// It is a value passed in at wiring time rather than a package-level global
// precisely so that different providers can run under different rules — a
// future GitHub client reaches a public CA and must stay verified even while
// the on-prem GitLab client skips verification.
type TLSPolicy struct {
	// CACertFile is the path to a PEM bundle appended to the system roots.
	// When set it takes precedence over InsecureSkipVerify: naming a CA is
	// an explicit request to verify against it, so a configured CA can never
	// silently degrade into no verification at all.
	CACertFile string

	// InsecureSkipVerify disables certificate verification entirely. It is
	// the on-prem escape hatch (GITLAB_TLS_INSECURE_SKIP_VERIFY) and leaves
	// the connection open to interception — only for a network the operator
	// already trusts.
	InsecureSkipVerify bool
}

// Verifying reports whether p leaves certificate verification on. Wiring
// code uses it to warn at startup when it does not.
func (p TLSPolicy) Verifying() bool {
	return p.CACertFile != "" || !p.InsecureSkipVerify
}

// NewTransport builds the transport implementing p. It clones
// http.DefaultTransport so proxy support, connection pooling and the dial
// timeouts stay at Go's defaults, and only replaces the TLS configuration.
func NewTransport(p TLSPolicy) (*http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("gitlab: unexpected default transport %T", http.DefaultTransport)
	}
	transport := base.Clone()

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	switch {
	case p.CACertFile != "":
		pool, err := caPoolWith(p.CACertFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.RootCAs = pool
	case p.InsecureSkipVerify:
		// #nosec G402 -- opt-in, documented, and the reason this knob exists:
		// an on-prem GitLab CE whose certificate the API host cannot verify.
		tlsConfig.InsecureSkipVerify = true
	}
	transport.TLSClientConfig = tlsConfig

	return transport, nil
}

// NewClientFactory returns a factory building one Client per base URL, all
// sharing a single transport (and so a single connection pool) built from
// policy. The returned func is deliberately unnamed so it satisfies each
// consumer's own ClientFactory type (gitlabconn, issuesync, mrsync, ...).
func NewClientFactory(policy TLSPolicy) (func(baseURL string) Client, error) {
	transport, err := NewTransport(policy)
	if err != nil {
		return nil, err
	}
	return func(baseURL string) Client {
		return NewHTTPClient(baseURL, WithTransport(transport))
	}, nil
}

// caPoolWith returns the system root pool plus the PEM certificates in path.
// A system pool that cannot be loaded is not fatal — the named CA alone is
// then the whole trust set, which is what an air-gapped host wants anyway.
func caPoolWith(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path) // #nosec G304 -- operator-supplied CA bundle path
	if err != nil {
		return nil, fmt.Errorf("gitlab: read CA cert file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("gitlab: CA cert file %s contains no PEM certificate", path)
	}
	return pool, nil
}

// IsCertificateError reports whether err was produced by TLS certificate
// verification rather than by a connection failure. Callers use it to say so
// specifically (an unknown CA is fixable by configuration; a refused
// connection is not) and to skip retrying, since a rejected certificate is
// permanent.
func IsCertificateError(err error) bool {
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verification) ||
		errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostname) ||
		errors.As(err, &invalid)
}
