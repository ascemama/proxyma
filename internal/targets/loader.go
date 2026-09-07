package targets

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"proxyma/internal/ids"
)

// LoadDir loads one JSON target object from each .json file in dir.
func LoadDir(dir string) (Registry, []Diagnostic, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Registry{}, nil, err
	}
	projectRoot := projectRootForTargetDir(dir)

	var diagnostics []Diagnostic
	var loaded []Target
	seenIDs := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		target, err := loadFile(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		if err := validate(&target, projectRoot, true); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		id := strings.TrimSpace(target.ID)
		if firstPath, ok := seenIDs[id]; ok {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("duplicate target id %q also used by %s", id, firstPath)})
			continue
		}
		seenIDs[id] = path
		loaded = append(loaded, target)
	}

	return NewRegistry(loaded), diagnostics, nil
}

func loadFile(path string) (Target, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) || err == fs.ErrNotExist {
			return Target{}, fmt.Errorf("target file not found")
		}
		return Target{}, err
	}
	var target Target
	if err := json.Unmarshal(data, &target); err != nil {
		return Target{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return target, nil
}

// ValidateForStorage validates a target before writing it to disk.
func ValidateForStorage(target *Target, projectRoot string) error {
	return validate(target, projectRoot, false)
}

func validate(target *Target, projectRoot string, resolvePrivateKey bool) error {
	switch {
	case target == nil:
		return fmt.Errorf("missing target")
	case strings.TrimSpace(target.ID) == "":
		return fmt.Errorf("missing required field id")
	case !ids.IsUUIDV4(strings.TrimSpace(target.ID)):
		return fmt.Errorf("id must be a UUID v4")
	case target.Transport != TransportSSH && target.Transport != TransportWinRM:
		if strings.TrimSpace(target.Transport) == "" {
			return fmt.Errorf("missing required field transport")
		}
		return fmt.Errorf("unsupported transport %q", target.Transport)
	case target.OS != OSLinux && target.OS != OSWindows:
		if strings.TrimSpace(target.OS) == "" {
			return fmt.Errorf("missing required field os")
		}
		return fmt.Errorf("unsupported os %q", target.OS)
	case strings.TrimSpace(target.Hostname) == "" && strings.TrimSpace(target.IP) == "":
		return fmt.Errorf("missing required target field hostname or ip")
	case strings.TrimSpace(target.Username) == "":
		return fmt.Errorf("missing required field username")
	case len(target.PolicyIDs) == 0:
		return fmt.Errorf("missing required field policy_ids")
	case len(target.AgentIDs) == 0:
		return fmt.Errorf("missing required field agent_ids")
	case len(target.UserIDs) == 0:
		return fmt.Errorf("missing required field user_ids")
	case target.Auth == nil:
		return fmt.Errorf("missing required field auth")
	case strings.TrimSpace(target.Auth.Type) == "":
		return fmt.Errorf("missing required field auth.type")
	}
	for i, policyID := range target.PolicyIDs {
		target.PolicyIDs[i] = strings.TrimSpace(policyID)
		if target.PolicyIDs[i] == "" {
			return fmt.Errorf("policy_ids must not contain empty values")
		}
	}
	seenAgentIDs := make(map[string]bool, len(target.AgentIDs))
	for i, agentID := range target.AgentIDs {
		target.AgentIDs[i] = strings.TrimSpace(agentID)
		if target.AgentIDs[i] == "" {
			return fmt.Errorf("agent_ids must not contain empty values")
		}
		if seenAgentIDs[target.AgentIDs[i]] {
			return fmt.Errorf("agent_ids must not contain duplicate values")
		}
		seenAgentIDs[target.AgentIDs[i]] = true
	}
	seenUserIDs := make(map[string]bool, len(target.UserIDs))
	for i, userID := range target.UserIDs {
		target.UserIDs[i] = strings.TrimSpace(userID)
		if target.UserIDs[i] == "" {
			return fmt.Errorf("user_ids must not contain empty values")
		}
		if seenUserIDs[target.UserIDs[i]] {
			return fmt.Errorf("user_ids must not contain duplicate values")
		}
		seenUserIDs[target.UserIDs[i]] = true
	}

	switch target.Auth.Type {
	case AuthTypePassword:
		if strings.TrimSpace(target.Auth.Password) == "" {
			return fmt.Errorf("missing required field auth.password")
		}
	case AuthTypePrivateKey:
		privateKeyPath, err := normalizePrivateKeyPath(target.Auth.PrivateKeyPath, projectRoot, resolvePrivateKey)
		if err != nil {
			return err
		}
		target.Auth.PrivateKeyPath = privateKeyPath
	default:
		return fmt.Errorf("unsupported auth type %q", target.Auth.Type)
	}

	switch target.Transport {
	case TransportSSH:
		if target.WinRM != nil {
			return fmt.Errorf("SSH targets must not define winrm")
		}
	case TransportWinRM:
		if target.OS != OSWindows {
			return fmt.Errorf("WinRM targets require os windows")
		}
		if target.Auth.Type != AuthTypePassword {
			return fmt.Errorf("WinRM targets require password authentication")
		}
		if target.WinRM == nil {
			return fmt.Errorf("missing required field winrm")
		}
		target.WinRM.Scheme = strings.ToLower(strings.TrimSpace(target.WinRM.Scheme))
		if target.WinRM.Scheme != "http" && target.WinRM.Scheme != "https" {
			return fmt.Errorf("winrm.scheme must be http or https")
		}
		if target.WinRM.Port < 0 || target.WinRM.Port > 65535 {
			return fmt.Errorf("winrm.port must be between 1 and 65535")
		}
		if target.WinRM.Port == 0 {
			if target.WinRM.Scheme == "https" {
				target.WinRM.Port = 5986
			} else {
				target.WinRM.Port = 5985
			}
		}
		if target.WinRM.TLSInsecure && target.WinRM.Scheme != "https" {
			return fmt.Errorf("winrm.tls_insecure is valid only for https")
		}
	}

	return nil
}

func normalizePrivateKeyPath(rawPath string, projectRoot string, resolvePrivateKey bool) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", fmt.Errorf("missing required field auth.private_key_path")
	}
	if filepath.IsAbs(trimmed) || path.IsAbs(strings.ReplaceAll(trimmed, "\\", "/")) {
		return "", fmt.Errorf("auth.private_key_path must be relative to %s", PrivateKeyDirectory)
	}

	slashPath := strings.ReplaceAll(trimmed, "\\", "/")
	cleaned := path.Clean(slashPath)
	requiredPrefix := PrivateKeyDirectory + "/"
	if cleaned == "." || !strings.HasPrefix(cleaned, requiredPrefix) {
		return "", fmt.Errorf("auth.private_key_path must be under %s", PrivateKeyDirectory)
	}

	root := filepath.Clean(projectRoot)
	keyPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(cleaned)))
	keyRoot := filepath.Clean(filepath.Join(root, filepath.FromSlash(PrivateKeyDirectory)))
	rel, err := filepath.Rel(keyRoot, keyPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("auth.private_key_path must be under %s", PrivateKeyDirectory)
	}
	if !resolvePrivateKey {
		return cleaned, nil
	}
	return keyPath, nil
}

// ProjectRootForDir returns the repository root used for target path validation.
func ProjectRootForDir(dir string) string {
	return projectRootForTargetDir(dir)
}

func projectRootForTargetDir(dir string) string {
	if root, ok := findProjectRoot(dir); ok {
		return root
	}
	// Installed Proxyma configurations do not include the source repository's
	// go.mod. In that layout, target records live in <config>/targets and
	// private keys live in <config>/secrets/private_keys. Do not fall back to
	// the process working directory: systemd normally starts services in /,
	// which would resolve keys to /secrets/private_keys instead.
	if strings.TrimSpace(dir) != "" {
		if absoluteDir, err := filepath.Abs(dir); err == nil {
			return filepath.Dir(filepath.Clean(absoluteDir))
		}
	}
	return mustGetwd()
}

func findProjectRoot(start string) (string, bool) {
	if strings.TrimSpace(start) == "" {
		return "", false
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(abs)
	if err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", false
		}
		abs = parent
	}
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
