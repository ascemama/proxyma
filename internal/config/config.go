// Package config owns Proxyma runtime configuration and defaults.
package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultConfigPath           = "proxyma.config.json"
	DefaultListenAddress        = "127.0.0.1:8080"
	DefaultScheme               = SchemeHTTPS
	DefaultTLSMode              = TLSModeSelfSigned
	DefaultTLSCertificateFile   = "tls/self-signed.crt"
	DefaultTLSPrivateKeyFile    = "tls/self-signed.key"
	DefaultTargetDir            = "var/proxyma/targets"
	DefaultPolicyDir            = "var/proxyma/policies"
	DefaultAgentDir             = "var/proxyma/agents"
	DefaultUserDir              = "var/proxyma/users"
	DefaultTaskDir              = "var/proxyma/tasks"
	DefaultInteractionDir       = "var/proxyma/interactions"
	DefaultInteractionHistoryDB = "var/proxyma/interaction-history.db"
	DefaultInteractionConfigDir = "var/proxyma/interaction_instances"
	DefaultUserSessionDir       = "var/proxyma/user_sessions"
	DefaultCredentialTokenDir   = "var/proxyma/credential_tokens"
	DefaultOutputLimit          = 1 << 20
)

const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"

	TLSModeSelfSigned = "self_signed"
	TLSModeProvided   = "provided"
)

var DefaultCommandTimeout = 5 * time.Minute
var DefaultInteractionTimeout = 24 * time.Hour
var DefaultUserSessionTTL = 12 * time.Hour
var DefaultUserActivationTokenTTL = 12 * time.Hour
var DefaultUserActivationTokenMaxTTL = 48 * time.Hour

// Config contains runtime settings for the Proxyma service.
type Config struct {
	ListenAddress             string
	Scheme                    string
	TLSMode                   string
	TLSCertificateFile        string
	TLSPrivateKeyFile         string
	TLSServerName             string
	TargetDir                 string
	PolicyDir                 string
	AgentDir                  string
	UserDir                   string
	TaskDir                   string
	InteractionDir            string
	InteractionHistoryDB      string
	InteractionConfigDir      string
	UserSessionDir            string
	CredentialTokenDir        string
	KnownHostsPath            string
	CommandTimeout            time.Duration
	InteractionTimeout        time.Duration
	UserSessionTTL            time.Duration
	UserActivationTokenTTL    time.Duration
	UserActivationTokenMaxTTL time.Duration
	OutputLimit               int64
	RequireCommandApproval    bool
	Debug                     bool
	RebuildInteractionHistory bool
}

type loadOptions struct {
	configPath string
	noConfig   bool
	cli        configOverride
}

type configOverride struct {
	ListenAddress             *string
	Scheme                    *string
	TLSMode                   *string
	TLSCertificateFile        *string
	TLSPrivateKeyFile         *string
	TLSServerName             *string
	TargetDir                 *string
	PolicyDir                 *string
	AgentDir                  *string
	UserDir                   *string
	TaskDir                   *string
	InteractionDir            *string
	InteractionHistoryDB      *string
	InteractionConfigDir      *string
	UserSessionDir            *string
	CredentialTokenDir        *string
	KnownHostsPath            *string
	CommandTimeout            *time.Duration
	InteractionTimeout        *time.Duration
	UserSessionTTL            *time.Duration
	UserActivationTokenTTL    *time.Duration
	UserActivationTokenMaxTTL *time.Duration
	OutputLimit               *int64
	RequireCommandApproval    *bool
	Debug                     *bool
	RebuildInteractionHistory *bool
}

type fileConfig struct {
	Schema    *string          `json:"$schema"`
	Server    *serverConfig    `json:"server"`
	Paths     *pathsConfig     `json:"paths"`
	Execution *executionConfig `json:"execution"`
	Security  *securityConfig  `json:"security"`
	Logging   *loggingConfig   `json:"logging"`
}

type serverConfig struct {
	ListenAddress *string    `json:"listen_address"`
	Scheme        *string    `json:"scheme"`
	TLS           *tlsConfig `json:"tls"`
}

type tlsConfig struct {
	Mode            *string `json:"mode"`
	CertificateFile *string `json:"certificate_file"`
	PrivateKeyFile  *string `json:"private_key_file"`
	ServerName      *string `json:"server_name"`
}

