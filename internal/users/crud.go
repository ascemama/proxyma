package users

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"proxyma/internal/agents"
	"proxyma/internal/ids"
	"proxyma/internal/store"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
)

// Paths identifies the JSON directories managed by Proxyma configuration CRUD.
type Paths struct {
	UserDir              string
	CredentialTokenDir   string
	AgentDir             string
	TargetDir            string
	PolicyDir            string
	InteractionConfigDir string
}

// IDGenerator generates an opaque Proxyma resource ID.
type IDGenerator func() (string, error)

// MutationResult describes a completed management operation.
type MutationResult struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	ID       string `json:"id"`
	Path     string `json:"path"`
}

// CreateUser writes a new user with a generated UUID.
func CreateUser(paths Paths, user User, generateID IDGenerator) (MutationResult, error) {
	if strings.TrimSpace(user.ID) != "" {
		return MutationResult{}, fmt.Errorf("create user input must not include id")
	}
	existing, err := loadUsers(paths)
	if err != nil {
		return MutationResult{}, err
	}
	if _, found := existing.FindByUsername(user.Username); found {
		return MutationResult{}, fmt.Errorf("username %q already exists; keep the existing user or remove it before creating another one", NormalizeUsername(user.Username))
	}
	id, err := newID(generateID)
	if err != nil {
		return MutationResult{}, err
	}
	user.ID = id
	if user.Status == "" && user.PasswdHash != "" {
		user.Status = StatusActive
	}
	if err := validate(user); err != nil {
		return MutationResult{}, err
	}
	users := store.New(paths.UserDir)
	if exists, err := recordExists(users, id); err != nil || exists {
		return MutationResult{}, existsError("user", id, err)
	}
	if err := users.WriteJSON(id, user); err != nil {
		return MutationResult{}, err
	}
	return result("users", "create", id, users.RecordPath(id)), nil
}

// UpdateUser replaces an existing user without changing its ID.
func UpdateUser(paths Paths, id string, user User) (MutationResult, error) {
	id = strings.TrimSpace(id)
	if strings.TrimSpace(user.ID) != "" {
		return MutationResult{}, fmt.Errorf("update user input must not include id")
	}
	users := store.New(paths.UserDir)
	existing, recordName, err := findUserRecord(users, id)
	if err != nil {
		return MutationResult{}, err
	}
	user.ID = id
	user.Status = existing.Status
	if strings.TrimSpace(user.PasswdHash) == "" {
		user.PasswdHash = existing.PasswdHash
	}
	if strings.TrimSpace(user.Theme) == "" {
		user.Theme = existing.Theme
	}
	if err := validate(user); err != nil {
		return MutationResult{}, err
	}
	if err := users.WriteJSON(recordName, user); err != nil {
		return MutationResult{}, err
	}
	return result("users", "update", id, users.RecordPath(recordName)), nil
}

// UpdateUserTheme changes only a user's persisted appearance preference.
func UpdateUserTheme(paths Paths, id string, theme string) (MutationResult, error) {
	id = strings.TrimSpace(id)
	theme = strings.TrimSpace(theme)
	if theme != ThemeLight && theme != ThemeDark {
		return MutationResult{}, fmt.Errorf("theme must be light or dark")
	}
	users := store.New(paths.UserDir)
	existing, recordName, err := findUserRecord(users, id)
	if err != nil {
		return MutationResult{}, err
	}
	existing.Theme = theme
	if err := validate(existing); err != nil {
		return MutationResult{}, err
	}
	if err := users.WriteJSON(recordName, existing); err != nil {
		return MutationResult{}, err
	}
	return result("users", "update-theme", id, users.RecordPath(recordName)), nil
}

// ResetUserPassword replaces an existing user's password hash.
func ResetUserPassword(paths Paths, id string, password []byte) (MutationResult, error) {
	id = strings.TrimSpace(id)
	users := store.New(paths.UserDir)
	existing, recordName, err := findUserRecord(users, id)
	if err != nil {
		return MutationResult{}, err
	}
	if existing.Status != StatusActive {
		return MutationResult{}, fmt.Errorf("password can only be reset for an active user")
	}
	updated, err := ResetPassword(existing, password)
	if err != nil {
		return MutationResult{}, err
	}
	if err := validate(updated); err != nil {
		return MutationResult{}, err
	}
	if err := users.WriteJSON(recordName, updated); err != nil {
		return MutationResult{}, err
	}
	return result("users", "reset-password", id, users.RecordPath(recordName)), nil
}

func findUserRecord(users *store.FileStore, id string) (User, string, error) {
	paths, err := users.ListJSONFiles()
	if err != nil {
		return User{}, "", err
	}
	for _, path := range paths {
		var user User
		if err := users.ReadJSONFile(path, &user); err != nil {
			return User{}, "", err
		}
		if strings.TrimSpace(user.ID) == id {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			return user, name, nil
		}
	}
	return User{}, "", missingError("user", id, os.ErrNotExist)
}

