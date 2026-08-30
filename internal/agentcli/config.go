package agentcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	SchemaVersion             string `json:"schema_version"`
	ProxymaBaseURL            string `json:"proxyma_base_url"`
	AgentConfig               string `json:"agent_config"`
	StateDir                  string `json:"state_dir"`
	HTTPTimeoutSeconds        int    `json:"http_timeout_seconds,omitempty"`
	LockTimeoutSeconds        int    `json:"lock_timeout_seconds,omitempty"`
	ActivePollIntervalSeconds int    `json:"active_poll_interval_seconds,omitempty"`
	Path                      string `json:"-"`
	AgentID                   string `json:"-"`
	SigningKey                string `json:"-"`
}

type agentConfig struct {
	AgentID            string `json:"agent_id"`
	ID                 string `json:"id"`
	AgentJWTSigningKey string `json:"agent_jwt_signing_key,omitempty"`
}

type configDocument struct {
	SchemaVersion             string `json:"schema_version"`
	ProxymaBaseURL            string `json:"proxyma_base_url"`
	AgentConfig               string `json:"agent_config"`
	StateDir                  string `json:"state_dir"`
	HTTPTimeoutSeconds        *int   `json:"http_timeout_seconds,omitempty"`
	LockTimeoutSeconds        *int   `json:"lock_timeout_seconds,omitempty"`
	ActivePollIntervalSeconds *int   `json:"active_poll_interval_seconds,omitempty"`
}

func LoadConfig(explicit string) (Config, error) {
	path, err := discoverConfig(explicit)
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read harness config: %w", err)
	}
	var document configDocument
	if err := decodeStrictJSON(data, &document); err != nil {
		return Config{}, fmt.Errorf("decode harness config: %w", err)
	}
	cfg := Config{
		SchemaVersion:  document.SchemaVersion,
		ProxymaBaseURL: document.ProxymaBaseURL,
		AgentConfig:    document.AgentConfig,
		StateDir:       document.StateDir,
	}
	if cfg.SchemaVersion != StateVersion {
		return Config{}, fmt.Errorf("unsupported harness config schema %q; expected %q", cfg.SchemaVersion, StateVersion)
	}
	parsed, err := url.Parse(strings.TrimSpace(cfg.ProxymaBaseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Config{}, fmt.Errorf("proxyma_base_url must be an absolute http or https URL")
	}
	if cfg.AgentConfig == "" || cfg.StateDir == "" {
		return Config{}, fmt.Errorf("agent_config and state_dir are required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, err
	}
	cfg.Path = filepath.Clean(absPath)
	base := filepath.Dir(cfg.Path)
	cfg.AgentConfig = resolveRelative(base, cfg.AgentConfig)
	cfg.StateDir = resolveRelative(base, cfg.StateDir)
	cfg.ProxymaBaseURL = strings.TrimRight(strings.TrimSpace(cfg.ProxymaBaseURL), "/")
	var durationErr error
	cfg.HTTPTimeoutSeconds, durationErr = positiveOrDefault(document.HTTPTimeoutSeconds, 15, "http_timeout_seconds")
	if durationErr != nil {
		return Config{}, durationErr
	}
	cfg.LockTimeoutSeconds, durationErr = positiveOrDefault(document.LockTimeoutSeconds, 5, "lock_timeout_seconds")
	if durationErr != nil {
		return Config{}, durationErr
	}
	cfg.ActivePollIntervalSeconds, durationErr = positiveOrDefault(document.ActivePollIntervalSeconds, 30, "active_poll_interval_seconds")
	if durationErr != nil {
		return Config{}, durationErr
	}
	credentials, err := os.ReadFile(cfg.AgentConfig)
	if err != nil {
		return Config{}, fmt.Errorf("read agent config: %w", err)
	}
	var agent agentConfig
	if err := json.Unmarshal(credentials, &agent); err != nil {
		return Config{}, fmt.Errorf("decode agent config: %w", err)
	}
	configuredAgentID := strings.TrimSpace(agent.AgentID)
	serverAgentID := strings.TrimSpace(agent.ID)
	if configuredAgentID != "" && serverAgentID != "" && configuredAgentID != serverAgentID {
		return Config{}, fmt.Errorf("agent config contains conflicting agent_id and id values")
	}
	if configuredAgentID == "" {
		configuredAgentID = serverAgentID
	}
	cfg.AgentID = configuredAgentID
	cfg.SigningKey = os.Getenv("PROXYMA_AGENT_JWT_SIGNING_KEY")
	if cfg.SigningKey == "" {
		cfg.SigningKey = agent.AgentJWTSigningKey
	}
	if cfg.AgentID == "" || cfg.SigningKey == "" {
		return Config{}, fmt.Errorf("agent config must provide agent_id and a signing key")
	}
	if _, err := uuid.Parse(cfg.AgentID); err != nil {
		return Config{}, fmt.Errorf("agent_id must be a UUID")
	}
	return cfg, nil
}

func positiveOrDefault(value *int, fallback int, name string) (int, error) {
	if value == nil {
		return fallback, nil
	}
	if *value < 1 {
		return 0, fmt.Errorf("%s must be at least 1", name)
	}
	return *value, nil
}

func discoverConfig(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if path := os.Getenv("PROXYMA_HARNESS_CONFIG"); path != "" {
		return path, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "harness", "proxyma-agent.json")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("harness/proxyma-agent.json was not found")
}

func resolveRelative(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, path))
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("expected exactly one JSON value")
		}
		return fmt.Errorf("expected exactly one JSON value: %w", err)
	}
	return nil
}

func (c Config) HTTPTimeout() time.Duration { return time.Duration(c.HTTPTimeoutSeconds) * time.Second }
func (c Config) LockTimeout() time.Duration { return time.Duration(c.LockTimeoutSeconds) * time.Second }