type pathsConfig struct {
	TargetDir            *string `json:"target_dir"`
	PolicyDir            *string `json:"policy_dir"`
	AgentDir             *string `json:"agent_dir"`
	UserDir              *string `json:"user_dir"`
	TaskDir              *string `json:"task_dir"`
	InteractionDir       *string `json:"interaction_dir"`
	InteractionHistoryDB *string `json:"interaction_history_db"`
	InteractionConfigDir *string `json:"interaction_config_dir"`
	UserSessionDir       *string `json:"user_session_dir"`
	CredentialTokenDir   *string `json:"credential_token_dir"`
	KnownHostsPath       *string `json:"known_hosts_path"`
}

type executionConfig struct {
	CommandTimeout         *string `json:"command_timeout"`
	InteractionTimeout     *string `json:"interaction_timeout"`
	OutputLimit            *int64  `json:"output_limit"`
	RequireCommandApproval *bool   `json:"require_command_approval"`
}

type securityConfig struct {
	UserSessionTTL            *string `json:"user_session_ttl"`
	UserActivationTokenTTL    *string `json:"user_activation_token_ttl"`
	UserActivationTokenMaxTTL *string `json:"user_activation_token_max_ttl"`
	// ObsoleteAllowInsecureUserCookies is decoded only so pre-HTTPS
	// configurations can migrate forward. Its value is intentionally ignored:
	// cookie security is derived exclusively from server.scheme.
	ObsoleteAllowInsecureUserCookies *bool `json:"allow_insecure_user_cookies"`
}

type loggingConfig struct {
	Debug *bool `json:"debug"`
}

// Default returns Proxyma's safe first-run configuration.
func Default() Config {
	return Config{
		ListenAddress:             DefaultListenAddress,
		Scheme:                    DefaultScheme,
		TLSMode:                   DefaultTLSMode,
		TLSCertificateFile:        DefaultTLSCertificateFile,
		TLSPrivateKeyFile:         DefaultTLSPrivateKeyFile,
		TargetDir:                 DefaultTargetDir,
		PolicyDir:                 DefaultPolicyDir,
		AgentDir:                  DefaultAgentDir,
		UserDir:                   DefaultUserDir,
		TaskDir:                   DefaultTaskDir,
		InteractionDir:            DefaultInteractionDir,
		InteractionHistoryDB:      DefaultInteractionHistoryDB,
		InteractionConfigDir:      DefaultInteractionConfigDir,
		UserSessionDir:            DefaultUserSessionDir,
		CredentialTokenDir:        DefaultCredentialTokenDir,
		KnownHostsPath:            defaultKnownHostsPath(),
		CommandTimeout:            DefaultCommandTimeout,
		InteractionTimeout:        DefaultInteractionTimeout,
		UserSessionTTL:            DefaultUserSessionTTL,
		UserActivationTokenTTL:    DefaultUserActivationTokenTTL,
		UserActivationTokenMaxTTL: DefaultUserActivationTokenMaxTTL,
		OutputLimit:               DefaultOutputLimit,
		RequireCommandApproval:    true,
	}
}

