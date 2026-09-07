// Package sshclient executes remote commands through an in-process SSH client.
package sshclient

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"proxyma/internal/remoteexec"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Request contains one SSH remote-command execution request.
type Request struct {
	Host                       string
	Port                       int
	Username                   string
	AuthType                   string
	Password                   string
	PrivateKeyPath             string
	RemoteCommand              string
	SensitiveValues            []string
	KnownHostsPath             string
	ApprovedHostKeyFingerprint string
	Timeout                    time.Duration
	OutputLimit                int64
}

const (
	AuthTypePassword   = "password"
	AuthTypePrivateKey = "private_key"
)

// Result contains the SSH execution outcome.
type Result = remoteexec.Result

// Client runs SSH requests.
type Client interface {
	Run(ctx context.Context, request Request) Result
}

// Runner is the production SSH client implementation.
type Runner struct {
	Timeout               time.Duration
	OutputLimit           int64
	DefaultKnownHostsPath string
	Debug                 bool
	Logger                *log.Logger
	DialContext           func(ctx context.Context, network, address string) (net.Conn, error)
	NewHostKeyCallback    func(path string) (ssh.HostKeyCallback, error)
	TrustStore            *TrustStore
}

// Run executes one SSH remote command.
func (r Runner) Run(ctx context.Context, request Request) Result {
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = r.Timeout
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	outputLimit := request.OutputLimit
	if outputLimit <= 0 {
		outputLimit = r.OutputLimit
	}
	if outputLimit <= 0 {
		outputLimit = 1 << 20
	}

	knownHostsPath := request.KnownHostsPath
	if knownHostsPath == "" {
		knownHostsPath = r.DefaultKnownHostsPath
	}

	var pending *pendingHostKey
	callbackFactory := r.NewHostKeyCallback
	var callback ssh.HostKeyCallback
	var err error
	if r.TrustStore != nil && strings.TrimSpace(knownHostsPath) == strings.TrimSpace(r.TrustStore.Path) {
		callback, err = r.TrustStore.callback(request.ApprovedHostKeyFingerprint, &pending)
	} else if callbackFactory == nil {
		callbackFactory = func(path string) (ssh.HostKeyCallback, error) {
			return knownhosts.New(path)
		}
		callback, err = callbackFactory(knownHostsPath)
	} else {
		callback, err = callbackFactory(knownHostsPath)
	}
	if err != nil {
		return Result{Error: fmt.Sprintf("known-hosts verification setup failed: %v", err)}
	}

	addr := net.JoinHostPort(request.Host, strconv.Itoa(portOrDefault(request.Port)))
	dialContext := r.DialContext
	if dialContext == nil {
		dialer := &net.Dialer{Timeout: timeout}
		dialContext = dialer.DialContext
	}
	conn, err := dialContext(ctx, "tcp", addr)
	if err != nil {
		return Result{Error: err.Error()}
	}
	defer conn.Close()

	authMethods, err := authMethodsForRequest(request)
	if err != nil {
		return Result{Error: err.Error()}
	}

	config := &ssh.ClientConfig{
		User:            request.Username,
		Auth:            authMethods,
		HostKeyCallback: callback,
		Timeout:         timeout,
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		if result, ok := resultForHostKeyError(err); ok {
			return result
		}
		return Result{Error: err.Error()}
	}
	client := ssh.NewClient(clientConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return Result{Error: err.Error()}
	}
	defer session.Close()

	stdout := newLimitedBuffer(outputLimit)
	stderr := newLimitedBuffer(outputLimit)
	session.Stdout = stdout
	session.Stderr = stderr

	r.debugf("debug ssh effective request host=%s port=%d strategy=go_ssh_client username=%q remote_command=%q",
		request.Host,
		portOrDefault(request.Port),
		redactCredentialValues(request.Username, request.SensitiveValues...),
		redactCredentialValues(request.RemoteCommand, request.SensitiveValues...),
	)
	r.debugf("debug ssh call start host=%s port=%d strategy=go_ssh_client remote_command_bytes=%d", request.Host, portOrDefault(request.Port), len(request.RemoteCommand))
	if err := session.Start(request.RemoteCommand); err != nil {
		r.debugf("debug ssh call finish host=%s port=%d strategy=go_ssh_client status=failed exit_status=nil stdout_bytes=0 stderr_bytes=0 truncated=false timed_out=false error=%q", request.Host, portOrDefault(request.Port), err.Error())
		return Result{Error: err.Error()}
	}

	waitResult := make(chan error, 1)
	go func() {
		waitResult <- session.Wait()
	}()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case err := <-waitResult:
		result := Result{
			Stdout:    stdout.String(),
			Stderr:    stderr.String(),
			Truncated: stdout.Truncated() || stderr.Truncated(),
		}
		if err == nil {
			code := 0
			result.ExitStatus = &code
			if pending != nil {
				if err := r.TrustStore.Trust(pending); err != nil {
					return Result{Error: "SSH host key could not be stored"}
				}
			}
			r.debugf("debug ssh call finish host=%s port=%d strategy=go_ssh_client status=completed exit_status=0 stdout_bytes=%d stderr_bytes=%d truncated=%t timed_out=false error=%q",
				request.Host, portOrDefault(request.Port), len(result.Stdout), len(result.Stderr), result.Truncated, "")
			r.debugResult(request, result)
			return result
		}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitStatus()
			result.ExitStatus = &code
		}
		result.Error = err.Error()
		r.debugf("debug ssh call finish host=%s port=%d strategy=go_ssh_client status=failed exit_status=%s stdout_bytes=%d stderr_bytes=%d truncated=%t timed_out=false error=%q",
			request.Host, portOrDefault(request.Port), formatExitStatus(result.ExitStatus), len(result.Stdout), len(result.Stderr), result.Truncated, result.Error)
		r.debugResult(request, result)
		return result
	case <-runCtx.Done():
		_ = client.Close()
		result := Result{
			Stdout:    stdout.String(),
			Stderr:    stderr.String(),
			Truncated: stdout.Truncated() || stderr.Truncated(),
			Error:     "command timed out",
			TimedOut:  true,
		}
		r.debugf("debug ssh call finish host=%s port=%d strategy=go_ssh_client status=timed_out exit_status=nil stdout_bytes=%d stderr_bytes=%d truncated=%t timed_out=true error=%q",
			request.Host, portOrDefault(request.Port), len(result.Stdout), len(result.Stderr), result.Truncated, result.Error)
		r.debugResult(request, result)
		return result
	}
}

