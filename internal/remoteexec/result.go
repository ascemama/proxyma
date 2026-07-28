// Package remoteexec defines transport-neutral remote execution results.
package remoteexec

// Result contains one remote command execution outcome.
type Result struct {
	Stdout     string
	Stderr     string
	ExitStatus *int
	Truncated  bool
	Error      string
	ErrorCode  string
	HostKey    *HostKeyInfo
	TimedOut   bool
}

// HostKeyInfo identifies an SSH server key without exposing authentication material.
type HostKeyInfo struct {
	Host        string
	Port        int
	Algorithm   string
	Fingerprint string
}
