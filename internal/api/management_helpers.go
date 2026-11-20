package api

import (
	"net/http"
	"proxyma/internal/agents"
	"proxyma/internal/llm"
	"proxyma/internal/store"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/users"
	"strings"
)

const maxLoginBodyBytes = 16 << 10
const maxUserBodyBytes = 16 << 10
const maxAgentBodyBytes = 16 << 10
const maxTargetBodyBytes = 64 << 10
const maxInteractionBodyBytes = 16 << 10
const maxPolicyBodyBytes = 64 << 10

func readStoredUser(paths users.Paths, id string) (users.User, error) {
	var user users.User
	if err := store.New(paths.UserDir).ReadJSON(strings.TrimSpace(id), &user); err != nil {
		return users.User{}, err
	}
	return user, nil
}

func readStoredAgent(paths users.Paths, id string) (agents.Agent, error) {
	var agent agents.Agent
	if err := store.New(paths.AgentDir).ReadJSON(strings.TrimSpace(id), &agent); err != nil {
		return agents.Agent{}, err
	}
	return agent, nil
}

func readStoredTarget(paths users.Paths, id string) (targets.Target, error) {
	var target targets.Target
	if err := store.New(paths.TargetDir).ReadJSON(strings.TrimSpace(id), &target); err != nil {
		return targets.Target{}, err
	}
	return target, nil
}

func (s *Server) managementPaths() (users.Paths, bool) {
	paths := s.ManagementPaths
	if strings.TrimSpace(paths.UserDir) == "" || strings.TrimSpace(paths.AgentDir) == "" || strings.TrimSpace(paths.TargetDir) == "" || strings.TrimSpace(paths.PolicyDir) == "" {
		return users.Paths{}, false
	}
	return paths, true
}

func (s *Server) refreshUserRegistry(paths users.Paths) error {
	registry, diagnostics, err := users.LoadDir(paths.UserDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("users invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	s.setUserRegistry(registry)
	return nil
}

func (s *Server) userRegistry() users.Registry {
	s.usersMu.RLock()
	defer s.usersMu.RUnlock()
	return s.Users
}

func (s *Server) setUserRegistry(registry users.Registry) {
	s.usersMu.Lock()
	defer s.usersMu.Unlock()
	s.Users = registry
}

func (s *Server) replaceUserInRegistry(updated users.User) {
	items := s.userRegistry().All()
	for index := range items {
		if items[index].ID == updated.ID {
			items[index] = updated
			break
		}
	}
	s.setUserRegistry(users.NewRegistry(items))
}

func (s *Server) refreshAgentRegistry(paths users.Paths) error {
	registry, diagnostics, err := agents.LoadDir(paths.AgentDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("agents invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	s.setAgentRegistry(registry)
	return nil
}

func (s *Server) agentRegistry() agents.Registry {
	s.agentsMu.RLock()
	defer s.agentsMu.RUnlock()
	return s.Agents
}

func (s *Server) setAgentRegistry(registry agents.Registry) {
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()
	s.Agents = registry
}

func (s *Server) refreshTargetRegistry(paths users.Paths) error {
	registry, diagnostics, err := targets.LoadDir(paths.TargetDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("targets invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	s.setTargetRegistry(registry)
	return nil
}

func (s *Server) targetRegistry() targets.Registry {
	s.targetsMu.RLock()
	defer s.targetsMu.RUnlock()
	return s.Targets
}

func (s *Server) setTargetRegistry(registry targets.Registry) {
	s.targetsMu.Lock()
	defer s.targetsMu.Unlock()
	s.Targets = registry
}

func (s *Server) refreshPolicyEvaluator(paths users.Paths) error {
	set, diagnostics, err := policy.LoadDir(paths.PolicyDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("policies invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	set.SetReviewer(s.LLMProfiles)
	if s.Policies != nil {
		s.Policies.Set(set)
	}
	return nil
}

func (s *Server) refreshLLMProfilesAndPolicies(paths users.Paths) error {
	profiles, diagnostics, err := llm.LoadDir(paths.LLMProfileDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("LLM profiles invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	catalog, err := llm.NewCatalog(profiles)
	if err != nil {
		return validationError(err.Error())
	}
	catalog.ConfigureDebug(s.Debug, s.Logger)
	set, policyDiagnostics, err := policy.LoadDir(paths.PolicyDir)
	if err != nil {
		return err
	}
	if len(policyDiagnostics) > 0 {
		return validationError("policies invalid: " + policyDiagnostics[0].Path + ": " + policyDiagnostics[0].Message)
	}
	set.SetReviewer(s.LLMProfiles)
	if s.LLMProfiles != nil {
		s.LLMProfiles.Set(catalog)
	}
	if s.Policies != nil {
		s.Policies.Set(set)
	}
	return nil
}

func writeUserMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		payload.Code = "invalid_user"
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_user"})
}

func writeAgentMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		if payload.Code == "" {
			payload.Code = "invalid_agent"
		}
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_agent"})
}

func writeTargetMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		if payload.Code == "" {
			payload.Code = "invalid_target"
		}
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_target"})
}

func writeInteractionMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		payload.Code = "invalid_interaction"
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_interaction"})
}

func writePolicyMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		if payload.Code == "" {
			payload.Code = "invalid_policy"
		}
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_policy"})
}
