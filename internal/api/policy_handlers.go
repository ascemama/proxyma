package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"proxyma/internal/ids"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/users"
)

func (s *Server) handleUserPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listUserPolicies(w, r)
	case http.MethodPost:
		s.createUserPolicy(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUserPolicy(w http.ResponseWriter, r *http.Request) {
	id, suffix, ok := policyPathParts(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "policy not found")
		return
	}
	if err := requireUUIDV4(id, "policy_id"); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if suffix == "targets" {
		if r.Method == http.MethodPut {
			s.assignUserPolicyTargets(w, r, id)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if suffix == "rule-lists/import" {
		if r.Method == http.MethodPost {
			s.importUserPolicyRules(w, r, id)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if suffix == "evaluate" {
		if r.Method == http.MethodPost {
			s.evaluateUserPolicy(w, r, id)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getUserPolicy(w, r, id)
	case http.MethodPut:
		s.updateUserPolicy(w, r, id)
	case http.MethodDelete:
		s.deleteUserPolicy(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listUserPolicies(w http.ResponseWriter, r *http.Request) {
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	items, err := users.ReadPolicies(paths)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	response := UserPoliciesResponse{Policies: make([]UserPolicyResponse, 0, len(items))}
	for _, item := range items {
		converted, err := s.policyResponse(paths, item)
		if err != nil {
			writePolicyMutationError(w, err)
			return
		}
		response.Policies = append(response.Policies, converted)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) getUserPolicy(w http.ResponseWriter, _ *http.Request, id string) {
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	item, err := readPolicy(paths, id)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	response, err := s.policyResponse(paths, item)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) createUserPolicy(w http.ResponseWriter, r *http.Request) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	request, ok := decodePolicyMutationRequest(w, r)
	if !ok {
		return
	}
	if err := validatePolicyReferenceIDs(request); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	item, err := policyFromMutationRequest(request, user.ID)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if err := validatePolicyLLM(paths, item, user); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	result, err := users.CreatePolicy(paths, item, nil)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if err := s.refreshPolicyEvaluator(paths); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	created, err := readPolicy(paths, result.ID)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	response, err := s.policyResponse(paths, created)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) updateUserPolicy(w http.ResponseWriter, r *http.Request, id string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	request, ok := decodePolicyMutationRequest(w, r)
	if !ok {
		return
	}
	if err := validatePolicyReferenceIDs(request); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	existing, err := readPolicy(paths, id)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if !users.CanModifyPolicy(existing, user.ID, user.Admin) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "only the policy owner or an administrator may modify this policy", Code: "forbidden"})
		return
	}
	item, err := policyFromMutationRequest(request, existing.OwnerUserID)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if err := validatePolicyLLM(paths, item, user); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if _, err := users.UpdatePolicy(paths, id, item); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if err := s.refreshPolicyEvaluator(paths); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	updated, err := readPolicy(paths, id)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	response, err := s.policyResponse(paths, updated)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) deleteUserPolicy(w http.ResponseWriter, r *http.Request, id string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	existing, err := readPolicy(paths, id)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if !users.CanModifyPolicy(existing, user.ID, user.Admin) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "only the policy owner or an administrator may delete this policy", Code: "forbidden"})
		return
	}
	if _, err := users.DeletePolicy(paths, id); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if err := s.refreshPolicyEvaluator(paths); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) importUserPolicyRules(w http.ResponseWriter, r *http.Request, id string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	existing, err := readPolicy(paths, id)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if !users.CanModifyPolicy(existing, user.ID, user.Admin) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "only the policy owner or an administrator may import rules", Code: "forbidden"})
		return
	}
	var document users.RuleListDocument
	if !decodeStrictJSON(w, r, &document, maxPolicyBodyBytes, "invalid JSON rule-list import") {
		return
	}
	result, err := users.ImportRuleList(paths, id, document)
	if err != nil {
		if len(result.Conflicts) > 0 {
			writeJSON(w, http.StatusConflict, UserPolicyRuleConflictResponse{Error: err.Error(), Code: "policy_rule_conflict", Conflicts: importEntryResponses(result.Conflicts)})
		} else {
			writePolicyMutationError(w, err)
		}
		return
	}
	if err := s.refreshPolicyEvaluator(paths); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	response, err := s.policyResponse(paths, result.Policy)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, UserPolicyRuleListImportResponse{Policy: response, Added: policyRuleResponses(result.Added), Skipped: importEntryResponses(result.Skipped), Conflicts: importEntryResponses(result.Conflicts)})
}