func authMethodsForRequest(request Request) ([]ssh.AuthMethod, error) {
	authType := request.AuthType
	if strings.TrimSpace(authType) == "" {
		authType = AuthTypePassword
	}

	switch authType {
	case AuthTypePassword:
		if strings.TrimSpace(request.Password) == "" {
			return nil, fmt.Errorf("password authentication setup failed: missing password")
		}
		return []ssh.AuthMethod{ssh.Password(request.Password)}, nil
	case AuthTypePrivateKey:
		if strings.TrimSpace(request.PrivateKeyPath) == "" {
			return nil, fmt.Errorf("private-key authentication setup failed: missing private key path")
		}
		data, err := os.ReadFile(request.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("private-key authentication setup failed: could not read private key")
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("private-key authentication setup failed: could not parse unencrypted private key")
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, fmt.Errorf("unsupported SSH auth type %q", authType)
	}
}

type limitedBuffer struct {
	buf       []byte
	limit     int64
	truncated bool
}

func newLimitedBuffer(limit int64) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - int64(len(b.buf))
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		b.buf = append(b.buf, p[:int(remaining)]...)
		b.truncated = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return string(b.buf)
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}

func (r Runner) debugf(format string, args ...any) {
	if !r.Debug {
		return
	}
	logger := r.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf(format, args...)
}

func (r Runner) debugResult(request Request, result Result) {
	r.debugf("debug ssh result host=%s port=%d strategy=go_ssh_client stdout=%q stderr=%q",
		request.Host,
		portOrDefault(request.Port),
		result.Stdout,
		result.Stderr,
	)
}

func portOrDefault(port int) int {
	if port > 0 {
		return port
	}
	return 22
}

func formatExitStatus(status *int) string {
	if status == nil {
		return "nil"
	}
	return strconv.Itoa(*status)
}

func redactCredentialValues(text string, values ...string) string {
	redacted := text
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		redacted = strings.ReplaceAll(redacted, value, "[REDACTED]")
	}
	return redacted
}
