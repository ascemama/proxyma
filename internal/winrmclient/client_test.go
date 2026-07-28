package winrmclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

type fakeCommandClient struct {
	command string
	stdout  string
	stderr  string
	exit    int
	err     error
}

func (f *fakeCommandClient) RunWithContext(_ context.Context, command string, stdout io.Writer, stderr io.Writer) (int, error) {
	f.command = command
	_, _ = io.WriteString(stdout, f.stdout)
	_, _ = io.WriteString(stderr, f.stderr)
	return f.exit, f.err
}

func TestRunnerEncodesPowerShellAndCapturesResult(t *testing.T) {
	fake := &fakeCommandClient{stdout: "output", stderr: "warning", exit: 0}
	runner := Runner{NewClient: func(request Request, timeout time.Duration) (commandClient, error) {
		if request.Scheme != SchemeHTTPS || request.Port != 5986 || timeout != time.Second {
			t.Fatalf("request=%#v timeout=%s", request, timeout)
		}
		return fake, nil
	}}
	result := runner.Run(context.Background(), Request{Host: "win.example", Port: 5986, Scheme: SchemeHTTPS, Username: "u", Password: "p", PowerShell: `Get-Service`, Timeout: time.Second, OutputLimit: 1024})
	if result.Error != "" || result.Stdout != "output" || result.Stderr != "warning" || result.ExitStatus == nil || *result.ExitStatus != 0 {
		t.Fatalf("result = %#v", result)
	}
	const prefix = "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand "
	if !strings.HasPrefix(fake.command, prefix) {
		t.Fatalf("command = %q", fake.command)
	}
	encoded := strings.TrimPrefix(fake.command, prefix)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	if decoded := string(utf16.Decode(units)); decoded != "Get-Service" {
		t.Fatalf("decoded command = %q", decoded)
	}
}

func TestRunnerTruncatesOutput(t *testing.T) {
	fake := &fakeCommandClient{stdout: "123456", exit: 0}
	runner := Runner{NewClient: func(Request, time.Duration) (commandClient, error) { return fake, nil }}
	result := runner.Run(context.Background(), Request{PowerShell: "x", OutputLimit: 3})
	if result.Stdout != "123" || !result.Truncated {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunnerDebugLogClassifiesAndRedactsFailure(t *testing.T) {
	var logs bytes.Buffer
	fake := &fakeCommandClient{err: errors.New("401 unauthorized for secret"), exit: 1}
	runner := Runner{
		Debug: true, Logger: log.New(&logs, "", 0),
		NewClient: func(Request, time.Duration) (commandClient, error) { return fake, nil },
	}
	result := runner.Run(context.Background(), Request{
		Host: "win.example", Scheme: SchemeHTTPS, Port: 5986, Username: `DOMAIN\user`, Password: "secret",
		PowerShell: "Get-Service", SensitiveValues: []string{"secret", `DOMAIN\user`}, Timeout: time.Second,
	})
	if result.Error == "" {
		t.Fatal("expected failure")
	}
	output := logs.String()
	for _, expected := range []string{"debug winrm request prepared", "auth=ntlm", "username_kind=domain_backslash", "tls_verify=true", "debug winrm call start", "debug winrm call finish", "error_category=authentication"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("log missing %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "secret") || strings.Contains(output, `DOMAIN\user`) {
		t.Fatalf("log leaked credential: %s", output)
	}
}

func TestEncodedPowerShellUsesUTF16LE(t *testing.T) {
	command := encodedPowerShellCommand("台灣")
	encoded := command[strings.LastIndex(command, " ")+1:]
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, []byte("台灣")) || len(data) != 4 {
		t.Fatalf("encoded bytes = %x", data)
	}
}