// DeleteUser removes an unreferenced user.
func DeleteUser(paths Paths, id string) (MutationResult, error) {
	id = strings.TrimSpace(id)
	users := store.New(paths.UserDir)
	var existing User
	if err := users.ReadJSON(id, &existing); err != nil {
		return MutationResult{}, missingError("user", id, err)
	}
	agentRegistry, err := loadAgents(paths)
	if err != nil {
		return MutationResult{}, err
	}
	for _, agent := range agentRegistry.All() {
		if contains(agent.UserIDs, id) {
			return MutationResult{}, fmt.Errorf("user %q is referenced by agent %q", id, agent.ID)
		}
	}
	targetRegistry, err := loadTargets(paths)
	if err != nil {
		return MutationResult{}, err
	}
	for _, target := range targetRegistry.All() {
		if contains(target.UserIDs, id) {
			return MutationResult{}, fmt.Errorf("user %q is referenced by target %q", id, target.ID)
		}
	}
	if err := users.DeleteJSON(id); err != nil {
		return MutationResult{}, err
	}
	if strings.TrimSpace(paths.CredentialTokenDir) != "" {
		if err := store.New(paths.CredentialTokenDir).DeleteJSON(id); err != nil {
			return MutationResult{}, err
		}
	}
	return result("users", "delete", id, users.RecordPath(id)), nil
}

// CreateAgent writes a new agent with a generated UUID.
func CreateAgent(paths Paths, agent agents.Agent, generateID IDGenerator) (MutationResult, error) {
	if strings.TrimSpace(agent.ID) != "" {
		return MutationResult{}, fmt.Errorf("create agent input must not include id")
	}
	id, err := newID(generateID)
	if err != nil {
		return MutationResult{}, err
	}
	agent.ID = id
	if err := validateAgent(paths, agent); err != nil {
		return MutationResult{}, err
	}
	agentsStore := store.New(paths.AgentDir)
	if exists, err := recordExists(agentsStore, id); err != nil || exists {
		return MutationResult{}, existsError("agent", id, err)
	}
	if err := agentsStore.WriteJSON(id, agent); err != nil {
		return MutationResult{}, err
	}
	return result("agents", "create", id, agentsStore.RecordPath(id)), nil
}

// UpdateAgent replaces an existing agent without changing its ID.
func UpdateAgent(paths Paths, id string, agent agents.Agent) (MutationResult, error) {
	id = strings.TrimSpace(id)
	if strings.TrimSpace(agent.ID) != "" {
		return MutationResult{}, fmt.Errorf("update agent input must not include id")
	}
	agentsStore := store.New(paths.AgentDir)
	var existing agents.Agent
	if err := agentsStore.ReadJSON(id, &existing); err != nil {
		return MutationResult{}, missingError("agent", id, err)
	}
	agent.ID = id
	if err := validateAgent(paths, agent); err != nil {
		return MutationResult{}, err
	}
	if err := agentsStore.WriteJSON(id, agent); err != nil {
		return MutationResult{}, err
	}
	return result("agents", "update", id, agentsStore.RecordPath(id)), nil
}

// DeleteAgent removes an unreferenced agent.
func DeleteAgent(paths Paths, id string) (MutationResult, error) {
	id = strings.TrimSpace(id)
	agentsStore := store.New(paths.AgentDir)
	var existing agents.Agent
	if err := agentsStore.ReadJSON(id, &existing); err != nil {
		return MutationResult{}, missingError("agent", id, err)
	}
	targetRegistry, err := loadTargets(paths)
	if err != nil {
		return MutationResult{}, err
	}
	for _, target := range targetRegistry.All() {
		if contains(target.AgentIDs, id) {
			return MutationResult{}, fmt.Errorf("agent %q is referenced by target %q", id, target.ID)
		}
	}
	if err := agentsStore.DeleteJSON(id); err != nil {
		return MutationResult{}, err
	}
	return result("agents", "delete", id, agentsStore.RecordPath(id)), nil
}

// CreateTarget writes a new target with a generated UUID.
func CreateTarget(paths Paths, target targets.Target, generateID IDGenerator) (MutationResult, error) {
	if strings.TrimSpace(target.ID) != "" {
		return MutationResult{}, fmt.Errorf("create target input must not include id")
	}
	id, err := newID(generateID)
	if err != nil {
		return MutationResult{}, err
	}
	target.ID = id
	if err := validateTarget(paths, &target); err != nil {
		return MutationResult{}, err
	}
	targetsStore := store.New(paths.TargetDir)
	if exists, err := recordExists(targetsStore, id); err != nil || exists {
		return MutationResult{}, existsError("target", id, err)
	}
	if err := targetsStore.WriteJSON(id, target); err != nil {
		return MutationResult{}, err
	}
	return result("targets", "create", id, targetsStore.RecordPath(id)), nil
}