// Load builds Config from hardcoded defaults, the default or explicit JSON file,
// PROXYMA_* environment variables, then explicit CLI flags.
func Load(args []string, lookupEnv func(string) (string, bool)) (Config, error) {
	opts, err := parseLoadOptions(args)
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	targetDirSet := false
	if !opts.noConfig {
		configPath := opts.configPath
		if configPath == "" {
			configPath = DefaultConfigPath
		}
		fileOverride, loaded, err := loadFileOverride(configPath, opts.configPath != "")
		if err != nil {
			return Config{}, err
		}
		if loaded {
			targetDirSet = fileOverride.TargetDir != nil
			if err := applyOverride(&cfg, fileOverride); err != nil {
				return Config{}, err
			}
		}
	}
	envOverride, err := envOverride(lookupEnv)
	if err != nil {
		return Config{}, err
	}
	if envOverride.TargetDir != nil {
		targetDirSet = true
	}
	if err := applyOverride(&cfg, envOverride); err != nil {
		return Config{}, err
	}
	if opts.cli.TargetDir != nil {
		targetDirSet = true
	}
	if err := applyOverride(&cfg, opts.cli); err != nil {
		return Config{}, err
	}
	if !targetDirSet {
		cfg.TargetDir = defaultTargetDir()
	}
	if strings.TrimSpace(cfg.PolicyDir) == "" {
		cfg.PolicyDir = DefaultPolicyDir
	}
	normalize(&cfg)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// FromArgs parses command-line flags into Config without reading config files or
// environment variables. It is retained for package-level tests and callers that
// need the previous CLI-only behavior.
func FromArgs(args []string) (Config, error) {
	opts, err := parseLoadOptions(args)
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	if opts.cli.TargetDir == nil {
		cfg.TargetDir = defaultTargetDir()
	}
	if strings.TrimSpace(cfg.PolicyDir) == "" {
		cfg.PolicyDir = DefaultPolicyDir
	}
	if err := applyOverride(&cfg, opts.cli); err != nil {
		return Config{}, err
	}
	normalize(&cfg)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func parseLoadOptions(args []string) (loadOptions, error) {
	var raw struct {
		configPath                string
		noConfig                  bool
		listen                    string
		scheme                    string
		tlsMode                   string
		tlsCertificateFile        string
		tlsPrivateKeyFile         string
		tlsServerName             string
		targetDir                 string
		policyDir                 string
		agentDir                  string
		userDir                   string
		taskDir                   string
		interactionDir            string
		interactionHistoryDB      string
		interactionConfigDir      string
		userSessionDir            string
		credentialTokenDir        string
		knownHostsPath            string
		commandTimeout            time.Duration
		interactionTimeout        time.Duration
		userSessionTTL            time.Duration
		userActivationTokenTTL    time.Duration
		userActivationTokenMaxTTL time.Duration
		outputLimit               int64
		requireCommandApproval    bool
		debug                     bool
		rebuildInteractionHistory bool
	}
	fs := flag.NewFlagSet("Proxyma", flag.ContinueOnError)
	fs.StringVar(&raw.configPath, "config", "", "Proxyma JSON config file path")
	fs.BoolVar(&raw.noConfig, "no-config", false, "ignore the default Proxyma JSON config file")
	fs.StringVar(&raw.listen, "listen", "", "HTTP listen address")
	fs.StringVar(&raw.scheme, "scheme", "", "server scheme: https or http")
	fs.StringVar(&raw.tlsMode, "tls-mode", "", "TLS mode: self_signed or provided")
	fs.StringVar(&raw.tlsCertificateFile, "tls-cert-file", "", "TLS certificate PEM file")
	fs.StringVar(&raw.tlsPrivateKeyFile, "tls-key-file", "", "TLS private key PEM file")
	fs.StringVar(&raw.tlsServerName, "tls-server-name", "", "additional DNS name or IP for a self-signed certificate")
	fs.StringVar(&raw.targetDir, "target-dir", "", "directory containing target JSON files")
	fs.StringVar(&raw.policyDir, "policy-dir", "", "directory containing policy JSON files")
	fs.StringVar(&raw.agentDir, "agent-dir", "", "directory containing Proxyma agent JSON files")
	fs.StringVar(&raw.userDir, "user-dir", "", "directory containing Proxyma user JSON files")
	fs.StringVar(&raw.taskDir, "task-dir", "", "directory containing persisted task JSON records")
	fs.StringVar(&raw.interactionDir, "interaction-dir", "", "directory containing persisted interaction JSON records")
	fs.StringVar(&raw.interactionHistoryDB, "interaction-history-db", "", "SQLite database path for the interaction history search index")
	fs.StringVar(&raw.interactionConfigDir, "interaction-config-dir", "", "directory containing interaction instance JSON configuration")
	fs.StringVar(&raw.userSessionDir, "user-session-dir", "", "directory containing persisted user session JSON records")
	fs.StringVar(&raw.credentialTokenDir, "credential-token-dir", "", "directory containing persisted activation and password-reset token records")
	fs.StringVar(&raw.knownHostsPath, "known-hosts-path", "", "OpenSSH known_hosts file used for SSH host-key verification")
	fs.DurationVar(&raw.commandTimeout, "command-timeout", 0, "default command execution timeout")
	fs.DurationVar(&raw.interactionTimeout, "interaction-timeout", 0, "default human interaction timeout")
	fs.DurationVar(&raw.userSessionTTL, "user-session-ttl", 0, "GUI/API user session lifetime")
	fs.DurationVar(&raw.userActivationTokenTTL, "user-activation-token-ttl", 0, "default user activation token lifetime")
	fs.DurationVar(&raw.userActivationTokenMaxTTL, "user-activation-token-max-ttl", 0, "maximum user activation token lifetime")
	fs.Int64Var(&raw.outputLimit, "output-limit", 0, "maximum captured stdout or stderr bytes before truncation")
	fs.BoolVar(&raw.requireCommandApproval, "require-command-approval", false, "require local approval before command execution")
	fs.BoolVar(&raw.debug, "debug", false, "log HTTP requests, POST bodies, and command execution details")
	fs.BoolVar(&raw.rebuildInteractionHistory, "rebuild-interaction-history", false, "rebuild the SQLite interaction history index from JSON stores before serving")
	if err := fs.Parse(args); err != nil {
		return loadOptions{}, err
	}
	opts := loadOptions{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config":
			opts.configPath = raw.configPath
		case "no-config":
			opts.noConfig = raw.noConfig
		case "listen":
			opts.cli.ListenAddress = &raw.listen
		case "scheme":
			opts.cli.Scheme = &raw.scheme
		case "tls-mode":
			opts.cli.TLSMode = &raw.tlsMode
		case "tls-cert-file":
			opts.cli.TLSCertificateFile = &raw.tlsCertificateFile
		case "tls-key-file":
			opts.cli.TLSPrivateKeyFile = &raw.tlsPrivateKeyFile
		case "tls-server-name":
			opts.cli.TLSServerName = &raw.tlsServerName
		case "target-dir":
			opts.cli.TargetDir = &raw.targetDir
		case "policy-dir":
			opts.cli.PolicyDir = &raw.policyDir
		case "agent-dir":
			opts.cli.AgentDir = &raw.agentDir
		case "user-dir":
			opts.cli.UserDir = &raw.userDir
		case "task-dir":
			opts.cli.TaskDir = &raw.taskDir
		case "interaction-dir":
			opts.cli.InteractionDir = &raw.interactionDir
		case "interaction-history-db":
			opts.cli.InteractionHistoryDB = &raw.interactionHistoryDB
		case "interaction-config-dir":
			opts.cli.InteractionConfigDir = &raw.interactionConfigDir
		case "user-session-dir":
			opts.cli.UserSessionDir = &raw.userSessionDir
		case "credential-token-dir":
			opts.cli.CredentialTokenDir = &raw.credentialTokenDir
		case "known-hosts-path":
			opts.cli.KnownHostsPath = &raw.knownHostsPath
		case "command-timeout":
			opts.cli.CommandTimeout = &raw.commandTimeout
		case "interaction-timeout":
			opts.cli.InteractionTimeout = &raw.interactionTimeout
		case "user-session-ttl":
			opts.cli.UserSessionTTL = &raw.userSessionTTL
		case "user-activation-token-ttl":
			opts.cli.UserActivationTokenTTL = &raw.userActivationTokenTTL
		case "user-activation-token-max-ttl":
			opts.cli.UserActivationTokenMaxTTL = &raw.userActivationTokenMaxTTL
		case "output-limit":
			opts.cli.OutputLimit = &raw.outputLimit
		case "require-command-approval":
			opts.cli.RequireCommandApproval = &raw.requireCommandApproval
		case "debug":
			opts.cli.Debug = &raw.debug
		case "rebuild-interaction-history":
			opts.cli.RebuildInteractionHistory = &raw.rebuildInteractionHistory
		}
	})
	return opts, nil
}

