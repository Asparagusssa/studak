package maxbot

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"testing"
)

func TestEmbeddedRussianTrustedRootCA(t *testing.T) {
	block, rest := pem.Decode(russianTrustedRootCA)
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		t.Fatal("embedded MAX root must contain exactly one PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsCA || cert.Subject.CommonName != "Russian Trusted Root CA" {
		t.Fatalf("unexpected certificate: CA=%v CN=%q", cert.IsCA, cert.Subject.CommonName)
	}
	want := "d26d2d0231b7c39f92cc738512ba54103519e4405d68b5bd703e9788ca8ecf31"
	sum := sha256.Sum256(cert.Raw)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("certificate fingerprint %s, want %s", got, want)
	}
}
