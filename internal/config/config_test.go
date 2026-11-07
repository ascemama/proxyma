package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if cfg.ListenAddress != "127.0.0.1:8080" {
		t.Fatalf("ListenAddress = %q", cfg.ListenAddress)
	}
	if cfg.Scheme != SchemeHTTPS || cfg.TLSMode != TLSModeSelfSigned {
		t.Fatalf("unexpected transport defaults: %#v", cfg)
	}
	if cfg.TLSCertificateFile != "tls/self-signed.crt" || cfg.TLSPrivateKeyFile != "tls/self-signed.key" {
		t.Fatalf("unexpected TLS file defaults: %#v", cfg)
	}
	if cfg.TargetDir != "var/proxyma/targets" {
		t.Fatalf("TargetDir = %q", cfg.TargetDir)
	}
	if cfg.PolicyDir != "var/proxyma/policies" {
		t.Fatalf("PolicyDir = %q", cfg.PolicyDir)
	}
	if cfg.AgentDir != "var/proxyma/agents" {
		t.Fatalf("AgentDir = %q", cfg.AgentDir)
	}
	if cfg.UserDir != "var/proxyma/users" {
		t.Fatalf("UserDir = %q", cfg.UserDir)
	}
	if cfg.DatabasePath != "var/proxyma/database.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.InteractionConfigDir != "var/proxyma/interaction_instances" {
		t.Fatalf("InteractionConfigDir = %q", cfg.InteractionConfigDir)
	}
	if cfg.UserSessionDir != "var/proxyma/user_sessions" {
		t.Fatalf("UserSessionDir = %q", cfg.UserSessionDir)
	}
	if cfg.CredentialTokenDir != "var/proxyma/credential_tokens" {
		t.Fatalf("CredentialTokenDir = %q", cfg.CredentialTokenDir)
	}
	if cfg.LLMProfileDir != "var/proxyma/llm_profiles" {
		t.Fatalf("LLMProfileDir = %q", cfg.LLMProfileDir)
	}
	if cfg.KnownHostsPath != filepath.Join("var", "proxyma", ".ssh", "known_hosts") {
		t.Fatalf("KnownHostsPath = %q", cfg.KnownHostsPath)
	}
	if cfg.CommandTimeout != 5*time.Minute {
		t.Fatalf("CommandTimeout = %s", cfg.CommandTimeout)
	}
	if cfg.InteractionTimeout != 24*time.Hour {
		t.Fatalf("InteractionTimeout = %s", cfg.InteractionTimeout)
	}
	if cfg.UserSessionTTL != 12*time.Hour {
		t.Fatalf("UserSessionTTL = %s", cfg.UserSessionTTL)
	}
	if cfg.UserActivationTokenTTL != 12*time.Hour || cfg.UserActivationTokenMaxTTL != 48*time.Hour {
		t.Fatalf("activation token lifetimes = %s / %s", cfg.UserActivationTokenTTL, cfg.UserActivationTokenMaxTTL)
	}
	if cfg.OutputLimit != 1<<20 {
		t.Fatalf("OutputLimit = %d", cfg.OutputLimit)
	}
	if !cfg.RequireCommandApproval {
		t.Fatal("RequireCommandApproval = false")
	}
	if cfg.Debug {
		t.Fatal("Debug = true")
	}
}

