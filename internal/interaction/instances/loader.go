package instances

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"proxyma/internal/ids"
	"proxyma/internal/interaction"
	interactionntfy "proxyma/internal/interaction/ntfy"
)

const ProviderNtfy = "ntfy"

// Diagnostic describes a rejected interaction instance file.
type Diagnostic struct {
	Path    string
	Message string
}

// NtfyInstance describes one configured ntfy interaction instance.
type NtfyInstance struct {
	Schema        string        `json:"$schema,omitempty"`
	ID            string        `json:"id"`
	CreatorUserID string        `json:"creator_user_id"`
	DisplayName   string        `json:"display_name,omitempty"`
	Enabled       bool          `json:"enabled"`
	BaseURL       string        `json:"base_url"`
	RequestTopic  string        `json:"request_topic"`
	ResponseTopic string        `json:"response_topic"`
	Token         string        `json:"token,omitempty"`
	PollInterval  time.Duration `json:"-"`
	PollTimeout   time.Duration `json:"-"`
	RespondedBy   string        `json:"responded_by,omitempty"`
}

// InstanceMetadata contains provider-neutral interaction instance attributes.
type InstanceMetadata struct {
	ID            string
	ProviderType  string
	CreatorUserID string
	DisplayName   string
	Enabled       bool
}

// InstanceDefinition exposes common metadata for a configured interaction instance.
type InstanceDefinition interface {
	Metadata() InstanceMetadata
}

// Metadata returns the provider-neutral attributes of an ntfy instance.
func (n NtfyInstance) Metadata() InstanceMetadata {
	return InstanceMetadata{
		ID:            n.ID,
		ProviderType:  ProviderNtfy,
		CreatorUserID: n.CreatorUserID,
		DisplayName:   n.DisplayName,
		Enabled:       n.Enabled,
	}
}

type ntfyFile struct {
	Schema        string `json:"$schema,omitempty"`
	ID            string `json:"id"`
	CreatorUserID string `json:"creator_user_id"`
	DisplayName   string `json:"display_name,omitempty"`
	Enabled       bool   `json:"enabled"`
	BaseURL       string `json:"base_url"`
	RequestTopic  string `json:"request_topic"`
	ResponseTopic string `json:"response_topic"`
	Token         string `json:"token,omitempty"`
	PollInterval  string `json:"poll_interval"`
	PollTimeout   string `json:"poll_timeout"`
	RespondedBy   string `json:"responded_by,omitempty"`
}

// Registry stores configured interaction instance definitions by id.
type Registry struct {
	enabled map[string]InstanceDefinition
	all     map[string]InstanceDefinition
}

// LoadDir loads interaction instance definitions from provider subdirectories.
func LoadDir(dir string) (Registry, []Diagnostic, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) || err == fs.ErrNotExist {
			return Registry{}, nil, nil
		}
		return Registry{}, nil, err
	}

	registry := Registry{
		enabled: make(map[string]InstanceDefinition),
		all:     make(map[string]InstanceDefinition),
	}
	seenIDs := map[string]string{interaction.DefaultInstanceLocal: "built-in local interaction instance"}
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		providerDir := filepath.Join(dir, entry.Name())
		switch strings.ToLower(strings.TrimSpace(entry.Name())) {
		case ProviderNtfy:
			loaded, ntfyDiagnostics := loadNtfyDir(providerDir, seenIDs)
			diagnostics = append(diagnostics, ntfyDiagnostics...)
			for _, instance := range loaded {
				registry.all[instance.ID] = instance
				if instance.Enabled {
					registry.enabled[instance.ID] = instance
				}
			}
		case interaction.DefaultInstanceLocal:
			diagnostics = append(diagnostics, Diagnostic{Path: providerDir, Message: "local interaction configuration is built in and must not use an interactions/local directory"})
		default:
			diagnostics = append(diagnostics, Diagnostic{Path: providerDir, Message: fmt.Sprintf("unsupported interaction provider directory %q", entry.Name())})
		}
	}
	return registry, diagnostics, nil
}

// HasEnabled reports whether id references the built-in local instance or a loaded enabled instance.
func (r Registry) HasEnabled(id string) bool {
	id = NormalizeInstanceID(id)
	if id == interaction.DefaultInstanceLocal {
		return true
	}
	_, ok := r.enabled[id]
	return ok
}

// Enabled returns all enabled file-backed interaction instance definitions.
func (r Registry) Enabled() []InstanceDefinition {
	items := make([]InstanceDefinition, 0, len(r.enabled))
	for _, definition := range r.enabled {
		items = append(items, definition)
	}
	return items
}

