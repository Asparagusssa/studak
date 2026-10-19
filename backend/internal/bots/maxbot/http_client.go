package maxbot

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"net/http"
	"time"
)

// russianTrustedRootCA is the Russian Trusted Root CA published by the
// Ministry of Digital Development. MAX's public API currently chains to it,
// while Alpine's default CA bundle does not include it.
//
//go:embed russian_trusted_root_ca.pem
var russianTrustedRootCA []byte

// NewHTTPClient returns a client intended only for fixed MAX API endpoints.
// It keeps the operating system roots and adds the one root required by MAX.
func NewHTTPClient(timeout time.Duration) *http.Client {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(russianTrustedRootCA) {
		// The embedded certificate is validated by tests. Falling back here is
		// fail-closed: MAX requests will fail TLS verification.
		roots = x509.NewCertPool()
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
