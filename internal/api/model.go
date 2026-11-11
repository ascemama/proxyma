package api

import (
	"time"

	"proxyma/internal/interaction"
	"proxyma/internal/interaction/history"
	"proxyma/internal/llm"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
	"proxyma/internal/transcriptor"
)

// CommandRequest is the JSON body submitted to POST /v1/agent/commands.
type CommandRequest struct {
	ClientOperationID string                `json:"client_operation_id,omitempty"`
	Command           string                `json:"command"`
	Reasoning         string                `json:"reasoning"`
	Target            *CommandTargetRequest `json:"target"`
	HarnessName       string                `json:"harness_name,omitempty"`
	Metadata          map[string]any        `json:"metadata,omitempty"`
}

// CommandTargetRequest identifies the Proxyma-managed target to use for a command.
type CommandTargetRequest struct {
	Hostname string `json:"hostname,omitempty"`
	IP       string `json:"ip,omitempty"`
}

// CommandCreatedResponse is returned when a command task is created.
type CommandCreatedResponse struct {
	TaskID            string `json:"task_id"`
	Status            string `json:"status"`
	ClientOperationID string `json:"client_operation_id,omitempty"`
	Replayed          bool   `json:"replayed,omitempty"`
}

// TaskStatusResponse is the JSON response returned by GET /v1/agent/tasks/{task_id}.
type TaskStatusResponse struct {
	TaskID             string                      `json:"task_id"`
	ClientOperationID  string                      `json:"client_operation_id,omitempty"`
	AgentID            string                      `json:"agent_id,omitempty"`
	Target             *TaskTargetResponse         `json:"target,omitempty"`
	Command            string                      `json:"command"`
	Reasoning          string                      `json:"reasoning"`
	HarnessName        string                      `json:"harness_name,omitempty"`
	Metadata           map[string]any              `json:"metadata,omitempty"`
	Transcription      *TranscriptionResponse      `json:"transcription,omitempty"`
	Status             string                      `json:"status"`
	CreatedAt          time.Time                   `json:"created_at"`
	StartedAt          *time.Time                  `json:"started_at,omitempty"`
	CompletedAt        *time.Time                  `json:"completed_at,omitempty"`
	Result             *TaskResultResponse         `json:"result,omitempty"`
	PendingInteraction *PendingInteractionResponse `json:"pending_interaction,omitempty"`
	Approval           *ApprovalResponse           `json:"approval,omitempty"`
	Policy             *PolicyDecisionResponse     `json:"policy,omitempty"`
	History            []TaskHistoryEventResponse  `json:"history"`
}

// TaskTargetResponse identifies the matched remote target without credentials.
type TaskTargetResponse struct {
	Transport string `json:"transport"`
	OS        string `json:"os"`
	Hostname  string `json:"hostname,omitempty"`
	IP        string `json:"ip,omitempty"`
	PolicyID  string `json:"policy_id,omitempty"`
}

// ResolveTargetResponse returns safe target-routing metadata for harnesses.
type ResolveTargetResponse struct {
	Target           ResolvedTargetResponse `json:"target"`
	CanonicalCommand CanonicalCommandHint   `json:"canonical_command"`
}

// AgentPolicyGuidanceResponse contains only the current policy guidance for an
// agent-authorized target. It is advisory; command enforcement remains server-side.
type AgentPolicyGuidanceResponse struct {
	Target ResolvedPolicyTargetResponse `json:"target"`
	Policy AgentPolicyResponse          `json:"policy"`
}

// ResolvedPolicyTargetResponse identifies the target associated with guidance
// without exposing credentials or ownership details.
type ResolvedPolicyTargetResponse struct {
	ID        string `json:"id"`
	Transport string `json:"transport"`
	OS        string `json:"os"`
	Hostname  string `json:"hostname,omitempty"`
	IP        string `json:"ip,omitempty"`
}

// AgentPolicyResponse is the safe agent-facing projection of a target policy.
type AgentPolicyResponse struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Enabled     bool              `json:"enabled"`
	Blacklist   []AgentPolicyRule `json:"blacklist"`
	Whitelist   []AgentPolicyRule `json:"whitelist"`
}

// AgentPolicyRule contains command guidance an agent may use when choosing a
// command. Rule IDs and other policy-management metadata are not needed here.
type AgentPolicyRule struct {
	CommandTemplate string `json:"command_template"`
	Reason          string `json:"reason,omitempty"`
}

