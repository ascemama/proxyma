package tlsserver

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPrepareSelfSignedGeneratesReusesAndRenews(t *testing.T) {
	root := t.TempDir()
	certificateFile := filepath.Join(root, "tls", "server.crt")
	privateKeyFile := filepath.Join(root, "tls", "server.key")
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	options := Options{
		Mode: "self_signed", CertificateFile: certificateFile, PrivateKeyFile: privateKeyFile,
		ServerName: "proxyma.example", ListenAddress: "192.0.2.10:8080",
		Now:      func() time.Time { return now },
		Hostname: func() (string, error) { return "proxyma-host", nil },
	}
	first, err := Prepare(options)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Generated {
		t.Fatal("expected a generated certificate")
	}
	for _, expected := range []string{"localhost", "proxyma-host", "proxyma.example"} {
		if !containsString(first.DNSNames, expected) {
			t.Fatalf("DNS SANs %v do not contain %q", first.DNSNames, expected)
		}
	}
	for _, expected := range []string{"127.0.0.1", "::1", "192.0.2.10"} {
		if !containsString(first.IPAddresses, expected) {
			t.Fatalf("IP SANs %v do not contain %q", first.IPAddresses, expected)
		}
	}
	keyInfo, err := os.Stat(privateKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && keyInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("private key permissions = %o", keyInfo.Mode().Perm())
	}

	second, err := Prepare(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Generated || second.FingerprintSHA256 != first.FingerprintSHA256 {
		t.Fatalf("expected certificate reuse: first=%#v second=%#v", first, second)
	}

	options.ServerName = ""
	changedSANs, err := Prepare(options)
	if err != nil {
		t.Fatal(err)
	}
	if !changedSANs.Generated || changedSANs.FingerprintSHA256 == first.FingerprintSHA256 ||
		containsString(changedSANs.DNSNames, "proxyma.example") {
		t.Fatalf("expected regeneration after removing a required SAN: %#v", changedSANs)
	}

	options.Now = func() time.Time { return now.Add(375 * 24 * time.Hour) }
	renewed, err := Prepare(options)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.Generated || renewed.FingerprintSHA256 == first.FingerprintSHA256 {
		t.Fatalf("expected certificate renewal: first=%#v renewed=%#v", first, renewed)
	}
}

func TestValidatePairRejectsMismatchedAndExpiredPairs(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	first := Options{
		Mode: "self_signed", CertificateFile: filepath.Join(firstRoot, "server.crt"),
		PrivateKeyFile: filepath.Join(firstRoot, "server.key"), Now: func() time.Time { return now },
	}
	second := Options{
		Mode: "self_signed", CertificateFile: filepath.Join(secondRoot, "server.crt"),
		PrivateKeyFile: filepath.Join(secondRoot, "server.key"), Now: func() time.Time { return now },
	}
	if _, err := Prepare(first); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(second); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidatePair(first.CertificateFile, second.PrivateKeyFile, now); err == nil {
		t.Fatal("expected mismatched pair rejection")
	}
	if _, err := ValidatePair(first.CertificateFile, first.PrivateKeyFile, now.Add(-time.Hour)); err == nil {
		t.Fatal("expected not-yet-valid certificate rejection")
	}
	if _, err := ValidatePair(first.CertificateFile, first.PrivateKeyFile, now.Add(398*24*time.Hour)); err == nil {
		t.Fatal("expected expired certificate rejection")
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
