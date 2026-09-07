package sshclient

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
)

func TestTrustStoreCreatesAndPreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "known_hosts")
	store := &TrustStore{Path: path}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing\n" {
		t.Fatalf("trust store was modified: %q", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
}

func TestRunnerApprovesUnknownHostOnlyAfterSuccessfulCredentials(t *testing.T) {
	privateKey, authorized := newPrivateKey(t)
	server := newTestSSHServerWithPublicKey(t, authorized.PublicKey())
	defer server.Close()
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := &TrustStore{Path: path}
	runner := Runner{Timeout: time.Second, OutputLimit: 1024, DefaultKnownHostsPath: path, TrustStore: store}
	request := Request{
		Host: server.host, Port: server.port, Username: server.username,
		AuthType: AuthTypePrivateKey, PrivateKeyPath: writePrivateKey(t, privateKey),
		RemoteCommand: "hostname", KnownHostsPath: path,
	}

	unknown := runner.Run(context.Background(), request)
	if unknown.ErrorCode != ErrorCodeHostKeyUnknown || unknown.HostKey == nil {
		t.Fatalf("unknown result = %#v", unknown)
	}
	if data, err := os.ReadFile(path); err != nil || len(data) != 0 {
		t.Fatalf("unknown key was stored: data=%q err=%v", data, err)
	}

	bad := request
	bad.Username = "wrong-user"
	bad.ApprovedHostKeyFingerprint = unknown.HostKey.Fingerprint
	failed := runner.Run(context.Background(), bad)
	if failed.Error == "" {
		t.Fatal("invalid credentials unexpectedly succeeded")
	}
	if data, err := os.ReadFile(path); err != nil || len(data) != 0 {
		t.Fatalf("failed credentials stored key: data=%q err=%v", data, err)
	}

	request.ApprovedHostKeyFingerprint = unknown.HostKey.Fingerprint
	approved := runner.Run(context.Background(), request)
	if approved.Error != "" {
		t.Fatalf("approved result error = %q", approved.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(server.hostSigner.PublicKey().Type())) {
		t.Fatalf("stored data = %q", data)
	}

	request.ApprovedHostKeyFingerprint = ""
	trusted := runner.Run(context.Background(), request)
	if trusted.Error != "" {
		t.Fatalf("trusted result error = %q", trusted.Error)
	}
}

func TestRunnerRejectsChangedHostKey(t *testing.T) {
	server := newTestSSHServer(t)
	defer server.Close()
	path := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(serverAddress(server))}, server.otherSigner.PublicKey())
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Timeout: time.Second, OutputLimit: 1024, DefaultKnownHostsPath: path, TrustStore: &TrustStore{Path: path}}
	result := runner.Run(context.Background(), Request{Host: server.host, Port: server.port, Username: server.username, Password: server.password, RemoteCommand: "hostname", KnownHostsPath: path})
	if result.ErrorCode != ErrorCodeHostKeyChanged || result.HostKey == nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestTrustStoreConcurrentAddDoesNotDuplicate(t *testing.T) {
	server := newTestSSHServer(t)
	defer server.Close()
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := &TrustStore{Path: path}
	pending := &pendingHostKey{hostname: serverAddress(server), remote: server.listener.Addr(), key: server.hostSigner.PublicKey()}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Trust(pending); err != nil {
				t.Errorf("Trust: %v", err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(string(data)), "\n") != 0 {
		t.Fatalf("duplicate entries: %q", data)
	}
}

func serverAddress(server *testSSHServer) string {
	return knownhosts.Normalize("[" + server.host + "]:" + strconv.Itoa(server.port))
}