func (s *Server) assignUserPolicyTargets(w http.ResponseWriter, r *http.Request, id string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	var request UserPolicyTargetAssignmentRequest
	if !decodeStrictJSON(w, r, &request, maxPolicyBodyBytes, "invalid JSON policy target assignment") {
		return
	}
	if err := requireUUIDV4List(request.TargetIDs, "target_ids"); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if err := users.AssignPolicyTargets(paths, id, user.ID, user.Admin, request.TargetIDs); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if err := s.refreshTargetRegistry(paths); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	item, err := readPolicy(paths, id)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	response, err := s.policyResponse(paths, item)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func policyPathParts(path string) (string, string, bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/v1/user/policies/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], "", true
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "targets" {
		return parts[0], "targets", true
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] == "rule-lists" && parts[2] == "import" {
		return parts[0], "rule-lists/import", true
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "evaluate" {
		return parts[0], "evaluate", true
	}
	return "", "", false
}

func decodePolicyMutationRequest(w http.ResponseWriter, r *http.Request) (UserPolicyMutationRequest, bool) {
	var request UserPolicyMutationRequest
	if !decodeStrictJSON(w, r, &request, maxPolicyBodyBytes, "invalid JSON policy request") {
		return UserPolicyMutationRequest{}, false
	}
	return request, true
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, target any, limit int64, message string) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: message, Code: "invalid_policy"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: message, Code: "invalid_policy"})
		return false
	}
	return true
}

func policyFromMutationRequest(request UserPolicyMutationRequest, ownerID string) (policy.Policy, error) {
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	item := policy.Policy{Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description), OwnerUserID: strings.TrimSpace(ownerID), Enabled: enabled, Rules: make([]policy.Rule, 0, len(request.Rules)), LLM: cloneLLMPolicy(request.LLM)}
	for _, rule := range request.Rules {
		ruleID := strings.TrimSpace(rule.ID)
		if ruleID == "" {
			var err error
			ruleID, err = ids.NewUUIDV4()
			if err != nil {
				return policy.Policy{}, err
			}
		}
		item.Rules = append(item.Rules, policy.Rule{ID: ruleID, Effect: strings.TrimSpace(rule.Effect), CommandTemplate: policy.NormalizeTemplate(rule.CommandTemplate), Reason: strings.TrimSpace(rule.Reason)})
	}
	if item.Name == "" {
		return policy.Policy{}, fmt.Errorf("missing required field name")
	}
	for index := range item.Rules {
		if err := policy.ValidateRuleForImport(&item.Rules[index]); err != nil {
			return policy.Policy{}, fmt.Errorf("rule %d: %w", index, err)
		}
	}
	return item, nil
}

func validatePolicyReferenceIDs(request UserPolicyMutationRequest) error {
	if request.LLM == nil {
		return nil
	}
	return requireOptionalUUIDV4(request.LLM.ProfileID, "profile_id")
}

func readPolicy(paths users.Paths, id string) (policy.Policy, error) {
	items, err := users.ReadPolicies(paths)
	if err != nil {
		return policy.Policy{}, err
	}
	for _, item := range items {
		if item.ID == strings.TrimSpace(id) {
			return item, nil
		}
	}
	return policy.Policy{}, fmt.Errorf("policy %q does not exist", id)
}

func (s *Server) policyResponse(paths users.Paths, item policy.Policy) (UserPolicyResponse, error) {
	assigned, err := users.AssignedTargets(paths, item.ID)
	if err != nil {
		return UserPolicyResponse{}, err
	}
	response := UserPolicyResponse{ID: item.ID, OwnerUserID: item.OwnerUserID, Name: item.Name, Description: item.Description, Enabled: item.Enabled, Rules: policyRuleResponses(item.Rules), AssignedTargetIDs: make([]string, 0, len(assigned)), AssignedTargets: make([]UserPolicyTargetResponse, 0, len(assigned))}
	if item.LLM != nil {
		response.LLM = &UserPolicyLLMResponse{Enabled: item.LLM.Enabled, ProfileID: item.LLM.ProfileID, Prompt: item.LLM.Prompt}
		if profile, err := findLLMProfile(paths, item.LLM.ProfileID); err == nil {
			public := profile.Public()
			response.LLM.Profile = &public
		}
	}
	for _, rule := range item.Rules {
		if rule.Effect == policy.DecisionDeny {
			response.BlacklistRuleCount++
		} else if rule.Effect == policy.DecisionAllow {
			response.WhitelistRuleCount++
		}
	}
	for _, target := range assigned {
		label := target.Hostname
		if label == "" {
			label = target.IP
		}
		if label == "" {
			label = target.ID
		}
		response.AssignedTargetIDs = append(response.AssignedTargetIDs, target.ID)
		response.AssignedTargets = append(response.AssignedTargets, UserPolicyTargetResponse{ID: target.ID, Label: label, Description: target.Description})
	}
	return response, nil
}