// ResolvedTargetResponse identifies the matched target without credentials.
type ResolvedTargetResponse struct {
	Transport string `json:"transport"`
	OS        string `json:"os"`
	Hostname  string `json:"hostname,omitempty"`
	IP        string `json:"ip,omitempty"`
}

// CanonicalCommandHint tells harnesses how to prefix a target command.
type CanonicalCommandHint struct {
	Prefix      string `json:"prefix"`
	PayloadKind string `json:"payload_kind"`
}

// TaskResultResponse is the final command outcome exposed through task status.
type TaskResultResponse struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitStatus *int   `json:"exit_status"`
	Truncated  bool   `json:"truncated"`
	Error      string `json:"error,omitempty"`
}

// PendingInteractionResponse is safe task-visible user-input metadata.
type PendingInteractionResponse struct {
	InteractionID string     `json:"interaction_id"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

// ApprovalResponse is safe task-visible command approval metadata.
type ApprovalResponse struct {
	Required      bool       `json:"required"`
	Status        string     `json:"status"`
	InteractionID string     `json:"interaction_id,omitempty"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
}

// PolicyDecisionResponse is safe task-visible command policy metadata.
type PolicyDecisionResponse struct {
	Decision      string `json:"decision"`
	PolicyID      string `json:"policy_id,omitempty"`
	RuleID        string `json:"rule_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Justification string `json:"justification,omitempty"`
	Source        string `json:"source,omitempty"`
	LLMProfileID  string `json:"llm_profile_id,omitempty"`
}

// TaskHistoryEventResponse records one externally visible task lifecycle transition.
type TaskHistoryEventResponse struct {
	Status  string    `json:"status"`
	At      time.Time `json:"at"`
	Message string    `json:"message,omitempty"`
}

// TranscriptionResponse is safe command transcriptor metadata exposed to harness clients.
type TranscriptionResponse struct {
	Transcribed   bool   `json:"transcribed"`
	CommandFamily string `json:"command_family"`
	Strategy      string `json:"strategy"`
	Retryable     bool   `json:"retryable,omitempty"`
}

// HealthResponse is returned by GET /v1/health.
type HealthResponse struct {
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// ErrorResponse is the JSON error shape returned by Proxyma.
type ErrorResponse struct {
	Error     string          `json:"error"`
	Code      string          `json:"code,omitempty"`
	Retryable bool            `json:"retryable,omitempty"`
	Hint      string          `json:"hint,omitempty"`
	HostKey   *SSHHostKeyInfo `json:"host_key,omitempty"`
}

// SSHHostKeyInfo is safe server identity metadata shown for explicit trust approval.
type SSHHostKeyInfo struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}

// LoginRequest is submitted to POST /v1/user/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ChangePasswordRequest is submitted by an authenticated user changing their password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// UserSettingsRequest updates settings owned by the authenticated user.
type UserSettingsRequest struct {
	Theme string `json:"theme"`
}

// UserProfileResponse is the safe Proxyma user profile returned to GUI callers.
type UserProfileResponse struct {
	ID                  string     `json:"id"`
	Username            string     `json:"username"`
	Name                string     `json:"name"`
	Admin               bool       `json:"admin"`
	Status              string     `json:"status"`
	Theme               string     `json:"theme"`
	ActivationURL       string     `json:"activation_url,omitempty"`
	ActivationExpiresAt *time.Time `json:"activation_expires_at,omitempty"`
}

// UserSessionResponse is returned by user login and session endpoints.
type UserSessionResponse struct {
	User      UserProfileResponse `json:"user"`
	ExpiresAt time.Time           `json:"expires_at"`
	CSRFToken string              `json:"csrf_token"`
	Build     BuildInfoResponse   `json:"build"`
}

// BuildInfoResponse identifies the Proxyma server serving the GUI.
type BuildInfoResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// UserUsersResponse lists Proxyma users visible to admin GUI callers.
type UserUsersResponse struct {
	Users []UserProfileResponse `json:"users"`
}

// UserMutationRequest is submitted by admin GUI users to create or update users.
type UserMutationRequest struct {
	Username                       string `json:"username,omitempty"`
	Name                           string `json:"name,omitempty"`
	Admin                          *bool  `json:"admin,omitempty"`
	ActivationTokenLifetimeSeconds *int64 `json:"activation_token_lifetime_seconds,omitempty"`
}

type ActivationPolicyResponse struct {
	DefaultLifetimeSeconds int64 `json:"default_lifetime_seconds"`
	MaximumLifetimeSeconds int64 `json:"maximum_lifetime_seconds"`
}

type ActivationContextResponse struct {
	Username  string    `json:"username"`
	Name      string    `json:"name"`
	Purpose   string    `json:"purpose"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CompleteActivationRequest struct {
	NewPassword string `json:"new_password"`
}
type RegenerateActivationRequest struct {
	ActivationTokenLifetimeSeconds *int64 `json:"activation_token_lifetime_seconds,omitempty"`
}
type ActivationLinkResponse struct {
	ActivationURL       string    `json:"activation_url"`
	ActivationExpiresAt time.Time `json:"activation_expires_at"`
}

// UserAgentsResponse lists Proxyma agents visible to the logged-in GUI user.
type UserAgentsResponse struct {
	Agents []UserAgentResponse `json:"agents"`
}

// UserAgentResponse is safe Proxyma agent metadata for GUI callers.
type UserAgentResponse struct {
	ID                    string   `json:"id"`
	DisplayName           string   `json:"display_name,omitempty"`
	Enabled               bool     `json:"enabled"`
	UserIDs               []string `json:"user_ids"`
	InteractionInstanceID string   `json:"interaction_instance_id,omitempty"`
}

// UserAgentMutationRequest is submitted by GUI users to create or update agents.
type UserAgentMutationRequest struct {
	DisplayName           string `json:"display_name,omitempty"`
	AgentJWTSigningKey    string `json:"agent_jwt_signing_key,omitempty"`
	Enabled               *bool  `json:"enabled,omitempty"`
	InteractionInstanceID string `json:"interaction_instance_id,omitempty"`
}

// UserTargetsResponse lists Proxyma targets visible to the logged-in GUI user.
type UserTargetsResponse struct {
	Targets []UserTargetResponse `json:"targets"`
}

// UserTargetMutationRequest is submitted by GUI users to create or update targets.
type UserTargetMutationRequest struct {
	Transport                  string           `json:"transport,omitempty"`
	OS                         string           `json:"os,omitempty"`
	Hostname                   string           `json:"hostname,omitempty"`
	IP                         string           `json:"ip,omitempty"`
	Description                string           `json:"description,omitempty"`
	Username                   string           `json:"username,omitempty"`
	PolicyID                   string           `json:"policy_id,omitempty"`
	AgentIDs                   []string         `json:"agent_ids,omitempty"`
	AuthType                   string           `json:"auth_type,omitempty"`
	Password                   string           `json:"password,omitempty"`
	PrivateKeyPath             string           `json:"private_key_path,omitempty"`
	PrivateKeyUpload           []byte           `json:"-"`
	WinRM                      *UserTargetWinRM `json:"winrm,omitempty"`
	TargetID                   string           `json:"target_id,omitempty"`
	ApprovedHostKeyFingerprint string           `json:"approved_host_key_fingerprint,omitempty"`
	CredentialTestToken        string           `json:"credential_test_token,omitempty"`
}

// UserTargetWinRM contains safe WinRM endpoint settings.
type UserTargetWinRM struct {
	Scheme      string `json:"scheme"`
	Port        int    `json:"port,omitempty"`
	TLSInsecure bool   `json:"tls_insecure,omitempty"`
}

// UserTargetCredentialTestResponse reports a safe SSH credential-test outcome.
type UserTargetCredentialTestResponse struct {
	Status              string `json:"status"`
	Message             string `json:"message,omitempty"`
	CredentialTestToken string `json:"credential_test_token"`
}

// UserInteractionsResponse lists interaction instances visible to the logged-in GUI user.
type UserInteractionsResponse struct {
	Interactions []UserInteractionResponse `json:"interactions"`
}

// UserInteractionResponse is safe interaction instance metadata for GUI callers.
type UserInteractionResponse struct {
	ID                 string   `json:"id"`
	Type               string   `json:"type"`
	CreatorUserID      string   `json:"creator_user_id"`
	CreatorLabel       string   `json:"creator_label,omitempty"`
	DisplayName        string   `json:"display_name,omitempty"`
	Enabled            bool     `json:"enabled"`
	BaseURL            string   `json:"base_url,omitempty"`
	RequestTopic       string   `json:"request_topic,omitempty"`
	ResponseTopic      string   `json:"response_topic,omitempty"`
	PollInterval       string   `json:"poll_interval,omitempty"`
	PollTimeout        string   `json:"poll_timeout,omitempty"`
	RespondedBy        string   `json:"responded_by,omitempty"`
	TokenConfigured    bool     `json:"token_configured"`
	ReferencedAgentIDs []string `json:"referenced_agent_ids"`
}

// UserInteractionMutationRequest is submitted by GUI users to create or update interaction instances.
type UserInteractionMutationRequest struct {
	Type          string `json:"type,omitempty"`
	CreatorUserID string `json:"creator_user_id,omitempty"`
	DisplayName   string `json:"display_name,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	RequestTopic  string `json:"request_topic,omitempty"`
	ResponseTopic string `json:"response_topic,omitempty"`
	Token         string `json:"token,omitempty"`
	PollInterval  string `json:"poll_interval,omitempty"`
	PollTimeout   string `json:"poll_timeout,omitempty"`
	RespondedBy   string `json:"responded_by,omitempty"`
}

// UserPoliciesResponse lists Proxyma policies visible to GUI callers.
type UserPoliciesResponse struct {
	Policies []UserPolicyResponse `json:"policies"`
}

// UserPolicyResponse is safe Proxyma policy metadata and rule content for GUI callers.
type UserPolicyResponse struct {
	ID                 string                     `json:"id"`
	OwnerUserID        string                     `json:"owner_user_id"`
	Name               string                     `json:"name"`
	Description        string                     `json:"description,omitempty"`
	Enabled            bool                       `json:"enabled"`
	Rules              []UserPolicyRuleResponse   `json:"rules"`
	BlacklistRuleCount int                        `json:"blacklist_rule_count"`
	WhitelistRuleCount int                        `json:"whitelist_rule_count"`
	AssignedTargetIDs  []string                   `json:"assigned_target_ids"`
	AssignedTargets    []UserPolicyTargetResponse `json:"assigned_targets"`
	LLM                *UserPolicyLLMResponse     `json:"llm,omitempty"`
}

type UserPolicyLLMResponse struct {
	Enabled   bool               `json:"enabled"`
	ProfileID string             `json:"profile_id,omitempty"`
	Prompt    string             `json:"prompt,omitempty"`
	Profile   *llm.PublicProfile `json:"profile,omitempty"`
}

// UserPolicyRuleResponse is one policy rule returned to GUI callers.
type UserPolicyRuleResponse struct {
	ID              string `json:"id"`
	Effect          string `json:"effect"`
	CommandTemplate string `json:"command_template"`
	Reason          string `json:"reason,omitempty"`
}

// UserPolicyTargetResponse is safe target metadata shown in policy management.
type UserPolicyTargetResponse struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// UserPolicyMutationRequest is submitted by GUI users to create policies.
type UserPolicyMutationRequest struct {
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Rules       []policy.Rule     `json:"rules,omitempty"`
	LLM         *policy.LLMPolicy `json:"llm,omitempty"`
}

type UserLLMProfilesResponse struct {
	Profiles []llm.PublicProfile `json:"profiles"`
}

type UserLLMProfileMutationRequest struct {
	Name           string       `json:"name"`
	Provider       llm.Provider `json:"provider"`
	Model          string       `json:"model"`
	BaseURL        string       `json:"base_url"`
	APIKey         string       `json:"api_key,omitempty"`
	TimeoutSeconds int          `json:"timeout_seconds"`
	Enabled        *bool        `json:"enabled,omitempty"`
}

type UserLLMProfileTestRequest struct {
	Name           string       `json:"name"`
	Provider       llm.Provider `json:"provider"`
	Model          string       `json:"model"`
	BaseURL        string       `json:"base_url"`
	APIKey         string       `json:"api_key"`
	TimeoutSeconds int          `json:"timeout_seconds"`
}

type UserLLMProfileTestResponse struct {
	Status string `json:"status"`
}

type PolicyEvaluationRequest struct {
	TargetID string `json:"target_id"`
	Command  string `json:"command"`
}

type PolicyEvaluationResponse struct {
	Decision PolicyDecisionResponse `json:"decision"`
}

// UserPolicyTargetAssignmentRequest replaces target assignments visible to the actor.
type UserPolicyTargetAssignmentRequest struct {
	TargetIDs []string `json:"target_ids"`
}

// UserPolicyRuleListImportResponse describes an append-only rule-list import.
type UserPolicyRuleListImportResponse struct {
	Policy    UserPolicyResponse                `json:"policy"`
	Added     []UserPolicyRuleResponse          `json:"added"`
	Skipped   []UserPolicyRuleListEntryResponse `json:"skipped"`
	Conflicts []UserPolicyRuleListEntryResponse `json:"conflicts"`
}

type UserPolicyRuleListEntryResponse struct {
	CommandTemplate string `json:"command_template"`
	Reason          string `json:"reason,omitempty"`
}

// UserPolicyRuleConflictResponse reports an atomic import conflict.
type UserPolicyRuleConflictResponse struct {
	Error     string                            `json:"error"`
	Code      string                            `json:"code"`
	Conflicts []UserPolicyRuleListEntryResponse `json:"conflicts"`
}

// UserHistoryResponse lists unified command and interaction history entries.
type UserHistoryResponse struct {
	Items      []history.HistoryItem `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

// UserTargetResponse is safe Proxyma target metadata for GUI callers.
type UserTargetResponse struct {
	ID          string           `json:"id"`
	Transport   string           `json:"transport"`
	OS          string           `json:"os"`
	Hostname    string           `json:"hostname,omitempty"`
	IP          string           `json:"ip,omitempty"`
	PolicyID    string           `json:"policy_id"`
	AgentIDs    []string         `json:"agent_ids"`
	UserIDs     []string         `json:"user_ids"`
	Description string           `json:"description,omitempty"`
	AuthType    string           `json:"auth_type,omitempty"`
	WinRM       *UserTargetWinRM `json:"winrm,omitempty"`
}

// InteractionResponse is returned by interaction status and response endpoints.
type InteractionResponse struct {
	InteractionID    string                       `json:"interaction_id"`
	TaskID           string                       `json:"task_id,omitempty"`
	Kind             string                       `json:"kind"`
	ExpectedResponse string                       `json:"expected_response"`
	Subject          string                       `json:"subject"`
	Message          string                       `json:"message"`
	HarnessName      string                       `json:"harness_name,omitempty"`
	Metadata         map[string]any               `json:"metadata,omitempty"`
	Status           string                       `json:"status"`
	CreatedAt        time.Time                    `json:"created_at"`
	ExpiresAt        *time.Time                   `json:"expires_at,omitempty"`
	RespondedAt      *time.Time                   `json:"responded_at,omitempty"`
	Response         *InteractionAnswerResponse   `json:"response,omitempty"`
	CancelReason     string                       `json:"cancel_reason,omitempty"`
	Delivery         *InteractionDeliveryResponse `json:"delivery,omitempty"`
}

// InteractionAnswerResponse is the normalized safe recorded response.
type InteractionAnswerResponse struct {
	Approved    *bool      `json:"approved,omitempty"`
	Answer      string     `json:"answer,omitempty"`
	RespondedBy string     `json:"responded_by,omitempty"`
	RespondedAt *time.Time `json:"responded_at,omitempty"`
}

// InteractionDeliveryResponse is safe provider delivery metadata.
type InteractionDeliveryResponse struct {
	Provider    string    `json:"provider"`
	Reference   string    `json:"reference,omitempty"`
	DeliveredAt time.Time `json:"delivered_at"`
}

func commandContextFromRequest(request CommandRequest) tasks.CommandContext {
	return tasks.CommandContext{
		ClientOperationID: request.ClientOperationID,
		Command:           request.Command,
		Reasoning:         request.Reasoning,
		Target:            taskTargetFromCommandTarget(request.Target),
		HarnessName:       request.HarnessName,
		Metadata:          request.Metadata,
	}
}

func taskTargetFromCommandTarget(target *CommandTargetRequest) *tasks.Target {
	if target == nil {
		return nil
	}
	return &tasks.Target{Hostname: target.Hostname, IP: target.IP}
}

func taskStatusResponseFromTask(task *tasks.Task) TaskStatusResponse {
	if task == nil {
		return TaskStatusResponse{}
	}
	response := TaskStatusResponse{
		TaskID:             task.TaskID,
		ClientOperationID:  task.ClientOperationID,
		AgentID:            task.AgentID,
		Target:             targetResponseFromTaskTarget(task.Target),
		Command:            task.Command,
		Reasoning:          task.Reasoning,
		HarnessName:        task.HarnessName,
		Metadata:           copyMetadata(task.Metadata),
		Transcription:      transcriptionResponseFromMetadata(task.Transcription),
		Status:             task.Status,
		CreatedAt:          task.CreatedAt,
		StartedAt:          task.StartedAt,
		CompletedAt:        task.CompletedAt,
		Result:             resultResponseFromTaskResult(task.Result),
		PendingInteraction: pendingInteractionResponseFromSummary(task.PendingInteraction),
		Approval:           approvalResponseFromApproval(task.Approval),
		Policy:             policyDecisionResponseFromPolicyDecision(task.Policy),
		History:            make([]TaskHistoryEventResponse, 0, len(task.History)),
	}
	for _, event := range task.History {
		response.History = append(response.History, TaskHistoryEventResponse{
			Status:  event.Status,
			At:      event.At,
			Message: event.Message,
		})
	}
	return response
}

func targetResponseFromTaskTarget(target *tasks.Target) *TaskTargetResponse {
	if target == nil {
		return nil
	}
	return &TaskTargetResponse{Transport: target.Transport, OS: target.OS, Hostname: target.Hostname, IP: target.IP, PolicyID: target.PolicyID}
}

func transcriptionResponseFromMetadata(metadata *transcriptor.Metadata) *TranscriptionResponse {
	if metadata == nil {
		return nil
	}
	return &TranscriptionResponse{
		Transcribed:   metadata.Transcribed,
		CommandFamily: metadata.CommandFamily,
		Strategy:      metadata.Strategy,
		Retryable:     metadata.Retryable,
	}
}

func pendingInteractionResponseFromSummary(summary *interaction.Summary) *PendingInteractionResponse {
	if summary == nil {
		return nil
	}
	return &PendingInteractionResponse{
		InteractionID: summary.InteractionID,
		Kind:          summary.Kind,
		Status:        summary.Status,
		CreatedAt:     summary.CreatedAt,
		ExpiresAt:     summary.ExpiresAt,
	}
}

func approvalResponseFromApproval(approval *tasks.Approval) *ApprovalResponse {
	if approval == nil {
		return nil
	}
	return &ApprovalResponse{
		Required:      approval.Required,
		Status:        approval.Status,
		InteractionID: approval.InteractionID,
		DecidedAt:     approval.DecidedAt,
	}
}

func policyDecisionResponseFromPolicyDecision(decision *tasks.PolicyDecision) *PolicyDecisionResponse {
	if decision == nil {
		return nil
	}
	return &PolicyDecisionResponse{
		Decision:      decision.Decision,
		PolicyID:      decision.PolicyID,
		RuleID:        decision.RuleID,
		Reason:        decision.Reason,
		Justification: decision.Justification,
		Source:        decision.Source,
		LLMProfileID:  decision.LLMProfileID,
	}
}

func resultResponseFromTaskResult(result *tasks.TaskResult) *TaskResultResponse {
	if result == nil {
		return nil
	}
	return &TaskResultResponse{
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitStatus: result.ExitStatus,
		Truncated:  result.Truncated,
		Error:      result.Error,
	}
}

func interactionResponseFromInteraction(record *interaction.Interaction) InteractionResponse {
	if record == nil {
		return InteractionResponse{}
	}
	return InteractionResponse{
		InteractionID:    record.InteractionID,
		TaskID:           record.TaskID,
		Kind:             record.Kind,
		ExpectedResponse: record.ExpectedResponse,
		Subject:          record.Subject,
		Message:          record.Message,
		HarnessName:      record.HarnessName,
		Metadata:         copyMetadata(record.Metadata),
		Status:           record.Status,
		CreatedAt:        record.CreatedAt,
		ExpiresAt:        record.ExpiresAt,
		RespondedAt:      record.RespondedAt,
		Response:         interactionAnswerResponseFromResponse(record.Response),
		CancelReason:     record.CancelReason,
		Delivery:         interactionDeliveryResponseFromDelivery(record.Delivery),
	}
}

func interactionAnswerResponseFromResponse(response *interaction.Response) *InteractionAnswerResponse {
	if response == nil {
		return nil
	}
	return &InteractionAnswerResponse{
		Approved:    response.Approved,
		Answer:      response.Answer,
		RespondedBy: response.RespondedBy,
		RespondedAt: response.RespondedAt,
	}
}

func interactionDeliveryResponseFromDelivery(delivery *interaction.Delivery) *InteractionDeliveryResponse {
	if delivery == nil {
		return nil
	}
	return &InteractionDeliveryResponse{
		Provider:    delivery.Provider,
		Reference:   delivery.Reference,
		DeliveredAt: delivery.DeliveredAt,
	}
}

func copyMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}
