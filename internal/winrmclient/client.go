// Package winrmclient executes PowerShell commands through WinRM.
package winrmclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"proxyma/internal/remoteexec"

	"github.com/masterzen/winrm"
)

const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// Request contains one WinRM PowerShell execution request.
type Request struct {
	Host            string
	Port            int
	Scheme          string
	TLSInsecure     bool
	Username        string
	Password        string
	PowerShell      string
	SensitiveValues []string
	Timeout         time.Duration
	OutputLimit     int64
}

// Result contains the WinRM execution outcome.
type Result = remoteexec.Result

// Client runs WinRM requests.
type Client interface {
	Run(ctx context.Context, request Request) Result
}

type commandClient interface {
	RunWithContext(ctx context.Context, command string, stdout io.Writer, stderr io.Writer) (int, error)
}

// Runner is the production WinRM implementation.
type Runner struct {
	Timeout     time.Duration
	OutputLimit int64
	Debug       bool
	Logger      *log.Logger
	NewClient   func(request Request, timeout time.Duration) (commandClient, error)
}

// Run executes one PowerShell payload through WinRM.
func (r Runner) Run(ctx context.Context, request Request) Result {
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = r.Timeout
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	limit := request.OutputLimit
	if limit <= 0 {
		limit = r.OutputLimit
	}
	if limit <= 0 {
		limit = 1 << 20
	}
	scheme := strings.ToLower(strings.TrimSpace(request.Scheme))
	port := request.Port
	if port <= 0 {
		port = defaultPort(scheme)
	}
	r.debugf("debug winrm request prepared host=%s port=%d scheme=%s tls_verify=%t auth=ntlm username_kind=%s timeout=%s output_limit=%d powershell_bytes=%d",
		request.Host, port, scheme, scheme == SchemeHTTPS && !request.TLSInsecure, usernameKind(request.Username), timeout, limit, len(request.PowerShell))

	newClient := r.NewClient
	if newClient == nil {
		newClient = newWinRMClient
	}
	client, err := newClient(request, timeout)
	if err != nil {
		safeError := redact(err.Error(), request.SensitiveValues...)
		r.debugf("debug winrm client setup failed host=%s port=%d scheme=%s error_category=%s error=%q",
			request.Host, port, scheme, errorCategory(safeError), safeError)
		return Result{Error: err.Error()}
	}

	command := encodedPowerShellCommand(request.PowerShell)
	stdout := newLimitedBuffer(limit)
	stderr := newLimitedBuffer(limit)
	r.debugf("debug winrm effective request host=%s port=%d scheme=%s strategy=go_winrm_client username=%q powershell=%q encoded_command_bytes=%d",
		request.Host, port, scheme,
		redact(request.Username, request.SensitiveValues...),
		redact(request.PowerShell, request.SensitiveValues...),
		len(command),
	)
	r.debugf("debug winrm call start host=%s port=%d scheme=%s strategy=go_winrm_client", request.Host, port, scheme)

	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	exitCode, err := client.RunWithContext(runCtx, command, stdout, stderr)
	result := Result{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ExitStatus: &exitCode,
		Truncated:  stdout.Truncated() || stderr.Truncated(),
	}
	if runCtx.Err() == context.DeadlineExceeded {
		result.ExitStatus = nil
		result.Error = "command timed out"
		result.TimedOut = true
		r.debugResult(request, port, scheme, result, time.Since(started))
		return result
	}
	if err != nil {
		result.Error = err.Error()
	}
	r.debugResult(request, port, scheme, result, time.Since(started))
	return result
}

func newWinRMClient(request Request, timeout time.Duration) (commandClient, error) {
	if strings.TrimSpace(request.Host) == "" {
		return nil, fmt.Errorf("WinRM host is required")
	}
	if strings.TrimSpace(request.Username) == "" || strings.TrimSpace(request.Password) == "" {
		return nil, fmt.Errorf("WinRM username and password are required")
	}
	scheme := strings.ToLower(strings.TrimSpace(request.Scheme))
	if scheme != SchemeHTTP && scheme != SchemeHTTPS {
		return nil, fmt.Errorf("unsupported WinRM scheme %q", request.Scheme)
	}
	if request.TLSInsecure && scheme != SchemeHTTPS {
		return nil, fmt.Errorf("WinRM insecure TLS is valid only for HTTPS")
	}
	port := request.Port
	if port <= 0 {
		port = defaultPort(scheme)
	}
	if port > 65535 {
		return nil, fmt.Errorf("WinRM port must be between 1 and 65535")
	}
	endpoint := winrm.NewEndpoint(request.Host, port, scheme == SchemeHTTPS, request.TLSInsecure, nil, nil, nil, timeout)
	parameters := *winrm.DefaultParameters
	parameters.TransportDecorator = func() winrm.Transporter { return &winrm.ClientNTLM{} }
	return winrm.NewClientWithParameters(endpoint, request.Username, request.Password, &parameters)
}

func encodedPowerShellCommand(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[i*2:], unit)
	}
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(data)
}

func defaultPort(scheme string) int {
	if scheme == SchemeHTTPS {
		return 5986
	}
	return 5985
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func newLimitedBuffer(limit int64) *limitedBuffer { return &limitedBuffer{limit: limit} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
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

func (r Runner) debugResult(request Request, port int, scheme string, result Result, duration time.Duration) {
	safeError := redact(result.Error, request.SensitiveValues...)
	status := "completed"
	if result.TimedOut {
		status = "timed_out"
	} else if result.Error != "" || (result.ExitStatus != nil && *result.ExitStatus != 0) {
		status = "failed"
	}
	r.debugf("debug winrm call finish host=%s port=%d scheme=%s strategy=go_winrm_client status=%s exit_status=%s duration=%s stdout_bytes=%d stderr_bytes=%d truncated=%t timed_out=%t error_category=%s error=%q",
		request.Host, port, scheme, status, formatExitStatus(result.ExitStatus), duration,
		len(result.Stdout), len(result.Stderr), result.Truncated, result.TimedOut,
		errorCategory(safeError), safeError)
}

func formatExitStatus(status *int) string {
	if status == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *status)
}

func usernameKind(username string) string {
	switch {
	case strings.Contains(username, `\`):
		return "domain_backslash"
	case strings.Contains(username, "@"):
		return "upn"
	default:
		return "local_or_unqualified"
	}
}

func errorCategory(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "certificate"), strings.Contains(lower, "tls"), strings.Contains(lower, "x509"):
		return "tls"
	case strings.Contains(lower, "401"), strings.Contains(lower, "403"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "forbidden"), strings.Contains(lower, "authentication"), strings.Contains(lower, "credentials"):
		return "authentication"
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "no such host"), strings.Contains(lower, "timeout"), strings.Contains(lower, "dial tcp"):
		return "connection"
	case strings.Contains(lower, "soap"), strings.Contains(lower, "wsman"), strings.Contains(lower, "winrm"):
		return "protocol"
	case strings.TrimSpace(message) == "":
		return "none"
	default:
		return "execution"
	}
}

func redact(text string, values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	return text
}