func cloneLLMPolicy(value *policy.LLMPolicy) *policy.LLMPolicy {
	if value == nil {
		return nil
	}
	copy := *value
	copy.ProfileID = strings.TrimSpace(copy.ProfileID)
	copy.Prompt = strings.TrimSpace(copy.Prompt)
	return &copy
}

func validatePolicyLLM(paths users.Paths, item policy.Policy, user users.User) error {
	if item.LLM == nil || !item.LLM.Enabled {
		return nil
	}
	if strings.TrimSpace(item.LLM.ProfileID) == "" {
		return fmt.Errorf("enabled LLM policy requires profile_id")
	}
	if strings.TrimSpace(item.LLM.Prompt) == "" {
		return fmt.Errorf("enabled LLM policy requires a prompt")
	}
	profile, err := findLLMProfile(paths, item.LLM.ProfileID)
	if err != nil {
		return fmt.Errorf("LLM profile %q does not exist", item.LLM.ProfileID)
	}
	if !users.CanUseLLMProfile(profile, user.ID, user.Admin) {
		return fmt.Errorf("user is not allowed to use LLM profile %q", item.LLM.ProfileID)
	}
	return nil
}

func (s *Server) evaluateUserPolicy(w http.ResponseWriter, r *http.Request, policyID string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, configured := s.managementPaths()
	if !configured {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	var request PolicyEvaluationRequest
	if !decodeStrictJSON(w, r, &request, maxPolicyBodyBytes, "invalid JSON policy evaluation request") {
		return
	}
	if strings.TrimSpace(request.Command) == "" || strings.TrimSpace(request.TargetID) == "" {
		writeError(w, http.StatusBadRequest, "target_id and command are required")
		return
	}
	if err := requireUUIDV4(request.TargetID, "target_id"); err != nil {
		writePolicyMutationError(w, err)
		return
	}
	target, err := findTargetForPolicyEvaluation(paths, request.TargetID, user)
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	if target.PolicyID != strings.TrimSpace(policyID) {
		writeError(w, http.StatusBadRequest, "target is not assigned to this policy")
		return
	}
	if s.Policies == nil {
		writeError(w, http.StatusInternalServerError, "policy evaluator is not configured")
		return
	}
	result, err := s.Policies.Evaluate(r.Context(), policy.Request{PolicyID: policyID, Target: policy.Target{Hostname: target.Hostname, IP: target.IP, PolicyID: target.PolicyID}, Command: request.Command, RemoteCommand: request.Command})
	if err != nil {
		writePolicyMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PolicyEvaluationResponse{Decision: policyDecisionResponse(result)})
}

func findTargetForPolicyEvaluation(paths users.Paths, id string, user users.User) (targets.Target, error) {
	all, err := users.ReadTargetsForUser(paths, user.ID)
	if err != nil {
		return targets.Target{}, err
	}
	if user.Admin {
		registry, diagnostics, loadErr := targets.LoadDir(paths.TargetDir)
		if loadErr != nil {
			return targets.Target{}, loadErr
		}
		if len(diagnostics) > 0 {
			return targets.Target{}, fmt.Errorf("targets invalid: %s: %s", diagnostics[0].Path, diagnostics[0].Message)
		}
		all = registry.All()
	}
	for _, target := range all {
		if target.ID == strings.TrimSpace(id) {
			return target, nil
		}
	}
	return targets.Target{}, fmt.Errorf("target %q does not exist or is not owned by current user", id)
}

func policyDecisionResponse(result policy.Result) PolicyDecisionResponse {
	return PolicyDecisionResponse{Decision: result.Decision, PolicyID: result.PolicyID, RuleID: result.RuleID, Reason: result.Reason, Justification: result.Justification, Source: result.Source, LLMProfileID: result.LLMProfileID}
}

func importEntryResponses(entries []users.RuleListEntry) []UserPolicyRuleListEntryResponse {
	response := make([]UserPolicyRuleListEntryResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, UserPolicyRuleListEntryResponse{CommandTemplate: entry.CommandTemplate, Reason: entry.Reason})
	}
	return response
}

func policyRuleResponses(rules []policy.Rule) []UserPolicyRuleResponse {
	response := make([]UserPolicyRuleResponse, 0, len(rules))
	for _, rule := range rules {
		response = append(response, UserPolicyRuleResponse{ID: rule.ID, Effect: rule.Effect, CommandTemplate: rule.CommandTemplate, Reason: rule.Reason})
	}
	return response
}