func loadFileOverride(path string, explicit bool) (configOverride, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return configOverride{}, false, nil
		}
		return configOverride{}, false, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer file.Close()
	override, err := decodeFileOverride(file)
	if err != nil {
		return configOverride{}, false, fmt.Errorf("parse config file %q: %w", path, err)
	}
	return override, true, nil
}

func decodeFileOverride(reader io.Reader) (configOverride, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var raw fileConfig
	if err := decoder.Decode(&raw); err != nil {
		return configOverride{}, err
	}
	var override configOverride
	if raw.Server != nil {
		override.ListenAddress = raw.Server.ListenAddress
		override.Scheme = raw.Server.Scheme
		if raw.Server.TLS != nil {
			override.TLSMode = raw.Server.TLS.Mode
			override.TLSCertificateFile = raw.Server.TLS.CertificateFile
			override.TLSPrivateKeyFile = raw.Server.TLS.PrivateKeyFile
			override.TLSServerName = raw.Server.TLS.ServerName
		}
	}
	if raw.Paths != nil {
		override.TargetDir = raw.Paths.TargetDir
		override.PolicyDir = raw.Paths.PolicyDir
		override.AgentDir = raw.Paths.AgentDir
		override.UserDir = raw.Paths.UserDir
		override.TaskDir = raw.Paths.TaskDir
		override.InteractionDir = raw.Paths.InteractionDir
		override.InteractionHistoryDB = raw.Paths.InteractionHistoryDB
		override.InteractionConfigDir = raw.Paths.InteractionConfigDir
		override.UserSessionDir = raw.Paths.UserSessionDir
		override.CredentialTokenDir = raw.Paths.CredentialTokenDir
		override.KnownHostsPath = raw.Paths.KnownHostsPath
	}
	if raw.Execution != nil {
		var err error
		override.CommandTimeout, err = durationPointer("execution.command_timeout", raw.Execution.CommandTimeout)
		if err != nil {
			return configOverride{}, err
		}
		override.InteractionTimeout, err = durationPointer("execution.interaction_timeout", raw.Execution.InteractionTimeout)
		if err != nil {
			return configOverride{}, err
		}
		override.OutputLimit = raw.Execution.OutputLimit
		override.RequireCommandApproval = raw.Execution.RequireCommandApproval
	}
	if raw.Security != nil {
		var err error
		override.UserSessionTTL, err = durationPointer("security.user_session_ttl", raw.Security.UserSessionTTL)
		if err != nil {
			return configOverride{}, err
		}
		override.UserActivationTokenTTL, err = durationPointer("security.user_activation_token_ttl", raw.Security.UserActivationTokenTTL)
		if err != nil {
			return configOverride{}, err
		}
		override.UserActivationTokenMaxTTL, err = durationPointer("security.user_activation_token_max_ttl", raw.Security.UserActivationTokenMaxTTL)
		if err != nil {
			return configOverride{}, err
		}
	}
	if raw.Logging != nil {
		override.Debug = raw.Logging.Debug
	}
	return override, nil
}

