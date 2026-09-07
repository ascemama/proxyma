package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"proxyma/internal/tlsserver"
)

func TestCertificatesValidateCommand(t *testing.T) {
	root := t.TempDir()
	certificateFile := filepath.Join(root, "server.crt")
	privateKeyFile := filepath.Join(root, "server.key")
	if _, err := tlsserver.Prepare(tlsserver.Options{
		Mode: "self_signed", CertificateFile: certificateFile, PrivateKeyFile: privateKeyFile,
		Now: func() time.Time { return time.Now().UTC() },
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"certificates", "validate", "--cert-file", certificateFile, "--key-file", privateKeyFile}, &stdout, &stderr, bytes.NewReader(nil), emptyLookup)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var result tlsserver.Info
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.FingerprintSHA256 == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCertificatesInstallReplacesPairAndKeepsExistingPairOnValidationFailure(t *testing.T) {
	root := t.TempDir()
	sourceCertificate := filepath.Join(root, "source.crt")
	sourceKey := filepath.Join(root, "source.key")
	destinationCertificate := filepath.Join(root, "managed", "provided.crt")
	destinationKey := filepath.Join(root, "managed", "provided.key")
	if _, err := tlsserver.Prepare(tlsserver.Options{
		Mode: "self_signed", CertificateFile: sourceCertificate, PrivateKeyFile: sourceKey,
		Now: func() time.Time { return time.Now().UTC() },
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"certificates", "install",
		"--cert-file", sourceCertificate, "--key-file", sourceKey,
		"--dest-cert-file", destinationCertificate, "--dest-key-file", destinationKey,
	}, &stdout, &stderr, bytes.NewReader(nil), emptyLookup)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	originalCertificate, err := os.ReadFile(destinationCertificate)
	if err != nil {
		t.Fatal(err)
	}
	originalKey, err := os.ReadFile(destinationKey)
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo, err := os.Stat(destinationKey); err != nil || (runtime.GOOS != "windows" && keyInfo.Mode().Perm() != 0o600) {
		t.Fatalf("private key permissions: info=%v err=%v", keyInfo, err)
	}

	badKey := filepath.Join(root, "bad.key")
	if err := os.WriteFile(badKey, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"certificates", "install",
		"--cert-file", sourceCertificate, "--key-file", badKey,
		"--dest-cert-file", destinationCertificate, "--dest-key-file", destinationKey,
	}, &stdout, &stderr, bytes.NewReader(nil), emptyLookup)
	if code == 0 {
		t.Fatal("invalid replacement unexpectedly succeeded")
	}
	if got, _ := os.ReadFile(destinationCertificate); !bytes.Equal(got, originalCertificate) {
		t.Fatal("certificate changed after failed replacement")
	}
	if got, _ := os.ReadFile(destinationKey); !bytes.Equal(got, originalKey) {
		t.Fatal("private key changed after failed replacement")
	}
}

func TestConfigUpdateTransportPreservesUnrelatedSettingsAndMigratesCookieField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxyma.config.json")
	if err := os.WriteFile(path, []byte(`{
  "server": {"listen_address": "0.0.0.0:8080"},
  "security": {"user_session_ttl": "12h", "allow_insecure_user_cookies": true},
  "unrelated": {"preserve": true}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"config", "update-transport", "--file", path,
		"--scheme", "https", "--tls-mode", "provided",
		"--tls-cert-file", "/etc/proxyma/tls/provided.crt",
		"--tls-key-file", "/etc/proxyma/tls/provided.key",
		"--tls-server-name=",
	}, &stdout, &stderr, bytes.NewReader(nil), emptyLookup)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(content, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["unrelated"]; !ok {
		t.Fatal("unrelated configuration was removed")
	}
	var security map[string]json.RawMessage
	if err := json.Unmarshal(root["security"], &security); err != nil {
		t.Fatal(err)
	}
	if _, ok := security["allow_insecure_user_cookies"]; ok {
		t.Fatal("obsolete cookie field was not removed")
	}
	var server struct {
		Scheme string `json:"scheme"`
		TLS    struct {
			Mode            string `json:"mode"`
			CertificateFile string `json:"certificate_file"`
			PrivateKeyFile  string `json:"private_key_file"`
		} `json:"tls"`
	}
	if err := json.Unmarshal(root["server"], &server); err != nil {
		t.Fatal(err)
	}
	if server.Scheme != "https" || server.TLS.Mode != "provided" ||
		server.TLS.CertificateFile != "/etc/proxyma/tls/provided.crt" ||
		server.TLS.PrivateKeyFile != "/etc/proxyma/tls/provided.key" {
		t.Fatalf("unexpected server configuration: %#v", server)
	}
}

func TestConfigUpdateTransportChangesOnlyExplicitComponents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxyma.config.json")
	if err := os.WriteFile(path, []byte(`{
  "server": {
    "listen_address": "0.0.0.0:8080",
    "scheme": "http",
    "tls": {
      "mode": "provided",
      "certificate_file": "provided.crt",
      "private_key_file": "provided.key",
      "server_name": "keep.example"
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "update-transport", "--file", path, "--scheme", "https"}, &stdout, &stderr, bytes.NewReader(nil), emptyLookup)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Server struct {
			Scheme string `json:"scheme"`
			TLS    struct {
				Mode            string `json:"mode"`
				CertificateFile string `json:"certificate_file"`
				PrivateKeyFile  string `json:"private_key_file"`
				ServerName      string `json:"server_name"`
			} `json:"tls"`
		} `json:"server"`
	}
	if err := json.Unmarshal(content, &root); err != nil {
		t.Fatal(err)
	}
	if root.Server.Scheme != "https" || root.Server.TLS.Mode != "provided" ||
		root.Server.TLS.CertificateFile != "provided.crt" ||
		root.Server.TLS.PrivateKeyFile != "provided.key" ||
		root.Server.TLS.ServerName != "keep.example" {
		t.Fatalf("unexpected transport configuration: %#v", root.Server)
	}
}

func emptyLookup(string) (string, bool) { return "", false }
