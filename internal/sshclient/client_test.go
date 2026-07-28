package sshclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestRunnerExecutesRemoteCommand(t *testing.T) {
	server := newTestSSHServer(t)
	defer server.Close()

	result := Runner{Timeout: time.Second, OutputLimit: 1024}.Run(context.Background(), Request{
		Host:           server.host,
		Port:           server.port,
		Username:       server.username,
		Password:       server.password,
		RemoteCommand:  "hostname",
		KnownHostsPath: server.knownHostsPath,
		Timeout:        time.Second,
		OutputLimit:    1024,
	})
	if result.Error != "" {
		t.Fatalf("result error = %q", result.Error)
	}
	if result.ExitStatus == nil || *result.ExitStatus != 0 {
		t.Fatalf("exit status = %#v", result.ExitStatus)
	}
	if strings.TrimSpace(result.Stdout) != "test-host" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunnerExecutesRemoteCommandWithPrivateKey(t *testing.T) {
	privateKey, signer := newPrivateKey(t)
	server := newTestSSHServerWithPublicKey(t, signer.PublicKey())
	defer server.Close()

	result := Runner{Timeout: time.Second, OutputLimit: 1024}.Run(context.Background(), Request{
		Host:           server.host,
		Port:           server.port,
		Username:       server.username,
		AuthType:       AuthTypePrivateKey,
		PrivateKeyPath: writePrivateKey(t, privateKey),
		RemoteCommand:  "hostname",
		KnownHostsPath: server.knownHostsPath,
		Timeout:        time.Second,
		OutputLimit:    1024,
	})
	if result.Error != "" {
		t.Fatalf("result error = %q", result.Error)
	}
	if result.ExitStatus == nil || *result.ExitStatus != 0 {
		t.Fatalf("exit status = %#v", result.ExitStatus)
	}
	if strings.TrimSpace(result.Stdout) != "test-host" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunnerRejectsUnknownOrMismatchedHostKeys(t *testing.T) {
	server := newTestSSHServer(t)
	defer server.Close()

	for _, knownHostsPath := range []string{writeKnownHosts(t, server.host, server.port, server.otherSigner), filepath.Join(t.TempDir(), "missing-known-hosts")} {
		result := Runner{Timeout: time.Second, OutputLimit: 1024}.Run(context.Background(), Request{
			Host:           server.host,
			Port:           server.port,
			Username:       server.username,
			Password:       server.password,
			RemoteCommand:  "hostname",
			KnownHostsPath: knownHostsPath,
			Timeout:        time.Second,
			OutputLimit:    1024,
		})
		if result.Error == "" {
			t.Fatalf("expected host-key error for %s", knownHostsPath)
		}
	}
}

func TestRunnerReturnsKnownHostsSetupError(t *testing.T) {
	result := Runner{
		NewHostKeyCallback: func(path string) (ssh.HostKeyCallback, error) {
			return nil, errors.New("bad known hosts")
		},
	}.Run(context.Background(), Request{Host: "example.com", Port: 22, Username: "demo", Password: "secret", RemoteCommand: "hostname"})
	if !strings.Contains(result.Error, "known-hosts verification setup failed") {
		t.Fatalf("error = %q", result.Error)
	}
}

func TestRunnerDebugLogsRemainSecretSafe(t *testing.T) {
	server := newTestSSHServer(t)
	defer server.Close()

	var logs bytes.Buffer
	result := Runner{Timeout: time.Second, OutputLimit: 1024, Debug: true, Logger: log.New(&logs, "", 0)}.Run(context.Background(), Request{
		Host:            server.host,
		Port:            server.port,
		Username:        server.username,
		Password:        server.password,
		RemoteCommand:   "echo secret",
		SensitiveValues: []string{server.username, server.password},
		KnownHostsPath:  server.knownHostsPath,
		Timeout:         time.Second,
		OutputLimit:     1024,
	})
	if result.Error != "" {
		t.Fatalf("result error = %q", result.Error)
	}
	output := logs.String()
	for _, forbidden := range []string{server.username, server.password, "echo secret"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("debug log leaked %q in:\n%s", forbidden, output)
		}
	}
	for _, want := range []string{
		"debug ssh effective request",
		`username="[REDACTED]"`,
		`remote_command="echo [REDACTED]"`,
		"debug ssh call start",
		"host=" + server.host,
		"strategy=go_ssh_client",
		"debug ssh call finish",
		`debug ssh result host=127.0.0.1 port=`,
		`stdout="hidden\n"`,
		`stderr=""`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug log missing %q in:\n%s", want, output)
		}
	}
}

