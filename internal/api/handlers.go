package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/interaction"
	"proxyma/internal/interaction/history"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/llm"
	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
	"proxyma/internal/transcriptor"
	"proxyma/internal/users"
	"proxyma/internal/winrmclient"

	"github.com/google/uuid"
)

const agentCommandIdempotencyCapability = "agent_command_idempotency_v1"

// Server owns Proxyma HTTP dependencies.
type Server struct {
	Agents                    agents.Registry
	Users                     users.Registry
	UserSessions              *users.SessionStore
	CredentialTokens          *users.CredentialTokenStore
	Targets                   targets.Registry
	Policies                  *policy.MutableEvaluator
	LLMProfiles               *llm.MutableCatalog
	Tasks                     *tasks.Manager
	Interactions              interaction.Communicator
	InteractionHistory        history.QueryStore
	HistoryVisibilitySync     history.VisibilitySynchronizer
	HistoryRegistryUpdate     func(agents.Registry, targets.Registry)
	InteractionProvider       *interactioninstances.InteractionRouter
	SSHClient                 sshclient.Client
	WinRMClient               winrmclient.Client
	CommandTimeout            time.Duration
	OutputLimit               int64
	KnownHostsPath            string
	ManagementPaths           users.Paths
	UserSessionTTL            time.Duration
	UserActivationTokenTTL    time.Duration
	UserActivationTokenMaxTTL time.Duration
	UseDevelopmentCookies     bool
	BuildInfo                 BuildInfoResponse
	Debug                     bool
	Logger                    *log.Logger
	usersMu                   sync.RWMutex
	agentsMu                  sync.RWMutex
	targetsMu                 sync.RWMutex
	interactionProvidersMu    sync.Mutex
	throttle                  *loginThrottle
	credentialTestThrottle    *credentialTestThrottle
	credentialProofsMu        sync.Mutex
	credentialProofs          *credentialProofStore
}

func (s *Server) targetCredentialProofs() *credentialProofStore {
	s.credentialProofsMu.Lock()
	defer s.credentialProofsMu.Unlock()
	if s.credentialProofs == nil {
		s.credentialProofs = newCredentialProofStore()
	}
	return s.credentialProofs
}

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
		if _, err := uuid.Parse(wireRequest.ClientOperationID); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "client_operation_id must be a UUID", Code: "invalid_client_operation_id"})
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
	s.debugf("debug command created task_id=%s target=%s command_family=%s strategy=%s requested_by=%q command_len=%d metadata_keys=%s",
		task.TaskID, formatTarget(task.Target), transcriptionFamily(task.Transcription), transcriptionStrategy(task.Transcription), request.RequestedBy, len(request.Command), strings.Join(metadataKeys(request.Metadata), ","))
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

func (s *Server) debugf(format string, args ...any) {
	if !s.Debug {
		return
	}
	s.logf(format, args...)
}

func (s *Server) logf(format string, args ...any) {
	logger := s.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf(format, args...)
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
	request.Target = &tasks.Target{Transport: matchedTarget.Transport, OS: matchedTarget.OS, Hostname: matchedTarget.Hostname, IP: matchedTarget.IP, PolicyID: matchedTarget.PolicyID}
	request.InteractionInstanceID = agent.InteractionInstanceID
	request.Transcription = &plan.Transcription
	request.ExecutionPlan = plan
	request.SensitiveValues = append([]string(nil), plan.SensitiveValues...)
	return nil
}

func (s *Server) handleResolveTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	requested, err := resolveTargetQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agent, ok := agentFromContext(r.Context())
	if !ok {
		writeAgentAuthError(w, "auth_required", "missing authenticated agent", false, "")
		return
	}
	matchedTarget, err := s.resolveRequestedTarget(&tasks.Target{Hostname: requested.Hostname, IP: requested.IP}, agent.ID)
	if err != nil {
		status, payload := errorResponse(err)
		writeJSON(w, status, payload)
		return
	}
	if !canSubmitCommand(agent, matchedTarget) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "agent is not authorized for target"})
		return
	}
	writeJSON(w, http.StatusOK, resolveTargetResponse(matchedTarget, requested))
}

