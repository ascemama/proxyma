package targets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDirValidTarget(t *testing.T) {
	dir := t.TempDir()
	writeTarget(t, dir, "target.json", `{"$schema":"../schemas/proxyma.target.schema.json","id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","ip":"127.0.0.1","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	target, found, duplicate := registry.MatchTarget("demo-target")
	if !found || duplicate || target.Username != "u" {
		t.Fatalf("match = %#v found=%t duplicate=%t", target, found, duplicate)
	}
	if target.PolicyID != "baseline" {
		t.Fatalf("policy id = %q", target.PolicyID)
	}
	if len(target.AgentIDs) != 1 || target.AgentIDs[0] != "df9ec8bb-1a22-4181-a7c8-126ad553221d" {
		t.Fatalf("agent ids = %#v", target.AgentIDs)
	}
	if len(target.UserIDs) != 1 || target.UserIDs[0] != "fcd2f3a9-d546-44f4-837b-0c7aa4219752" {
		t.Fatalf("user ids = %#v", target.UserIDs)
	}
}

func TestLoadDirRejectsMissingTransport(t *testing.T) {
	dir := t.TempDir()
	content := `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`
	if err := os.WriteFile(filepath.Join(dir, "target.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "missing required field transport") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirRejectsMissingOS(t *testing.T) {
	dir := t.TempDir()
	content := `{"transport":"ssh","id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`
	if err := os.WriteFile(filepath.Join(dir, "target.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "missing required field os") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadDirValidWinRMTarget(t *testing.T) {
	dir := t.TempDir()
	writeTarget(t, dir, "target.json", `{"transport":"winrm","os":"windows","id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"win.example","username":"Administrator","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"winrm":{"scheme":"https"},"auth":{"type":"password","password":"p"}}`)
	registry, diagnostics, err := LoadDir(dir)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("err=%v diagnostics=%#v", err, diagnostics)
	}
	target, found, _ := registry.MatchTarget("win.example")
	if !found || target.Transport != TransportWinRM || target.WinRM == nil || target.WinRM.Port != 5986 {
		t.Fatalf("target = %#v", target)
	}
}

func TestLoadDirRejectsInvalidOSAndTransportOSPair(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "unsupported-os.json",
			content: `{"transport":"ssh","os":"solaris","id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`,
			want:    `unsupported os "solaris"`,
		},
		{
			name:    "winrm-linux.json",
			content: `{"transport":"winrm","os":"linux","id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"win.example","username":"Administrator","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"winrm":{"scheme":"https"},"auth":{"type":"password","password":"p"}}`,
			want:    "WinRM targets require os windows",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.name), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, diagnostics, err := LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, tc.want) {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
		})
	}
}

func TestLoadDirValidPrivateKeyTarget(t *testing.T) {
	configDir := t.TempDir()
	dir := filepath.Join(configDir, "targets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTarget(t, dir, "target.json", `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"private_key","private_key_path":"secrets/private_keys/demo-key"}}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	target, found, duplicate := registry.MatchTarget("demo-target")
	if !found || duplicate || target.Username != "u" {
		t.Fatalf("match = %#v found=%t duplicate=%t", target, found, duplicate)
	}
	privateKeyPath, ok := target.PrivateKeyCredential()
	if !ok {
		t.Fatalf("private key credential missing: %#v", target.Auth)
	}
	wantPrefix := filepath.Join(configDir, "secrets", "private_keys")
	if !strings.HasPrefix(privateKeyPath, wantPrefix+string(filepath.Separator)) {
		t.Fatalf("private key path = %q, want under %q", privateKeyPath, wantPrefix)
	}
}

func TestLoadDirRejectsInvalidTargets(t *testing.T) {
	dir := t.TempDir()
	writeTarget(t, dir, "valid.json", `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"valid-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`)
	writeTarget(t, dir, "missing.json", `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"missing-target","username":"u"}`)
	writeTarget(t, dir, "malformed.json", `{`)
	writeTarget(t, dir, "duplicate.json", `{"id":"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5","ip":"valid-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, duplicate := registry.MatchTarget("valid-target"); !found || !duplicate {
		t.Fatalf("expected duplicate target match found=%t duplicate=%t", found, duplicate)
	}
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics length = %d, want 2: %#v", len(diagnostics), diagnostics)
	}
}

func TestLoadDirRejectsInvalidAuth(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing-auth.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"]}`,
			want:    "missing required field auth",
		},
		{
			name:    "missing-auth-type.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"password":"p"}}`,
			want:    "missing required field auth.type",
		},
		{
			name:    "missing-auth-password.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password"}}`,
			want:    "missing required field auth.password",
		},
		{
			name:    "legacy-top-level-password.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"password":"p"}`,
			want:    "missing required field auth",
		},
		{
			name:    "unsupported-auth-type.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"certificate","certificate_path":"target.pem"}}`,
			want:    `unsupported auth type "certificate"`,
		},
		{
			name:    "missing-private-key-path.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"private_key"}}`,
			want:    "missing required field auth.private_key_path",
		},
		{
			name:    "empty-private-key-path.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"private_key","private_key_path":" "}}`,
			want:    "missing required field auth.private_key_path",
		},
		{
			name:    "absolute-private-key-path.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"private_key","private_key_path":"/secrets/private_keys/demo-key"}}`,
			want:    "auth.private_key_path must be relative",
		},
		{
			name:    "traversal-private-key-path.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"private_key","private_key_path":"secrets/private_keys/../demo-key"}}`,
			want:    "auth.private_key_path must be under secrets/private_keys",
		},
		{
			name:    "outside-private-key-path.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"private_key","private_key_path":"secrets/other/demo-key"}}`,
			want:    "auth.private_key_path must be under secrets/private_keys",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTarget(t, dir, tc.name, tc.content)

			_, diagnostics, err := LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
			if !strings.Contains(diagnostics[0].Message, tc.want) {
				t.Fatalf("diagnostic message = %q, want %q", diagnostics[0].Message, tc.want)
			}
		})
	}
}