func durationPointer(field string, value *string) (*time.Duration, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := time.ParseDuration(*value)
	if err != nil {
		return nil, fmt.Errorf("%s must be a Go duration: %w", field, err)
	}
	return &parsed, nil
}

func envOverride(lookupEnv func(string) (string, bool)) (configOverride, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	var override configOverride
	for _, entry := range []struct {
		name  string
		apply func(string) error
	}{
		{"PROXYMA_LISTEN", func(v string) error { override.ListenAddress = &v; return nil }},
		{"PROXYMA_SCHEME", func(v string) error { override.Scheme = &v; return nil }},
		{"PROXYMA_TLS_MODE", func(v string) error { override.TLSMode = &v; return nil }},
		{"PROXYMA_TLS_CERT_FILE", func(v string) error { override.TLSCertificateFile = &v; return nil }},
		{"PROXYMA_TLS_KEY_FILE", func(v string) error { override.TLSPrivateKeyFile = &v; return nil }},
		{"PROXYMA_TLS_SERVER_NAME", func(v string) error { override.TLSServerName = &v; return nil }},
		{"PROXYMA_TARGET_DIR", func(v string) error { override.TargetDir = &v; return nil }},
		{"PROXYMA_POLICY_DIR", func(v string) error { override.PolicyDir = &v; return nil }},
		{"PROXYMA_AGENT_DIR", func(v string) error { override.AgentDir = &v; return nil }},
		{"PROXYMA_USER_DIR", func(v string) error { override.UserDir = &v; return nil }},
		{"PROXYMA_TASK_DIR", func(v string) error { override.TaskDir = &v; return nil }},
		{"PROXYMA_INTERACTION_DIR", func(v string) error { override.InteractionDir = &v; return nil }},
		{"PROXYMA_INTERACTION_HISTORY_DB", func(v string) error { override.InteractionHistoryDB = &v; return nil }},
		{"PROXYMA_INTERACTION_CONFIG_DIR", func(v string) error { override.InteractionConfigDir = &v; return nil }},
		{"PROXYMA_USER_SESSION_DIR", func(v string) error { override.UserSessionDir = &v; return nil }},
		{"PROXYMA_CREDENTIAL_TOKEN_DIR", func(v string) error { override.CredentialTokenDir = &v; return nil }},
		{"PROXYMA_KNOWN_HOSTS_PATH", func(v string) error { override.KnownHostsPath = &v; return nil }},
		{"PROXYMA_COMMAND_TIMEOUT", func(v string) error {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			override.CommandTimeout = &parsed
			return nil
		}},
		{"PROXYMA_INTERACTION_TIMEOUT", func(v string) error {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			override.InteractionTimeout = &parsed
			return nil
		}},
		{"PROXYMA_USER_SESSION_TTL", func(v string) error {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			override.UserSessionTTL = &parsed
			return nil
		}},
		{"PROXYMA_USER_ACTIVATION_TOKEN_TTL", func(v string) error {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			override.UserActivationTokenTTL = &parsed
			return nil
		}},
		{"PROXYMA_USER_ACTIVATION_TOKEN_MAX_TTL", func(v string) error {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			override.UserActivationTokenMaxTTL = &parsed
			return nil
		}},
		{"PROXYMA_OUTPUT_LIMIT", func(v string) error {
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			override.OutputLimit = &parsed
			return nil
		}},
		{"PROXYMA_REQUIRE_COMMAND_APPROVAL", func(v string) error {
			parsed, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			override.RequireCommandApproval = &parsed
			return nil
		}},
		{"PROXYMA_DEBUG", func(v string) error {
			parsed, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			override.Debug = &parsed
			return nil
		}},
	} {
		value, ok := lookupEnv(entry.name)
		if !ok {
			continue
		}
		if err := entry.apply(value); err != nil {
			return configOverride{}, fmt.Errorf("%s: %w", entry.name, err)
		}
	}
	return override, nil
}

