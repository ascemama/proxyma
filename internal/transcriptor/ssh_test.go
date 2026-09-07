package transcriptor

import "testing"

func TestParseSSHAcceptsCanonicalCommand(t *testing.T) {
	parsed, err := ParseSSH(`ssh -o BatchMode=yes -p 2222 {user}@example.com "ls -1 /bin"`)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TargetUserToken != "{user}" {
		t.Fatalf("target user = %q", parsed.TargetUserToken)
	}
	if parsed.TargetHost != "example.com" {
		t.Fatalf("target host = %q", parsed.TargetHost)
	}
	if parsed.TargetPort != 2222 {
		t.Fatalf("target port = %d", parsed.TargetPort)
	}
	if parsed.RemoteCommand != `"ls -1 /bin"` {
		t.Fatalf("remote command = %q", parsed.RemoteCommand)
	}
}

func TestParseSSHRejectsUnsupportedOption(t *testing.T) {
	_, err := ParseSSH(`ssh -A {user}@example.com hostname`)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != CodeUnsupportedCommandFormat || !err.Retryable {
		t.Fatalf("error = %#v", err)
	}
}

func TestParseSSHRejectsMissingRemoteCommand(t *testing.T) {
	_, err := ParseSSH(`ssh {user}@example.com`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseSSHRejectsUnmatchedQuote(t *testing.T) {
	_, err := ParseSSH(`ssh {user}@example.com "hostname`)
	if err == nil {
		t.Fatal("expected error")
	}
}