func TestLoadDirRejectsMissingOrEmptyPolicyID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing-policy-ids.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","auth":{"type":"password","password":"p"}}`,
			want:    "missing required field policy_id",
		},
		{
			name:    "empty-policy-ids.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`,
			want:    "missing required field policy_id",
		},
		{
			name:    "blank-policy-id.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":" ","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`,
			want:    "missing required field policy_id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTarget(t, dir, tc.name, tc.content)

			_, diagnostics, err := LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
			if !strings.Contains(diagnostics[0].Message, tc.want) {
				t.Fatalf("diagnostic message = %q, want %q", diagnostics[0].Message, tc.want)
			}
		})
	}
}

func TestLoadDirRejectsMissingOrEmptyAgentIDs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing-agent-ids.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`,
			want:    "missing required field agent_ids",
		},
		{
			name:    "empty-agent-ids.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":[],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`,
			want:    "missing required field agent_ids",
		},
		{
			name:    "blank-agent-id.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":[" "],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`,
			want:    "agent_ids must not contain empty values",
		},
		{
			name:    "duplicate-agent-id.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d","df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`,
			want:    "agent_ids must not contain duplicate values",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTarget(t, dir, tc.name, tc.content)

			_, diagnostics, err := LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
			if !strings.Contains(diagnostics[0].Message, tc.want) {
				t.Fatalf("diagnostic message = %q, want %q", diagnostics[0].Message, tc.want)
			}
		})
	}
}

func TestLoadDirRejectsMissingOrEmptyUserIDs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing-user-ids.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"auth":{"type":"password","password":"p"}}`,
			want:    "missing required field user_ids",
		},
		{
			name:    "empty-user-ids.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":[],"auth":{"type":"password","password":"p"}}`,
			want:    "missing required field user_ids",
		},
		{
			name:    "blank-user-id.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":[" "],"auth":{"type":"password","password":"p"}}`,
			want:    "user_ids must not contain empty values",
		},
		{
			name:    "duplicate-user-id.json",
			content: `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"demo-target","username":"u","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752","fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"p"}}`,
			want:    "user_ids must not contain duplicate values",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTarget(t, dir, tc.name, tc.content)

			_, diagnostics, err := LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
			if !strings.Contains(diagnostics[0].Message, tc.want) {
				t.Fatalf("diagnostic message = %q, want %q", diagnostics[0].Message, tc.want)
			}
		})
	}
}

func TestRootTargetSchemaCoversRuntimeFields(t *testing.T) {
	root := repoRoot(t)
	schemaText, err := os.ReadFile(filepath.Join(root, "schemas", "proxyma.target.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(schemaText) {
		t.Fatal("proxyma.target.schema.json is not valid JSON")
	}
	schema := string(schemaText)
	for _, required := range []string{
		"$schema",
		"id",
		"hostname",
		"ip",
		"username",
		"policy_id",
		"agent_ids",
		"user_ids",
		"auth",
		"type",
		"password",
		"private_key",
		"private_key_path",
		"secrets/private_keys",
		"description",
		"transport",
		"os",
		"winrm",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("proxyma.target.schema.json missing %q", required)
		}
	}
}

func writeTarget(t *testing.T, dir, name, content string) {
	t.Helper()
	if !strings.Contains(content, `"transport"`) {
		content = strings.Replace(content, "{", `{"transport":"ssh",`, 1)
	}
	if !strings.Contains(content, `"os"`) {
		content = strings.Replace(content, "{", `{"os":"linux",`, 1)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}
