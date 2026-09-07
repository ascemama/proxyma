package transcriptor

import "testing"

func TestParseWinRMPreservesPowerShellPayload(t *testing.T) {
	parsed, err := ParseWinRM(`winrm {user}@win.example Get-ChildItem "C:\Program Files" | Select-Object Name`)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TargetUserToken != "{user}" || parsed.TargetHost != "win.example" {
		t.Fatalf("parsed target = %#v", parsed)
	}
	want := `Get-ChildItem "C:\Program Files" | Select-Object Name`
	if parsed.PowerShell != want {
		t.Fatalf("PowerShell = %q, want %q", parsed.PowerShell, want)
	}
}

func TestParseWinRMRejectsMissingPayload(t *testing.T) {
	_, err := ParseWinRM(`winrm {user}@win.example`)
	if err == nil || !err.Retryable {
		t.Fatalf("error = %#v", err)
	}
}
