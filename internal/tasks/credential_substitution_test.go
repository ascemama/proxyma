package tasks

import (
	"strings"
	"testing"

	"proxyma/internal/targets"
)

func TestResolveCredentialPlaceholders(t *testing.T) {
	target := targets.Target{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "demo", Auth: targets.PasswordAuth("secret")}
	resolved, err := ResolveCredentialPlaceholders("echo {password} {user}", target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Command != "echo secret demo" {
		t.Fatalf("command = %q", resolved.Command)
	}
	if got := strings.Join(resolved.UsedPlaceholders, ","); got != "user,password" {
		t.Fatalf("used placeholders = %q", got)
	}
}

func TestResolveCredentialPlaceholdersPreservesUnsupportedTokens(t *testing.T) {
	target := targets.Target{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "demo", Auth: targets.PasswordAuth("secret")}
	resolved, err := ResolveCredentialPlaceholders("echo {username} {token}", target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Command != "echo {username} {token}" {
		t.Fatalf("command = %q", resolved.Command)
	}
	if len(resolved.UsedPlaceholders) != 0 {
		t.Fatalf("used placeholders = %#v, want none", resolved.UsedPlaceholders)
	}
}

func TestResolveCredentialPlaceholdersRequiresCredentialValues(t *testing.T) {
	target := targets.Target{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "", Auth: targets.PasswordAuth("secret")}
	_, err := ResolveCredentialPlaceholders("echo {user}", target)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked credential value: %q", err.Error())
	}
}

func TestValidateCommandPolicyRejectsUnauthorizedCommand(t *testing.T) {
	target := targets.Target{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "demo", Auth: targets.PasswordAuth("secret")}
	err := ValidateCommandPolicy("ls /home", target)
	if err == nil {
		t.Fatal("expected unauthorized command error")
	}
	if !strings.Contains(err.Error(), "supports canonical ssh") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestValidateCommandPolicyRejectsWrongHost(t *testing.T) {
	target := targets.Target{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "demo", Auth: targets.PasswordAuth("secret")}
	err := ValidateCommandPolicy("ssh {user}@192.0.2.10 hostname", target)
	if err == nil {
		t.Fatal("expected host mismatch error")
	}
	if !strings.Contains(err.Error(), "target host conflicts") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestValidateCommandPolicyAcceptsCanonicalSSH(t *testing.T) {
	target := targets.Target{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "demo", Auth: targets.PasswordAuth("secret")}
	if err := ValidateCommandPolicy(`ssh -o BatchMode=yes -p 22 {user}@127.0.0.1 "hostname"`, target); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCommandPolicyAcceptsCanonicalWinRM(t *testing.T) {
	target := targets.Target{Transport: targets.TransportWinRM, Hostname: "win.example", Username: "Administrator", Auth: targets.PasswordAuth("secret"), WinRM: &targets.WinRM{Scheme: "https", Port: 5986}}
	if err := ValidateCommandPolicy(`winrm {user}@win.example Get-ChildItem "C:\Program Files"`, target); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCommandPolicy(`ssh {user}@win.example hostname`, target); err == nil {
		t.Fatal("expected transport mismatch")
	}
}
