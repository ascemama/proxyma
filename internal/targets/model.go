// Package targets loads and validates target definitions.
package targets

import "strings"

// Target represents one managed target system.
type Target struct {
	Schema      string      `json:"$schema,omitempty"`
	ID          string      `json:"id"`
	Transport   string      `json:"transport"`
	OS          string      `json:"os"`
	Hostname    string      `json:"hostname,omitempty"`
	IP          string      `json:"ip,omitempty"`
	Username    string      `json:"username"`
	PolicyIDs   []string    `json:"policy_ids"`
	AgentIDs    []string    `json:"agent_ids"`
	UserIDs     []string    `json:"user_ids"`
	Auth        *TargetAuth `json:"auth"`
	WinRM       *WinRM      `json:"winrm,omitempty"`
	Description string      `json:"description,omitempty"`
}

const (
	TransportSSH       = "ssh"
	TransportWinRM     = "winrm"
	OSLinux            = "linux"
	OSWindows          = "windows"
	AuthTypePassword   = "password"
	AuthTypePrivateKey = "private_key"
)

// WinRM describes the endpoint used for Windows PowerShell remoting.
type WinRM struct {
	Scheme      string `json:"scheme"`
	Port        int    `json:"port,omitempty"`
	TLSInsecure bool   `json:"tls_insecure,omitempty"`
}

const PrivateKeyDirectory = "secrets/private_keys"

// TargetAuth describes how Proxyma authenticates to a managed target.
type TargetAuth struct {
	Type           string `json:"type"`
	Password       string `json:"password,omitempty"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
}

// PasswordAuth returns an auth object for password-auth targets.
func PasswordAuth(password string) *TargetAuth {
	return &TargetAuth{Type: AuthTypePassword, Password: password}
}

// PrivateKeyAuth returns an auth object for private-key-auth targets.
func PrivateKeyAuth(privateKeyPath string) *TargetAuth {
	return &TargetAuth{Type: AuthTypePrivateKey, PrivateKeyPath: privateKeyPath}
}

// PasswordCredential returns the password for password-auth targets.
func (t Target) PasswordCredential() (string, bool) {
	if t.Auth == nil || t.Auth.Type != AuthTypePassword {
		return "", false
	}
	return t.Auth.Password, strings.TrimSpace(t.Auth.Password) != ""
}

// PrivateKeyCredential returns the private key path for private-key-auth targets.
func (t Target) PrivateKeyCredential() (string, bool) {
	if t.Auth == nil || t.Auth.Type != AuthTypePrivateKey {
		return "", false
	}
	return t.Auth.PrivateKeyPath, strings.TrimSpace(t.Auth.PrivateKeyPath) != ""
}

// Diagnostic describes a rejected target file.
type Diagnostic struct {
	Path    string
	Message string
}

// Registry stores valid targets for hostname/IP lookup.
type Registry struct {
	targets []Target
}

// NewRegistry returns a registry containing the provided targets.
func NewRegistry(items []Target) Registry {
	cloned := make([]Target, len(items))
	for i, item := range items {
		cloned[i] = cloneTarget(item)
	}
	return Registry{targets: cloned}
}

// All returns a copy of all loaded targets.
func (r Registry) All() []Target {
	out := make([]Target, len(r.targets))
	for i, target := range r.targets {
		out[i] = cloneTarget(target)
	}
	return out
}

// MatchTarget finds exactly one managed target for a hostname or IP.
func (r Registry) MatchTarget(target string) (Target, bool, bool) {
	return r.matchTarget(target, "")
}

// MatchTargetForAgent finds exactly one managed target for a hostname or IP
// after narrowing candidates to targets authorized for agentID.
func (r Registry) MatchTargetForAgent(target string, agentID string) (Target, bool, bool) {
	return r.matchTarget(target, strings.TrimSpace(agentID))
}

func (r Registry) matchTarget(target string, agentID string) (Target, bool, bool) {
	needle := normalizeTarget(target)
	if needle == "" {
		return Target{}, false, false
	}

	var matches []Target
	for _, candidate := range r.targets {
		if agentID != "" && !candidate.hasAgent(agentID) {
			continue
		}
		if candidate.matchesTarget(needle) {
			matches = append(matches, candidate)
		}
	}

	switch len(matches) {
	case 0:
		return Target{}, false, false
	case 1:
		return cloneTarget(matches[0]), true, false
	default:
		return Target{}, true, true
	}
}

func (t Target) hasAgent(agentID string) bool {
	agentID = strings.TrimSpace(agentID)
	for _, candidate := range t.AgentIDs {
		if strings.TrimSpace(candidate) == agentID {
			return true
		}
	}
	return false
}

func cloneTarget(target Target) Target {
	target.PolicyIDs = append([]string(nil), target.PolicyIDs...)
	target.AgentIDs = append([]string(nil), target.AgentIDs...)
	target.UserIDs = append([]string(nil), target.UserIDs...)
	if target.Auth != nil {
		auth := *target.Auth
		target.Auth = &auth
	}
	if target.WinRM != nil {
		winrm := *target.WinRM
		target.WinRM = &winrm
	}
	return target
}

// MatchesTarget reports whether the target owns the provided hostname or IP.
func (t Target) MatchesTarget(target string) bool {
	return t.matchesTarget(normalizeTarget(target))
}

func (t Target) matchesTarget(target string) bool {
	if target == "" {
		return false
	}
	return normalizeTarget(t.Hostname) == target || normalizeTarget(t.IP) == target
}

func normalizeTarget(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