func resolveTargetQuery(r *http.Request) (*CommandTargetRequest, error) {
	values := r.URL.Query()
	for key, items := range values {
		if key != "hostname" && key != "ip" {
			return nil, errors.New("unsupported target resolve query parameter")
		}
		if len(items) != 1 {
			return nil, errors.New("target resolve query parameters must not be repeated")
		}
	}
	requested := &CommandTargetRequest{
		Hostname: strings.TrimSpace(values.Get("hostname")),
		IP:       strings.TrimSpace(values.Get("ip")),
	}
	if requested.Hostname == "" && requested.IP == "" {
		return nil, errors.New("target is required")
	}
	return requested, nil
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

func resolveTargetResponse(target targets.Target, requested *CommandTargetRequest) ResolveTargetResponse {
	destination := strings.TrimSpace(target.IP)
	if requested != nil {
		if strings.TrimSpace(requested.IP) != "" {
			destination = strings.TrimSpace(requested.IP)
		} else if strings.TrimSpace(requested.Hostname) != "" {
			destination = strings.TrimSpace(requested.Hostname)
		}
	}
	if destination == "" {
		destination = strings.TrimSpace(target.Hostname)
	}
	payloadKind := "posix_shell"
	switch {
	case target.Transport == targets.TransportWinRM:
		payloadKind = "powershell"
	case target.OS == targets.OSWindows:
		payloadKind = "windows_shell"
	}
	return ResolveTargetResponse{
		Target: ResolvedTargetResponse{
			Transport: target.Transport,
			OS:        target.OS,
			Hostname:  target.Hostname,
			IP:        target.IP,
		},
		CanonicalCommand: CanonicalCommandHint{
			Prefix:      target.Transport + " " + tasks.PlaceholderUser + "@" + destination + " ",
			PayloadKind: payloadKind,
		},
	}
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

func (s *Server) matchRequestedTarget(target *tasks.Target, agentID string) (targets.Target, error) {
	return s.matchRequestedTargetWithOptions(target, agentID, true)
}

func (s *Server) resolveRequestedTarget(target *tasks.Target, agentID string) (targets.Target, error) {
	return s.matchRequestedTargetWithOptions(target, agentID, false)
}

func (s *Server) matchRequestedTargetWithOptions(target *tasks.Target, agentID string, revealUnauthorized bool) (targets.Target, error) {
	hostname := strings.TrimSpace(target.Hostname)
	ip := strings.TrimSpace(target.IP)

	var matched targets.Target
	hasMatch := false
	for _, candidate := range []struct {
		label string
		value string
	}{
		{label: "hostname", value: hostname},
		{label: "ip", value: ip},
	} {
		if candidate.value == "" {
			continue
		}
		target, found, duplicate := s.targetRegistry().MatchTargetForAgent(candidate.value, agentID)
		switch {
		case duplicate:
			return targets.Target{}, validationError("multiple targets match the submitted target " + candidate.label)
		case !found:
			if revealUnauthorized {
				if _, globallyFound, globallyDuplicate := s.targetRegistry().MatchTarget(candidate.value); globallyFound || globallyDuplicate {
					return targets.Target{}, forbiddenError("agent is not authorized for target")
				}
			}
			// Resolve is a discovery-sensitive endpoint when revealUnauthorized is
			// false. Do not reveal whether a target exists globally when this agent
			// is not authorized for it.
			return targets.Target{}, notFoundError("unknown target")
		case !hasMatch:
			matched = target
			hasMatch = true
		case !sameMatchedTarget(matched, target):
			return targets.Target{}, validationError("target hostname and ip identify different targets")
		}
	}
	if !hasMatch {
		return targets.Target{}, notFoundError("unknown target")
	}
	return matched, nil
}

func sameMatchedTarget(a, b targets.Target) bool {
	return strings.EqualFold(strings.TrimSpace(a.Hostname), strings.TrimSpace(b.Hostname)) &&
		strings.EqualFold(strings.TrimSpace(a.IP), strings.TrimSpace(b.IP))
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/agent/tasks/")
	if strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	task, ok := s.Tasks.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	agent, ok := agentFromContext(r.Context())
	if !ok {
		writeAgentAuthError(w, "auth_required", "missing authenticated agent", false, "")
		return
	}
	if !canReadTask(agent, task) {
		writeError(w, http.StatusForbidden, "agent is not authorized for task")
		return
	}
	writeJSON(w, http.StatusOK, taskStatusResponseFromTask(task))
}

func (s *Server) handleAgentInteractions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/agent/interactions/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if s.Interactions == nil || len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusNotFound, "interaction not found")
		return
	}
	interactionID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		record, err := s.Interactions.Get(r.Context(), interactionID)
		if err != nil {
			writeInteractionError(w, err)
			return
		}
		if err := s.authorizeAgentInteraction(r, record); err != nil {
			status, payload := errorResponse(err)
			writeJSON(w, status, payload)
			return
		}
		writeJSON(w, http.StatusOK, interactionResponseFromInteraction(record))
		return
	}
	writeError(w, http.StatusNotFound, "interaction not found")
}

func (s *Server) authorizeAgentInteraction(r *http.Request, record *interaction.Interaction) error {
	agent, ok := agentFromContext(r.Context())
	if !ok {
		return requestError{status: http.StatusUnauthorized, payload: ErrorResponse{Error: "missing authenticated agent", Code: "auth_required"}}
	}
	if record == nil || strings.TrimSpace(record.TaskID) == "" || s.Tasks == nil {
		return forbiddenError("agent is not authorized for interaction")
	}
	task, ok := s.Tasks.Get(record.TaskID)
	if !ok || !canUseInteraction(agent, record, task) {
		return forbiddenError("agent is not authorized for interaction")
	}
	return nil
}

func writeInteractionError(w http.ResponseWriter, err error) {
	if errors.Is(err, interaction.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction not found")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok", Capabilities: []string{agentCommandIdempotencyCapability}})
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