// All returns all file-backed interaction instance definitions, including disabled instances.
func (r Registry) All() []InstanceDefinition {
	items := make([]InstanceDefinition, 0, len(r.all))
	for _, definition := range r.all {
		items = append(items, definition)
	}
	return items
}

// NormalizeInstanceID returns the canonical interaction instance id.
func NormalizeInstanceID(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return interaction.DefaultInstanceLocal
	}
	return trimmed
}

func loadNtfyDir(dir string, seenIDs map[string]string) ([]NtfyInstance, []Diagnostic) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []Diagnostic{{Path: dir, Message: err.Error()}}
	}
	var loaded []NtfyInstance
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		instance, err := loadNtfyFile(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		if err := validateNtfy(instance); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		id := NormalizeInstanceID(instance.ID)
		if firstPath, ok := seenIDs[id]; ok {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("duplicate interaction instance id %q also used by %s", id, firstPath)})
			continue
		}
		instance.ID = id
		seenIDs[id] = path
		loaded = append(loaded, instance)
	}
	return loaded, diagnostics
}

func loadNtfyFile(path string) (NtfyInstance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return NtfyInstance{}, err
	}
	var raw ntfyFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return NtfyInstance{}, fmt.Errorf("invalid JSON: %w", err)
	}
	pollInterval, err := time.ParseDuration(raw.PollInterval)
	if err != nil {
		return NtfyInstance{}, fmt.Errorf("poll_interval must be a Go duration: %w", err)
	}
	pollTimeout, err := time.ParseDuration(raw.PollTimeout)
	if err != nil {
		return NtfyInstance{}, fmt.Errorf("poll_timeout must be a Go duration: %w", err)
	}
	return NtfyInstance{
		Schema:        raw.Schema,
		ID:            strings.TrimSpace(raw.ID),
		CreatorUserID: strings.TrimSpace(raw.CreatorUserID),
		DisplayName:   strings.TrimSpace(raw.DisplayName),
		Enabled:       raw.Enabled,
		BaseURL:       strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/"),
		RequestTopic:  strings.TrimSpace(raw.RequestTopic),
		ResponseTopic: strings.TrimSpace(raw.ResponseTopic),
		Token:         strings.TrimSpace(raw.Token),
		PollInterval:  pollInterval,
		PollTimeout:   pollTimeout,
		RespondedBy:   strings.TrimSpace(raw.RespondedBy),
	}, nil
}

func validateNtfy(instance NtfyInstance) error {
	switch {
	case strings.TrimSpace(instance.ID) == "":
		return fmt.Errorf("missing required field id")
	case instance.ID == interaction.DefaultInstanceLocal:
		return fmt.Errorf("interaction instance id %q is reserved for the built-in local provider", interaction.DefaultInstanceLocal)
	case !ids.IsUUIDV4(instance.ID):
		return fmt.Errorf("id must be a UUID v4")
	case strings.TrimSpace(instance.CreatorUserID) == "":
		return fmt.Errorf("missing required field creator_user_id")
	case !ids.IsUUIDV4(instance.CreatorUserID):
		return fmt.Errorf("creator_user_id must be a UUID v4")
	case strings.TrimSpace(instance.BaseURL) == "":
		return fmt.Errorf("ntfy base URL is required")
	case strings.TrimSpace(instance.RequestTopic) == "":
		return fmt.Errorf("ntfy request topic is required")
	case strings.TrimSpace(instance.ResponseTopic) == "":
		return fmt.Errorf("ntfy response topic is required")
	case instance.PollInterval <= 0:
		return fmt.Errorf("ntfy poll interval must be positive")
	case instance.PollTimeout <= 0:
		return fmt.Errorf("ntfy poll timeout must be positive")
	}
	parsed, err := url.Parse(instance.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("ntfy base URL must be an absolute HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("ntfy base URL must use http or https")
	}
	return nil
}

// NtfyOptions converts this definition to provider options.
func (n NtfyInstance) NtfyOptions() interactionntfy.Options {
	return interactionntfy.Options{
		BaseURL:       n.BaseURL,
		RequestTopic:  n.RequestTopic,
		ResponseTopic: n.ResponseTopic,
		Token:         n.Token,
		PollInterval:  n.PollInterval,
		PollTimeout:   n.PollTimeout,
		RespondedBy:   n.RespondedBy,
	}
}
