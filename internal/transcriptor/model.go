// Package transcriptor parses canonical remote commands into internal execution plans.
package transcriptor

import (
	"time"

	"proxyma/internal/sshclient"
	"proxyma/internal/winrmclient"
)

const (
	KindSSHRemoteCommand         = "ssh_remote_command"
	KindWinRMRemoteCommand       = "winrm_remote_command"
	StrategyGoSSHClient          = "go_ssh_client"
	StrategyGoWinRMClient        = "go_winrm_client"
	CodeUnsupportedCommandFormat = "unsupported_command_format"
)

// Metadata is the safe task-visible evidence that Proxyma transcribed a command.
type Metadata struct {
	Transcribed   bool   `json:"transcribed"`
	CommandFamily string `json:"command_family"`
	Strategy      string `json:"strategy"`
	Retryable     bool   `json:"retryable,omitempty"`
}

// Plan is the internal non-persisted execution plan.
type Plan struct {
	Kind             string
	CanonicalCommand string
	SensitiveValues  []string
	Transcription    Metadata
	SSHRequest       *sshclient.Request
	WinRMRequest     *winrmclient.Request
}

// ParsedWinRMCommand is the normalized canonical WinRM request.
type ParsedWinRMCommand struct {
	TargetUserToken string
	TargetHost      string
	PowerShell      string
}

// ParsedSSHCommand is the normalized canonical SSH request.
type ParsedSSHCommand struct {
	TargetUserToken string
	TargetHost      string
	TargetPort      int
	RemoteCommand   string
}

// BuildWinRMPlan constructs the runtime execution plan for one WinRM command.
func BuildWinRMPlan(canonical string, parsed ParsedWinRMCommand, username, powershell, password string, sensitiveValues []string, scheme string, port int, tlsInsecure bool, timeout time.Duration, outputLimit int64) *Plan {
	return &Plan{
		Kind:             KindWinRMRemoteCommand,
		CanonicalCommand: canonical,
		SensitiveValues:  append([]string(nil), sensitiveValues...),
		Transcription: Metadata{
			Transcribed:   true,
			CommandFamily: KindWinRMRemoteCommand,
			Strategy:      StrategyGoWinRMClient,
		},
		WinRMRequest: &winrmclient.Request{
			Host:            parsed.TargetHost,
			Port:            port,
			Scheme:          scheme,
			TLSInsecure:     tlsInsecure,
			Username:        username,
			Password:        password,
			PowerShell:      powershell,
			SensitiveValues: append([]string(nil), sensitiveValues...),
			Timeout:         timeout,
			OutputLimit:     outputLimit,
		},
	}
}

// Error is a harness-visible transcriptor rejection.
type Error struct {
	Code      string
	Retryable bool
	Hint      string
	Message   string
}

func (e *Error) Error() string {
	return e.Message
}

// BuildSSHPlan constructs the runtime execution plan for one created SSH command.
func BuildSSHPlan(canonical string, parsed ParsedSSHCommand, username, remoteCommand string, auth SSHAuth, sensitiveValues []string, knownHostsPath string, timeout time.Duration, outputLimit int64) *Plan {
	return &Plan{
		Kind:             KindSSHRemoteCommand,
		CanonicalCommand: canonical,
		SensitiveValues:  append([]string(nil), sensitiveValues...),
		Transcription: Metadata{
			Transcribed:   true,
			CommandFamily: KindSSHRemoteCommand,
			Strategy:      StrategyGoSSHClient,
		},
		SSHRequest: &sshclient.Request{
			Host:            parsed.TargetHost,
			Port:            parsed.TargetPort,
			Username:        username,
			AuthType:        auth.Type,
			Password:        auth.Password,
			PrivateKeyPath:  auth.PrivateKeyPath,
			RemoteCommand:   remoteCommand,
			SensitiveValues: append([]string(nil), sensitiveValues...),
			KnownHostsPath:  knownHostsPath,
			Timeout:         timeout,
			OutputLimit:     outputLimit,
		},
	}
}

// SSHAuth describes the internal SSH authentication material for a request.
type SSHAuth struct {
	Type           string
	Password       string
	PrivateKeyPath string
}
