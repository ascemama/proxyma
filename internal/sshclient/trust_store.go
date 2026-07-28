package sshclient

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"proxyma/internal/remoteexec"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	ErrorCodeHostKeyUnknown = "ssh_host_key_unknown"
	ErrorCodeHostKeyChanged = "ssh_host_key_changed"
)

// TrustStore owns one Proxyma-specific OpenSSH known_hosts file.
type TrustStore struct {
	Path string
	mu   sync.Mutex
}

type pendingHostKey struct {
	hostname string
	remote   net.Addr
	key      ssh.PublicKey
}

type hostKeyError struct {
	code string
	info remoteexec.HostKeyInfo
}

func (e hostKeyError) Error() string {
	if e.code == ErrorCodeHostKeyChanged {
		return "SSH host key changed"
	}
	return "SSH host key is not trusted"
}

// Ensure creates the dedicated trust store without truncating existing entries.
func (s *TrustStore) Ensure() error {
	if s == nil || strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("known hosts path is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create known hosts directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("secure known hosts directory: %w", err)
	}
	file, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create known hosts file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close known hosts file: %w", err)
	}
	if err := os.Chmod(s.Path, 0o600); err != nil {
		return fmt.Errorf("secure known hosts file: %w", err)
	}
	return nil
}

func (s *TrustStore) callback(approvedFingerprint string, pending **pendingHostKey) (ssh.HostKeyCallback, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	verify, err := knownhosts.New(s.Path)
	if err != nil {
		return nil, err
	}
	approvedFingerprint = strings.TrimSpace(approvedFingerprint)
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := verify(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !asKnownHostsKeyError(err, &keyErr) {
			return err
		}
		info := hostKeyInfo(hostname, key)
		if len(keyErr.Want) > 0 {
			return hostKeyError{code: ErrorCodeHostKeyChanged, info: info}
		}
		if approvedFingerprint != "" && approvedFingerprint == info.Fingerprint {
			*pending = &pendingHostKey{hostname: hostname, remote: remote, key: key}
			return nil
		}
		return hostKeyError{code: ErrorCodeHostKeyUnknown, info: info}
	}, nil
}

func asKnownHostsKeyError(err error, target **knownhosts.KeyError) bool {
	keyErr, ok := err.(*knownhosts.KeyError)
	if ok {
		*target = keyErr
	}
	return ok
}

func hostKeyInfo(hostname string, key ssh.PublicKey) remoteexec.HostKeyInfo {
	host, portText, err := net.SplitHostPort(hostname)
	if err != nil {
		host = strings.Trim(hostname, "[]")
		portText = "22"
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 {
		port = 22
	}
	return remoteexec.HostKeyInfo{
		Host:        host,
		Port:        port,
		Algorithm:   key.Type(),
		Fingerprint: ssh.FingerprintSHA256(key),
	}
}

func resultForHostKeyError(err error) (Result, bool) {
	for current := err; current != nil; {
		if typed, ok := current.(hostKeyError); ok {
			info := typed.info
			return Result{Error: typed.Error(), ErrorCode: typed.code, HostKey: &info}, true
		}
		type unwrapper interface{ Unwrap() error }
		wrapped, ok := current.(unwrapper)
		if !ok {
			break
		}
		current = wrapped.Unwrap()
	}
	return Result{}, false
}

// Trust appends a successfully authenticated host key if it is still unknown.
func (s *TrustStore) Trust(pending *pendingHostKey) error {
	if pending == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Ensure(); err != nil {
		return err
	}
	verify, err := knownhosts.New(s.Path)
	if err != nil {
		return fmt.Errorf("load known hosts file: %w", err)
	}
	if err := verify(pending.hostname, pending.remote, pending.key); err == nil {
		return nil
	} else if keyErr, ok := err.(*knownhosts.KeyError); !ok || len(keyErr.Want) > 0 {
		return fmt.Errorf("known hosts entry changed before it could be stored")
	}

	existing, err := os.ReadFile(s.Path)
	if err != nil {
		return fmt.Errorf("read known hosts file: %w", err)
	}
	prefix := ""
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		prefix = "\n"
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(pending.hostname)}, pending.key)
	file, err := os.OpenFile(s.Path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open known hosts file: %w", err)
	}
	if _, err := file.WriteString(prefix + line + "\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("append known hosts file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync known hosts file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close known hosts file: %w", err)
	}
	return nil
}
