package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"proxyma/internal/agents"
	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
	"proxyma/internal/transcriptor"
)

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBodyErr *http.MaxBytesError
		if errors.As(err, &maxBodyErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON command request")
		return
	}
	s.debugf("debug command request body=%s", string(body))
	var wireRequest CommandRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wireRequest); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		s.debugf("debug command request decode failed error=%q", err.Error())
		writeError(w, http.StatusBadRequest, "invalid JSON command request")
		return
	}
	request := commandContextFromRequest(wireRequest)
	agent, ok := agentFromContext(r.Context())
	if !ok {
		writeAgentAuthError(w, "auth_required", "missing authenticated agent", false, "")
		return
	}
	request.AgentID = agent.ID
	if wireRequest.ClientOperationID != "" {
		if err := requireUUIDV4(wireRequest.ClientOperationID, "client_operation_id"); err != nil {
			_, payload := errorResponse(err)
			payload.Code = "invalid_client_operation_id"
			writeJSON(w, http.StatusBadRequest, payload)
			return
		}
		requestHash, err := commandRequestHash(wireRequest)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not hash command request")
			return
		}
		request.ClientRequestHash = requestHash
		existing, found, lookupErr := s.Tasks.LookupOperation(agent.ID, wireRequest.ClientOperationID, requestHash)
		if errors.Is(lookupErr, tasks.ErrIdempotencyConflict) {
			status, payload := errorResponse(idempotencyConflictError())
			writeJSON(w, status, payload)
			return
		}
		if lookupErr != nil {
			writeError(w, http.StatusInternalServerError, lookupErr.Error())
			return
		}
		if found {
			writeJSON(w, http.StatusAccepted, CommandCreatedResponse{TaskID: existing.TaskID, Status: existing.Status, ClientOperationID: existing.ClientOperationID, Replayed: true})
			return
		}
	}
	if err := s.prepareCommand(&request, agent); err != nil {
		status, payload := errorResponse(err)
		s.debugf("debug command request rejected status=%d error=%q code=%q retryable=%t",
			status, payload.Error, payload.Code, payload.Retryable)
		writeJSON(w, status, payload)
		return
	}
	task, replayed, err := s.Tasks.SubmitIdempotent(r.Context(), request)
	if err != nil {
		if errors.Is(err, tasks.ErrIdempotencyConflict) {
			status, payload := errorResponse(idempotencyConflictError())
			writeJSON(w, status, payload)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.debugf("debug command created task_id=%s target=%s command_family=%s strategy=%s harness_name=%q command_len=%d metadata_keys=%s",
		task.TaskID, formatTarget(task.Target), transcriptionFamily(task.Transcription), transcriptionStrategy(task.Transcription), request.HarnessName, len(request.Command), strings.Join(metadataKeys(request.Metadata), ","))
	writeJSON(w, http.StatusAccepted, CommandCreatedResponse{TaskID: task.TaskID, Status: task.Status, ClientOperationID: task.ClientOperationID, Replayed: replayed})
}

func commandRequestHash(request CommandRequest) (string, error) {
	request.ClientOperationID = ""
	data, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

func metadataKeys(metadata map[string]interface{}) []string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Server) prepareCommand(request *tasks.CommandContext, agent agents.Agent) error {
	switch {
	case strings.TrimSpace(request.Command) == "":
		return validationError("command is required")
	case strings.TrimSpace(request.Reasoning) == "":
		return validationError("reasoning is required")
	case strings.TrimSpace(request.HarnessName) == "":
		return validationError("harness_name is required")
	case request.Target == nil || (strings.TrimSpace(request.Target.Hostname) == "" && strings.TrimSpace(request.Target.IP) == ""):
		return validationError("target is required")
	}

	matchedTarget, err := s.matchRequestedTarget(request.Target, agent.ID)
	if err != nil {
		return err
	}
	if !canSubmitCommand(agent, matchedTarget) {
		return forbiddenError("agent is not authorized for target")
	}

	var plan *transcriptor.Plan
	username := matchedTarget.Username
	switch matchedTarget.Transport {
	case targets.TransportSSH:
		parsed, parseErr := transcriptor.ParseSSH(request.Command)
		if parseErr != nil {
			return transcriptorRequestError(parseErr)
		}
		if err := validateUserToken(parsed.TargetUserToken, "SSH"); err != nil {
			return err
		}
		resolved, resolveErr := tasks.ResolveCredentialPlaceholders(parsed.RemoteCommand, matchedTarget)
		if resolveErr != nil {
			return validationError(resolveErr.Error())
		}
		auth, sensitiveValues, authErr := sshAuthForTarget(matchedTarget, username)
		if authErr != nil {
			return validationError(authErr.Error())
		}
		plan = transcriptor.BuildSSHPlan(request.Command, parsed, username, resolved.Command, auth, sensitiveValues, s.KnownHostsPath, s.CommandTimeout, s.OutputLimit)
	case targets.TransportWinRM:
		parsed, parseErr := transcriptor.ParseWinRM(request.Command)
		if parseErr != nil {
			return transcriptorRequestError(parseErr)
		}
		if err := validateUserToken(parsed.TargetUserToken, "WinRM"); err != nil {
			return err
		}
		resolved, resolveErr := tasks.ResolveCredentialPlaceholders(parsed.PowerShell, matchedTarget)
		if resolveErr != nil {
			return validationError(resolveErr.Error())
		}
		password, ok := matchedTarget.PasswordCredential()
		if !ok || matchedTarget.WinRM == nil {
			return validationError("matched target cannot resolve WinRM authentication")
		}
		sensitiveValues := appendCredentialValue(appendCredentialValue(nil, username), password)
		plan = transcriptor.BuildWinRMPlan(request.Command, parsed, username, resolved.Command, password, sensitiveValues, matchedTarget.WinRM.Scheme, matchedTarget.WinRM.Port, matchedTarget.WinRM.TLSInsecure, s.CommandTimeout, s.OutputLimit)
	default:
		return validationError("unsupported target transport " + matchedTarget.Transport)
	}
	if err := tasks.ValidateCommandPolicy(request.Command, matchedTarget); err != nil {
		return validationError(err.Error())
	}
	request.AgentName = agent.DisplayName
	request.Target = &tasks.Target{ID: matchedTarget.ID, Transport: matchedTarget.Transport, OS: matchedTarget.OS, Hostname: matchedTarget.Hostname, IP: matchedTarget.IP, PolicyID: matchedTarget.PolicyID}
	request.InteractionInstanceID = agent.InteractionInstanceID
	request.Transcription = &plan.Transcription
	request.ExecutionPlan = plan
	request.SensitiveValues = append([]string(nil), plan.SensitiveValues...)
	return nil
}

func validateUserToken(token, transport string) error {
	if strings.TrimSpace(token) == "" || token == tasks.PlaceholderUser {
		return nil
	}
	return transcriptorRequestError(&transcriptor.Error{
		Code:      transcriptor.CodeUnsupportedCommandFormat,
		Retryable: true,
		Hint:      "submit canonical " + strings.ToLower(transport) + " {user}@host remote-command format",
		Message:   "command transcriptor cannot ingest a literal username in canonical " + transport + " target syntax",
	})
}

func sshAuthForTarget(target targets.Target, username string) (transcriptor.SSHAuth, []string, error) {
	sensitiveValues := appendCredentialValue(nil, username)
	switch {
	case target.Auth == nil:
		return transcriptor.SSHAuth{}, nil, errors.New("matched target cannot resolve SSH authentication")
	case target.Auth.Type == targets.AuthTypePassword:
		password, ok := target.PasswordCredential()
		if !ok {
			return transcriptor.SSHAuth{}, nil, errors.New("matched target cannot resolve " + tasks.PlaceholderPassword)
		}
		sensitiveValues = appendCredentialValue(sensitiveValues, password)
		return transcriptor.SSHAuth{Type: sshclient.AuthTypePassword, Password: password}, sensitiveValues, nil
	case target.Auth.Type == targets.AuthTypePrivateKey:
		privateKeyPath, ok := target.PrivateKeyCredential()
		if !ok {
			return transcriptor.SSHAuth{}, nil, errors.New("matched target cannot resolve private key")
		}
		return transcriptor.SSHAuth{Type: sshclient.AuthTypePrivateKey, PrivateKeyPath: privateKeyPath}, sensitiveValues, nil
	default:
		return transcriptor.SSHAuth{}, nil, errors.New("unsupported auth type " + target.Auth.Type)
	}
}

func appendCredentialValue(values []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	return append(values, value)
}

func formatTarget(target *tasks.Target) string {
	if target == nil {
		return ""
	}
	if strings.TrimSpace(target.Hostname) != "" {
		return target.Hostname
	}
	return target.IP
}

func transcriptionFamily(metadata *transcriptor.Metadata) string {
	if metadata == nil {
		return ""
	}
	return metadata.CommandFamily
}

func transcriptionStrategy(metadata *transcriptor.Metadata) string {
	if metadata == nil {
		return ""
	}
	return metadata.Strategy
}