// UpdateTarget replaces an existing target without changing its ID.
func UpdateTarget(paths Paths, id string, target targets.Target) (MutationResult, error) {
	id = strings.TrimSpace(id)
	if strings.TrimSpace(target.ID) != "" {
		return MutationResult{}, fmt.Errorf("update target input must not include id")
	}
	targetsStore := store.New(paths.TargetDir)
	var existing targets.Target
	if err := targetsStore.ReadJSON(id, &existing); err != nil {
		return MutationResult{}, missingError("target", id, err)
	}
	target.ID = id
	if err := validateTarget(paths, &target); err != nil {
		return MutationResult{}, err
	}
	if err := targetsStore.WriteJSON(id, target); err != nil {
		return MutationResult{}, err
	}
	return result("targets", "update", id, targetsStore.RecordPath(id)), nil
}

// DeleteTarget removes a target.
func DeleteTarget(paths Paths, id string) (MutationResult, error) {
	id = strings.TrimSpace(id)
	targetsStore := store.New(paths.TargetDir)
	var existing targets.Target
	if err := targetsStore.ReadJSON(id, &existing); err != nil {
		return MutationResult{}, missingError("target", id, err)
	}
	if err := targetsStore.DeleteJSON(id); err != nil {
		return MutationResult{}, err
	}
	return result("targets", "delete", id, targetsStore.RecordPath(id)), nil
}

// ReadPolicies returns all configured command policies.
func ReadPolicies(paths Paths) ([]policy.Policy, error) {
	policies, err := loadPolicies(paths)
	if err != nil {
		return nil, err
	}
	return policies.All(), nil
}

// CreatePolicy writes a new command policy with a generated UUID.
func CreatePolicy(paths Paths, item policy.Policy, generateID IDGenerator) (MutationResult, error) {
	if strings.TrimSpace(item.ID) != "" {
		return MutationResult{}, fmt.Errorf("create policy input must not include id")
	}
	id, err := newID(generateID)
	if err != nil {
		return MutationResult{}, err
	}
	item.ID = id
	if err := policy.Validate(&item); err != nil {
		return MutationResult{}, err
	}
	policiesStore := store.New(paths.PolicyDir)
	if exists, err := recordExists(policiesStore, id); err != nil || exists {
		return MutationResult{}, existsError("policy", id, err)
	}
	if err := policiesStore.WriteJSON(id, item); err != nil {
		return MutationResult{}, err
	}
	return result("policies", "create", id, policiesStore.RecordPath(id)), nil
}

// DeletePolicy removes an unreferenced command policy.
func DeletePolicy(paths Paths, id string) (MutationResult, error) {
	id = strings.TrimSpace(id)
	if !ids.IsUUIDV4(id) {
		return MutationResult{}, fmt.Errorf("policy id must be a UUID v4")
	}
	targetRegistry, err := loadTargets(paths)
	if err != nil {
		return MutationResult{}, err
	}
	for _, target := range targetRegistry.All() {
		if target.PolicyID == id {
			return MutationResult{}, fmt.Errorf("policy %q is referenced by target %q", id, target.ID)
		}
	}
	path, err := policyPathByID(paths.PolicyDir, id)
	if err != nil {
		return MutationResult{}, err
	}
	if err := os.Remove(path); err != nil {
		return MutationResult{}, missingError("policy", id, err)
	}
	return result("policies", "delete", id, path), nil
}

// ReadAgentsForUser returns all agents owned by the supplied user ID.
func ReadAgentsForUser(paths Paths, userID string) ([]agents.Agent, error) {
	userID = strings.TrimSpace(userID)
	agentRegistry, err := loadAgents(paths)
	if err != nil {
		return nil, err
	}
	var owned []agents.Agent
	for _, agent := range agentRegistry.All() {
		if contains(agent.UserIDs, userID) {
			agent.UserIDs = append([]string(nil), agent.UserIDs...)
			owned = append(owned, agent)
		}
	}
	return owned, nil
}

// ReadTargetsForUser returns all targets owned by the supplied user ID.
func ReadTargetsForUser(paths Paths, userID string) ([]targets.Target, error) {
	userID = strings.TrimSpace(userID)
	targetRegistry, err := loadTargets(paths)
	if err != nil {
		return nil, err
	}
	var owned []targets.Target
	for _, target := range targetRegistry.All() {
		if contains(target.UserIDs, userID) {
			owned = append(owned, target)
		}
	}
	return owned, nil
}