func TestFromArgs(t *testing.T) {
	cfg, err := FromArgs([]string{
		"--listen", "127.0.0.1:0",
		"--scheme", "http",
		"--tls-mode", "provided",
		"--tls-cert-file", "provided.crt",
		"--tls-key-file", "provided.key",
		"--target-dir", "targets-test",
		"--policy-dir", "policies-test",
		"--agent-dir", "agents-test",
		"--user-dir", "users-test",
		"--database-path", "database-test.db",
		"--interaction-config-dir", "interaction-configs-test",
		"--user-session-dir", "sessions-test",
		"--credential-token-dir", "credential-tokens-test",
		"--llm-profile-dir", "llm-profiles-test",
		"--known-hosts-path", "known-hosts-test",
		"--command-timeout", "2s",
		"--interaction-timeout", "3s",
		"--user-session-ttl", "4h",
		"--output-limit", "128",
		"--require-command-approval=false",
		"--debug",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddress != "127.0.0.1:0" || cfg.TargetDir != "targets-test" || cfg.PolicyDir != "policies-test" || cfg.AgentDir != "agents-test" || cfg.UserDir != "users-test" || cfg.DatabasePath != "database-test.db" || cfg.InteractionConfigDir != "interaction-configs-test" || cfg.UserSessionDir != "sessions-test" || cfg.CredentialTokenDir != "credential-tokens-test" || cfg.LLMProfileDir != "llm-profiles-test" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if cfg.Scheme != SchemeHTTP || cfg.TLSMode != TLSModeProvided || cfg.TLSCertificateFile != "provided.crt" || cfg.TLSPrivateKeyFile != "provided.key" {
		t.Fatalf("unexpected transport config: %#v", cfg)
	}
	if cfg.KnownHostsPath != "known-hosts-test" {
		t.Fatalf("KnownHostsPath = %q", cfg.KnownHostsPath)
	}
	if cfg.CommandTimeout != 2*time.Second {
		t.Fatalf("CommandTimeout = %s", cfg.CommandTimeout)
	}
	if cfg.InteractionTimeout != 3*time.Second {
		t.Fatalf("InteractionTimeout = %s", cfg.InteractionTimeout)
	}
	if cfg.UserSessionTTL != 4*time.Hour {
		t.Fatalf("UserSessionTTL = %s", cfg.UserSessionTTL)
	}
	if cfg.OutputLimit != 128 {
		t.Fatalf("OutputLimit = %d", cfg.OutputLimit)
	}
	if cfg.RequireCommandApproval {
		t.Fatal("RequireCommandApproval = true")
	}
	if !cfg.Debug {
		t.Fatal("Debug = false")
	}
}

func TestFromArgsRejectsLegacyInteractionProviderFlags(t *testing.T) {
	for _, flag := range []string{"--interaction-provider", "--ntfy-base-url", "--ntfy-request-topic", "--ntfy-response-topic", "--ntfy-token", "--ntfy-poll-interval", "--ntfy-poll-timeout", "--ntfy-responded-by"} {
		if _, err := FromArgs([]string{flag, "legacy"}); err == nil {
			t.Fatalf("expected %s to be rejected", flag)
		}
	}
}

func TestFromArgsRejectsLegacyAssetDirFlag(t *testing.T) {
	if _, err := FromArgs([]string{"--asset-dir", "legacy-targets"}); err == nil {
		t.Fatal("expected --asset-dir to be rejected")
	}
}

func TestLoadRejectsLegacyUserActivationDirectorySettings(t *testing.T) {
	if _, err := FromArgs([]string{"--user-activation-dir", "legacy"}); err == nil {
		t.Fatal("expected legacy activation directory flag to be rejected")
	}
	withWorkingDir(t, t.TempDir())
	writeConfig(t, "proxyma.config.json", `{"paths":{"user_activation_dir":"legacy"}}`)
	if _, err := Load(nil, emptyEnv); err == nil {
		t.Fatal("expected legacy activation directory config key to be rejected")
	}
}

func TestFromArgsDefaultsToTargetsWhenLegacyAssetDirExists(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	if err := os.Mkdir("asset", 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TargetDir != "var/proxyma/targets" {
		t.Fatalf("TargetDir = %q", cfg.TargetDir)
	}
}

func TestFromArgsDoesNotAutoSelectSingularTargetDir(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	if err := os.Mkdir("target", 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TargetDir != "var/proxyma/targets" {
		t.Fatalf("TargetDir = %q", cfg.TargetDir)
	}
}

func TestLoadReadsDefaultConfigFile(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	writeConfig(t, "proxyma.config.json", `{
  "server": {
    "listen_address": "127.0.0.1:9090",
    "scheme": "https",
    "tls": {
      "mode": "provided",
      "certificate_file": "file.crt",
      "private_key_file": "file.key",
      "server_name": "proxyma.example"
    }
  },
  "paths": {
    "target_dir": "targets-from-file",
    "policy_dir": "policies-from-file",
    "agent_dir": "agents-from-file",
    "user_dir": "users-from-file",
    "database_path": "database-from-file.db",
    "interaction_config_dir": "interaction-configs-from-file",
    "user_session_dir": "sessions-from-file",
    "credential_token_dir": "credential-tokens-from-file",
    "known_hosts_path": "known-hosts-from-file"
  },
  "execution": {
    "command_timeout": "7s",
    "interaction_timeout": "9s",
    "output_limit": 256,
    "require_command_approval": false
  },
  "security": {"user_session_ttl": "11h"},
  "logging": {"debug": true}
}`)
	cfg, err := Load(nil, emptyEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddress != "127.0.0.1:9090" || cfg.TargetDir != "targets-from-file" || cfg.PolicyDir != "policies-from-file" || cfg.AgentDir != "agents-from-file" || cfg.UserDir != "users-from-file" || cfg.DatabasePath != "database-from-file.db" || cfg.InteractionConfigDir != "interaction-configs-from-file" || cfg.UserSessionDir != "sessions-from-file" || cfg.CredentialTokenDir != "credential-tokens-from-file" {
		t.Fatalf("unexpected file config paths: %#v", cfg)
	}
	if cfg.CommandTimeout != 7*time.Second || cfg.InteractionTimeout != 9*time.Second || cfg.UserSessionTTL != 11*time.Hour || cfg.OutputLimit != 256 {
		t.Fatalf("unexpected file config durations/limits: %#v", cfg)
	}
	if cfg.RequireCommandApproval || !cfg.Debug {
		t.Fatalf("unexpected file config bools: %#v", cfg)
	}
	if cfg.TLSMode != TLSModeProvided || cfg.TLSCertificateFile != "file.crt" || cfg.TLSPrivateKeyFile != "file.key" || cfg.TLSServerName != "proxyma.example" {
		t.Fatalf("unexpected file TLS config: %#v", cfg)
	}
}

func TestLoadIgnoresObsoleteInsecureCookieSetting(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	writeConfig(t, "proxyma.config.json", `{
  "server": {"scheme": "https"},
  "security": {"allow_insecure_user_cookies": true}
}`)
	cfg, err := Load(nil, emptyEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scheme != SchemeHTTPS {
		t.Fatalf("obsolete cookie setting changed scheme: %#v", cfg)
	}
}

func TestLoadPrecedenceDefaultsConfigEnvCLI(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	writeConfig(t, "proxyma.config.json", `{
  "server": {
    "listen_address": "from-file",
    "scheme": "https",
    "tls": {"mode": "self_signed", "certificate_file": "file.crt", "private_key_file": "file.key"}
  },
  "paths": {
    "target_dir": "target-file",
    "policy_dir": "policy-file",
    "agent_dir": "agent-file",
    "user_dir": "user-file",
    "database_path": "database-file.db",
    "interaction_config_dir": "interaction-config-file",
    "user_session_dir": "session-file",
    "credential_token_dir": "credential-token-file",
    "known_hosts_path": "known-hosts-file"
  },
  "execution": {
    "command_timeout": "2s",
    "interaction_timeout": "2s",
    "output_limit": 2,
    "require_command_approval": true
  },
  "security": {"user_session_ttl": "2h"},
  "logging": {"debug": false}
}`)
	env := mapEnv(map[string]string{
		"PROXYMA_LISTEN":                   "from-env",
		"PROXYMA_SCHEME":                   "http",
		"PROXYMA_TLS_MODE":                 "provided",
		"PROXYMA_TLS_CERT_FILE":            "env.crt",
		"PROXYMA_TLS_KEY_FILE":             "env.key",
		"PROXYMA_TARGET_DIR":               "target-env",
		"PROXYMA_POLICY_DIR":               "policy-env",
		"PROXYMA_USER_DIR":                 "user-env",
		"PROXYMA_DATABASE_PATH":            "database-env.db",
		"PROXYMA_INTERACTION_CONFIG_DIR":   "interaction-config-env",
		"PROXYMA_USER_SESSION_DIR":         "session-env",
		"PROXYMA_CREDENTIAL_TOKEN_DIR":     "credential-token-env",
		"PROXYMA_USER_SESSION_TTL":         "3h",
		"PROXYMA_COMMAND_TIMEOUT":          "3s",
		"PROXYMA_REQUIRE_COMMAND_APPROVAL": "false",
		"PROXYMA_DEBUG":                    "true",
	})
	cfg, err := Load([]string{
		"--listen", "from-cli",
		"--scheme", "https",
		"--tls-cert-file", "cli.crt",
		"--tls-key-file", "cli.key",
		"--command-timeout", "4s",
		"--user-session-ttl", "4h",
		"--credential-token-dir", "credential-token-cli",
		"--require-command-approval=true",
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddress != "from-cli" {
		t.Fatalf("ListenAddress = %q", cfg.ListenAddress)
	}
	if cfg.Scheme != SchemeHTTPS || cfg.TLSMode != TLSModeProvided || cfg.TLSCertificateFile != "cli.crt" || cfg.TLSPrivateKeyFile != "cli.key" {
		t.Fatalf("unexpected transport precedence: %#v", cfg)
	}
	if cfg.TargetDir != "target-env" {
		t.Fatalf("TargetDir = %q", cfg.TargetDir)
	}
	if cfg.PolicyDir != "policy-env" {
		t.Fatalf("PolicyDir = %q", cfg.PolicyDir)
	}
	if cfg.AgentDir != "agent-file" {
		t.Fatalf("AgentDir = %q", cfg.AgentDir)
	}
	if cfg.UserDir != "user-env" {
		t.Fatalf("UserDir = %q", cfg.UserDir)
	}
	if cfg.DatabasePath != "database-env.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.InteractionConfigDir != "interaction-config-env" {
		t.Fatalf("InteractionConfigDir = %q", cfg.InteractionConfigDir)
	}
	if cfg.UserSessionDir != "session-env" {
		t.Fatalf("UserSessionDir = %q", cfg.UserSessionDir)
	}
	if cfg.CredentialTokenDir != "credential-token-cli" {
		t.Fatalf("CredentialTokenDir = %q", cfg.CredentialTokenDir)
	}
	if cfg.CommandTimeout != 4*time.Second {
		t.Fatalf("CommandTimeout = %s", cfg.CommandTimeout)
	}
	if cfg.UserSessionTTL != 4*time.Hour {
		t.Fatalf("UserSessionTTL = %s", cfg.UserSessionTTL)
	}
	if !cfg.RequireCommandApproval {
		t.Fatal("RequireCommandApproval = false")
	}
	if !cfg.Debug {
		t.Fatal("Debug = false")
	}
}

func TestLoadNoConfigIgnoresDefaultConfigFile(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	if err := os.Mkdir("asset", 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "proxyma.config.json", `{
  "paths": {"target_dir": "from-file"}
}`)
	cfg, err := Load([]string{"--no-config"}, emptyEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TargetDir != "var/proxyma/targets" {
		t.Fatalf("TargetDir = %q", cfg.TargetDir)
	}
}

func TestLoadRejectsActivationTTLAboveMaximum(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	writeConfig(t, "proxyma.config.json", `{"security":{"user_activation_token_ttl":"49h","user_activation_token_max_ttl":"48h"}}`)
	if _, err := Load(nil, emptyEnv); err == nil || !strings.Contains(err.Error(), "must not exceed maximum") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsMissingExplicitConfigFile(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	if _, err := Load([]string{"--config", "missing.json"}, emptyEnv); err == nil {
		t.Fatal("expected missing explicit config file to be rejected")
	}
}

func TestLoadRejectsMalformedConfigDuration(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	writeConfig(t, "proxyma.config.json", `{
  "execution": {"command_timeout": "soon"}
}`)
	if _, err := Load(nil, emptyEnv); err == nil {
		t.Fatal("expected malformed duration to be rejected")
	}
}

func TestLoadRejectsLegacyInteractionBlock(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	writeConfig(t, "proxyma.config.json", `{
  "interaction": {"selected_provider": "local"}
}`)
	if _, err := Load(nil, emptyEnv); err == nil {
		t.Fatal("expected legacy interaction block to be rejected")
	}
}

func TestLoadRejectsMalformedEnvValue(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	if _, err := Load(nil, mapEnv(map[string]string{"PROXYMA_OUTPUT_LIMIT": "many"})); err == nil {
		t.Fatal("expected malformed env value to be rejected")
	}
}

func TestLoadExpandsKnownHostsHomePath(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	cfg, err := Load([]string{"--known-hosts-path", "~/.ssh/known_hosts"}, emptyEnv)
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KnownHostsPath != filepath.Join(home, ".ssh", "known_hosts") {
		t.Fatalf("KnownHostsPath = %q", cfg.KnownHostsPath)
	}
}

func TestExampleConfigAndSchemaCoverRuntimeFields(t *testing.T) {
	root := repoRoot(t)
	configText, err := os.ReadFile(filepath.Join(root, "proxyma.config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	schemaText, err := os.ReadFile(filepath.Join(root, "schemas", "proxyma.config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(schemaText) {
		t.Fatal("proxyma.config.schema.json is not valid JSON")
	}
	file, err := os.Open(filepath.Join(root, "proxyma.config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rootOverride, err := decodeFileOverride(file)
	if err != nil {
		t.Fatalf("proxyma.config.example.json does not match config decoder: %v", err)
	}
	cfg := Default()
	if err := applyOverride(&cfg, rootOverride); err != nil {
		t.Fatal(err)
	}
	normalize(&cfg)
	if err := validate(cfg); err != nil {
		t.Fatalf("proxyma.config.example.json produces invalid config: %v", err)
	}
	for _, required := range []string{
		"listen_address",
		"scheme",
		"tls",
		"mode",
		"certificate_file",
		"private_key_file",
		"server_name",
		"target_dir",
		"policy_dir",
		"agent_dir",
		"user_dir",
		"database_path",
		"interaction_config_dir",
		"user_session_dir",
		"credential_token_dir",
		"llm_profile_dir",
		"known_hosts_path",
		"command_timeout",
		"interaction_timeout",
		"output_limit",
		"require_command_approval",
		"security",
		"user_session_ttl",
		"user_activation_token_ttl",
		"user_activation_token_max_ttl",
		"debug",
	} {
		if !strings.Contains(string(configText), required) {
			t.Fatalf("proxyma.config.example.json missing %q", required)
		}
		if !strings.Contains(string(schemaText), required) {
			t.Fatalf("proxyma.config.schema.json missing %q", required)
		}
	}
}

func emptyEnv(string) (string, bool) {
	return "", false
}

func mapEnv(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func writeConfig(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(abs); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	})
}