func applyOverride(cfg *Config, override configOverride) error {
	if override.ListenAddress != nil {
		cfg.ListenAddress = *override.ListenAddress
	}
	if override.Scheme != nil {
		cfg.Scheme = *override.Scheme
	}
	if override.TLSMode != nil {
		cfg.TLSMode = *override.TLSMode
	}
	if override.TLSCertificateFile != nil {
		cfg.TLSCertificateFile = *override.TLSCertificateFile
	}
	if override.TLSPrivateKeyFile != nil {
		cfg.TLSPrivateKeyFile = *override.TLSPrivateKeyFile
	}
	if override.TLSServerName != nil {
		cfg.TLSServerName = *override.TLSServerName
	}
	if override.TargetDir != nil {
		cfg.TargetDir = *override.TargetDir
	}
	if override.PolicyDir != nil {
		cfg.PolicyDir = *override.PolicyDir
	}
	if override.AgentDir != nil {
		cfg.AgentDir = *override.AgentDir
	}
	if override.UserDir != nil {
		cfg.UserDir = *override.UserDir
	}
	if override.TaskDir != nil {
		cfg.TaskDir = *override.TaskDir
	}
	if override.InteractionDir != nil {
		cfg.InteractionDir = *override.InteractionDir
	}
	if override.InteractionHistoryDB != nil {
		cfg.InteractionHistoryDB = *override.InteractionHistoryDB
	}
	if override.InteractionConfigDir != nil {
		cfg.InteractionConfigDir = *override.InteractionConfigDir
	}
	if override.UserSessionDir != nil {
		cfg.UserSessionDir = *override.UserSessionDir
	}
	if override.CredentialTokenDir != nil {
		cfg.CredentialTokenDir = *override.CredentialTokenDir
	}
	if override.KnownHostsPath != nil {
		cfg.KnownHostsPath = *override.KnownHostsPath
	}
	if override.CommandTimeout != nil {
		cfg.CommandTimeout = *override.CommandTimeout
	}
	if override.InteractionTimeout != nil {
		cfg.InteractionTimeout = *override.InteractionTimeout
	}
	if override.UserSessionTTL != nil {
		cfg.UserSessionTTL = *override.UserSessionTTL
	}
	if override.UserActivationTokenTTL != nil {
		cfg.UserActivationTokenTTL = *override.UserActivationTokenTTL
	}
	if override.UserActivationTokenMaxTTL != nil {
		cfg.UserActivationTokenMaxTTL = *override.UserActivationTokenMaxTTL
	}
	if override.OutputLimit != nil {
		cfg.OutputLimit = *override.OutputLimit
	}
	if override.RequireCommandApproval != nil {
		cfg.RequireCommandApproval = *override.RequireCommandApproval
	}
	if override.Debug != nil {
		cfg.Debug = *override.Debug
	}
	if override.RebuildInteractionHistory != nil {
		cfg.RebuildInteractionHistory = *override.RebuildInteractionHistory
	}
	return nil
}