func TestRunnerDebugResultLogsRawCommandOutput(t *testing.T) {
	server := newTestSSHServer(t)
	defer server.Close()

	var logs bytes.Buffer
	result := Runner{Timeout: time.Second, OutputLimit: 1024, Debug: true, Logger: log.New(&logs, "", 0)}.Run(context.Background(), Request{
		Host:            server.host,
		Port:            server.port,
		Username:        server.username,
		Password:        server.password,
		RemoteCommand:   "show creds",
		SensitiveValues: []string{server.username, server.password},
		KnownHostsPath:  server.knownHostsPath,
		Timeout:         time.Second,
		OutputLimit:     1024,
	})
	if result.Error != "" {
		t.Fatalf("result error = %q", result.Error)
	}
	output := logs.String()
	for _, want := range []string{
		`debug ssh result host=127.0.0.1 port=`,
		`stdout="demo secret\n"`,
		`stderr=""`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug result log missing %q in:\n%s", want, output)
		}
	}
}

type testSSHServer struct {
	listener       net.Listener
	host           string
	port           int
	username       string
	password       string
	knownHostsPath string
	otherSigner    ssh.Signer
	hostSigner     ssh.Signer
}

func newTestSSHServer(t *testing.T) *testSSHServer {
	t.Helper()
	return newTestSSHServerWithPublicKey(t, nil)
}

func newTestSSHServerWithPublicKey(t *testing.T, authorizedPublicKey ssh.PublicKey) *testSSHServer {
	t.Helper()

	signer := newSigner(t)
	otherSigner := newSigner(t)
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == "demo" && string(password) == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid credentials")
		},
	}
	if authorizedPublicKey != nil {
		config.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == "demo" && bytes.Equal(key.Marshal(), authorizedPublicKey.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid public key")
		}
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	server := &testSSHServer{
		listener:       listener,
		host:           "127.0.0.1",
		port:           listener.Addr().(*net.TCPAddr).Port,
		username:       "demo",
		password:       "secret",
		knownHostsPath: writeKnownHosts(t, "127.0.0.1", listener.Addr().(*net.TCPAddr).Port, signer),
		otherSigner:    otherSigner,
		hostSigner:     signer,
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleSSHConn(conn, config)
		}
	}()

	return server
}

func (s *testSSHServer) Close() {
	_ = s.listener.Close()
}

func handleSSHConn(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()

	_, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func(channel ssh.Channel, requests <-chan *ssh.Request) {
			defer channel.Close()
			for req := range requests {
				switch req.Type {
				case "exec":
					var payload struct {
						Command string
					}
					_ = ssh.Unmarshal(req.Payload, &payload)
					_ = req.Reply(true, nil)
					switch payload.Command {
					case "hostname":
						_, _ = io.WriteString(channel, "test-host\n")
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: 0}))
					case "echo secret":
						_, _ = io.WriteString(channel, "hidden\n")
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: 0}))
					case "show creds":
						_, _ = io.WriteString(channel, "demo secret\n")
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: 0}))
					default:
						_, _ = io.WriteString(channel, "unsupported\n")
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: 1}))
					}
					return
				default:
					_ = req.Reply(false, nil)
				}
			}
		}(channel, requests)
	}
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, signer := newPrivateKey(t)
	return signer
}

func newPrivateKey(t *testing.T) (*rsa.PrivateKey, ssh.Signer) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, signer
}

func writePrivateKey(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "id_rsa")
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeKnownHosts(t *testing.T, host string, port int, signer ssh.Signer) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	line := knownhosts.Line([]string{fmt.Sprintf("[%s]:%d", host, port)}, signer.PublicKey())
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