func policyPathByID(dir string, id string) (string, error) {
	policiesStore := store.New(dir)
	paths, err := policiesStore.ListJSONFiles()
	if err != nil {
		return "", err
	}
	for _, path := range paths {
		var item policy.Policy
		if err := policiesStore.ReadJSONFile(path, &item); err != nil {
			return "", err
		}
		if strings.TrimSpace(item.ID) == id {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func newID(generateID IDGenerator) (string, error) {
	if generateID == nil {
		generateID = ids.NewUUIDV4
	}
	id, err := generateID()
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if !ids.IsUUIDV4(id) {
		return "", fmt.Errorf("generated id must be a UUID v4")
	}
	return id, nil
}

func validateAgent(paths Paths, agent agents.Agent) error {
	if err := agents.Validate(agent); err != nil {
		return err
	}
	userRegistry, err := loadUsers(paths)
	if err != nil {
		return err
	}
	for _, userID := range agent.UserIDs {
		if _, found := userRegistry.Find(userID); !found {
			return fmt.Errorf("unknown user id %q", userID)
		}
	}
	return nil
}

func validateTarget(paths Paths, target *targets.Target) error {
	if err := targets.ValidateForStorage(target, targets.ProjectRootForDir(paths.TargetDir)); err != nil {
		return err
	}
	policies, err := loadPolicies(paths)
	if err != nil {
		return err
	}
	if diagnostics := policies.ValidateReference(target.ID, target.PolicyID); len(diagnostics) > 0 {
		return diagnosticError("target policy", diagnostics)
	}
	agentRegistry, err := loadAgents(paths)
	if err != nil {
		return err
	}
	for _, agentID := range target.AgentIDs {
		if _, found := agentRegistry.Find(agentID); !found {
			return fmt.Errorf("unknown agent id %q", agentID)
		}
	}
	userRegistry, err := loadUsers(paths)
	if err != nil {
		return err
	}
	for _, userID := range target.UserIDs {
		if _, found := userRegistry.Find(userID); !found {
			return fmt.Errorf("unknown user id %q", userID)
		}
	}
	return nil
}

func loadUsers(paths Paths) (Registry, error) {
	registry, diagnostics, err := LoadDir(paths.UserDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewRegistry(nil), nil
		}
		return Registry{}, err
	}
	if len(diagnostics) > 0 {
		return Registry{}, diagnosticError("users", diagnostics)
	}
	return registry, nil
}

func loadAgents(paths Paths) (agents.Registry, error) {
	registry, diagnostics, err := agents.LoadDir(paths.AgentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agents.NewRegistry(nil), nil
		}
		return agents.Registry{}, err
	}
	if len(diagnostics) > 0 {
		return agents.Registry{}, diagnosticError("agents", diagnostics)
	}
	return registry, nil
}

func loadTargets(paths Paths) (targets.Registry, error) {
	registry, diagnostics, err := targets.LoadDir(paths.TargetDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return targets.NewRegistry(nil), nil
		}
		return targets.Registry{}, err
	}
	if len(diagnostics) > 0 {
		return targets.Registry{}, diagnosticError("targets", diagnostics)
	}
	return registry, nil
}

func loadPolicies(paths Paths) (*policy.Set, error) {
	policies, diagnostics, err := policy.LoadDir(paths.PolicyDir)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) > 0 {
		return nil, diagnosticError("policies", diagnostics)
	}
	return policies, nil
}

func recordExists(store *store.FileStore, id string) (bool, error) {
	_, err := os.Stat(store.RecordPath(id))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func existsError(resource string, id string, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s %q already exists", resource, id)
}

func missingError(resource string, id string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s %q does not exist", resource, id)
	}
	return err
}

func diagnosticError(label string, diagnostics interface{}) error {
	switch items := diagnostics.(type) {
	case []Diagnostic:
		if len(items) == 0 {
			return nil
		}
		return fmt.Errorf("%s invalid: %s: %s", label, items[0].Path, items[0].Message)
	case []agents.Diagnostic:
		if len(items) == 0 {
			return nil
		}
		return fmt.Errorf("%s invalid: %s: %s", label, items[0].Path, items[0].Message)
	case []targets.Diagnostic:
		if len(items) == 0 {
			return nil
		}
		return fmt.Errorf("%s invalid: %s: %s", label, items[0].Path, items[0].Message)
	case []policy.Diagnostic:
		if len(items) == 0 {
			return nil
		}
		return fmt.Errorf("%s invalid: %s: %s", label, items[0].Path, items[0].Message)
	default:
		return fmt.Errorf("%s invalid", label)
	}
}

func result(resource string, action string, id string, path string) MutationResult {
	return MutationResult{Resource: resource, Action: action, ID: id, Path: path}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}