func normalize(cfg *Config) {
	cfg.KnownHostsPath = expandHomePath(cfg.KnownHostsPath)
	cfg.InteractionHistoryDB = expandHomePath(cfg.InteractionHistoryDB)
	cfg.TLSCertificateFile = expandHomePath(strings.TrimSpace(cfg.TLSCertificateFile))
	cfg.TLSPrivateKeyFile = expandHomePath(strings.TrimSpace(cfg.TLSPrivateKeyFile))
	cfg.Scheme = strings.ToLower(strings.TrimSpace(cfg.Scheme))
	cfg.TLSMode = strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	cfg.TLSServerName = strings.TrimSpace(cfg.TLSServerName)
	cfg.PolicyDir = strings.TrimSpace(cfg.PolicyDir)
	cfg.InteractionConfigDir = strings.TrimSpace(cfg.InteractionConfigDir)
}

func expandHomePath(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

func validate(cfg Config) error {
	if cfg.Scheme != SchemeHTTPS && cfg.Scheme != SchemeHTTP {
		return fmt.Errorf("server scheme must be %q or %q", SchemeHTTPS, SchemeHTTP)
	}
	if cfg.TLSMode != TLSModeSelfSigned && cfg.TLSMode != TLSModeProvided {
		return fmt.Errorf("TLS mode must be %q or %q", TLSModeSelfSigned, TLSModeProvided)
	}
	if cfg.Scheme == SchemeHTTPS {
		if cfg.TLSCertificateFile == "" {
			return fmt.Errorf("TLS certificate file is required for HTTPS")
		}
		if cfg.TLSPrivateKeyFile == "" {
			return fmt.Errorf("TLS private key file is required for HTTPS")
		}
	}
	if cfg.CommandTimeout <= 0 {
		return fmt.Errorf("command timeout must be positive")
	}
	if cfg.InteractionTimeout <= 0 {
		return fmt.Errorf("interaction timeout must be positive")
	}
	if cfg.UserSessionTTL <= 0 {
		return fmt.Errorf("user session ttl must be positive")
	}
	if cfg.UserActivationTokenTTL <= 0 {
		return fmt.Errorf("user activation token ttl must be positive")
	}
	if cfg.UserActivationTokenMaxTTL <= 0 {
		return fmt.Errorf("user activation token max ttl must be positive")
	}
	if cfg.UserActivationTokenTTL > cfg.UserActivationTokenMaxTTL {
		return fmt.Errorf("user activation token ttl must not exceed maximum")
	}
	if cfg.OutputLimit <= 0 {
		return fmt.Errorf("output limit must be positive")
	}
	if strings.TrimSpace(cfg.PolicyDir) == "" {
		return fmt.Errorf("policy dir is required")
	}
	if strings.TrimSpace(cfg.InteractionConfigDir) == "" {
		return fmt.Errorf("interaction config dir is required")
	}
	if strings.TrimSpace(cfg.InteractionHistoryDB) == "" {
		return fmt.Errorf("interaction history db is required")
	}
	return nil
}

func defaultTargetDir() string {
	return DefaultTargetDir
}

func defaultKnownHostsPath() string {
	return filepath.Join("var", "proxyma", ".ssh", "known_hosts")
}
