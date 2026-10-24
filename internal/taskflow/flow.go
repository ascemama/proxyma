// Package taskflow owns Proxyma task lifecycle decisions.
package taskflow

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"proxyma/internal/interaction"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
)

// Manager is the task persistence and execution primitive used by taskflow.
type Manager interface {
	ListByStatus(status string) []*tasks.Task
	WaitForInteraction(taskID string, pending interaction.Summary) (*tasks.Task, error)
	ApplyInteraction(result interaction.Interaction) (*tasks.Task, bool, error)
	SetPolicyDecision(taskID string, decision tasks.PolicyDecision) (*tasks.Task, error)
	StartTask(ctx context.Context, taskID string) (bool, error)
	BlockTask(taskID string, message string) (*tasks.Task, error)
}

// Options configures taskflow decisions.
type Options struct {
	RequireCommandApproval bool
	Policies               policy.Evaluator
	Debug                  bool
	Logger                 *log.Logger
}

// Flow decides how Proxyma tasks move between lifecycle states.
type Flow struct {
	manager      Manager
	interactions interaction.Communicator
	options      Options
}

// New creates a taskflow controller.
func New(manager Manager, interactions interaction.Communicator, options Options) *Flow {
	return &Flow{manager: manager, interactions: interactions, options: options}
}

// StartCreated handles created tasks by either requesting approval or starting execution.
func (f *Flow) StartCreated(ctx context.Context) (int, error) {
	started := 0
	for _, task := range f.manager.ListByStatus(tasks.StatusAcceptedByUser) {
		ok, err := f.manager.StartTask(ctx, task.TaskID)
		if err != nil {
			return started, err
		}
		if ok {
			started++
		}
	}
	for _, task := range f.manager.ListByStatus(tasks.StatusCreated) {
		if approvalPending(task) {
			continue
		}
		decision, err := f.policyDecision(ctx, task)
		if err != nil {
			return started, err
		}
		task.Policy = &decision
		switch decision.Decision {
		case policy.DecisionAllow:
			ok, err := f.manager.StartTask(ctx, task.TaskID)
			if err != nil {
				return started, err
			}
			if ok {
				started++
			}
		case policy.DecisionApproval:
			if err := f.requestApproval(ctx, task); err != nil {
				return started, err
			}
		case policy.DecisionDeny:
			message := decision.Reason
			if strings.TrimSpace(message) == "" {
				message = "command denied by policy"
			}
			if _, err := f.manager.BlockTask(task.TaskID, message); err != nil {
				return started, err
			}
		default:
			if _, err := f.manager.BlockTask(task.TaskID, "command denied by invalid policy decision"); err != nil {
				return started, err
			}
		}
	}
	return started, nil
}

// ManageState applies answered interactions, expires old interactions, and reports running tasks.
func (f *Flow) ManageState(ctx context.Context) (int, error) {
	if f.interactions != nil {
		expired, err := f.interactions.ExpirePending(ctx, time.Now().UTC())
		if err != nil {
			return 0, err
		}
		for _, record := range expired {
			if err := f.applyInteraction(ctx, record); err != nil {
				return 0, err
			}
		}
	}
	for _, task := range f.manager.ListByStatus(tasks.StatusWaitingUserResponse) {
		if task.PendingInteraction == nil || f.interactions == nil {
			continue
		}
		record, err := f.interactions.Get(ctx, task.PendingInteraction.InteractionID)
		if err != nil {
			return 0, err
		}
		if record.Status == interaction.StatusPending {
			continue
		}
		if err := f.applyInteraction(ctx, *record); err != nil {
			return 0, err
		}
	}
	return len(f.manager.ListByStatus(tasks.StatusRunning)), nil
}

func approvalPending(task *tasks.Task) bool {
	return task.Approval != nil && task.Approval.Status == tasks.ApprovalStatusPending
}

func (f *Flow) policyDecision(ctx context.Context, task *tasks.Task) (tasks.PolicyDecision, error) {
	if task.Policy != nil && task.Policy.Decision != "" {
		return *task.Policy, nil
	}
	evaluator := f.options.Policies
	if evaluator == nil {
		evaluator = policy.NoopEvaluator{RequireApproval: f.options.RequireCommandApproval}
	}
	result, err := evaluator.Evaluate(ctx, policy.Request{
		TaskID:        task.TaskID,
		RequestedBy:   task.RequestedBy,
		Target:        policyTarget(task.Target),
		Command:       task.Command,
		RemoteCommand: tasks.RedactCredentialValues(remoteCommand(task), task.SensitiveValues...),
		PolicyID:      targetPolicyID(task.Target),
		Metadata:      task.Metadata,
	})
	if err != nil {
		return tasks.PolicyDecision{}, err
	}
	f.logPolicyEvaluationSteps(task, result)
	decision := tasks.PolicyDecision{
		Decision:      result.Decision,
		PolicyID:      result.PolicyID,
		RuleID:        result.RuleID,
		Reason:        result.Reason,
		Justification: result.Justification,
		Source:        result.Source,
		LLMProfileID:  result.LLMProfileID,
	}
	if _, err := f.manager.SetPolicyDecision(task.TaskID, decision); err != nil {
		return tasks.PolicyDecision{}, err
	}
	f.logPolicyDecision(task, decision)
	return decision, nil
}

func (f *Flow) logPolicyEvaluationSteps(task *tasks.Task, result policy.Result) {
	if !f.options.Debug || f.options.Logger == nil || task == nil {
		return
	}
	for _, step := range result.Trace {
		f.options.Logger.Printf("policy step=%s result=%s task_id=%s agent_id=%s policy_id=%s rule_id=%s llm_profile_id=%s",
			step.Stage, step.Outcome, task.TaskID, task.AgentID, step.PolicyID, step.RuleID, step.LLMProfileID)
	}
}

func (f *Flow) logPolicyDecision(task *tasks.Task, decision tasks.PolicyDecision) {
	if f.options.Logger == nil || task == nil {
		return
	}
	if decision.Reason != "" {
		f.options.Logger.Printf("policy decision=%s source=%s task_id=%s agent_id=%s policy_id=%s rule_id=%s llm_profile_id=%s reason=%q",
			decision.Decision, decision.Source, task.TaskID, task.AgentID, decision.PolicyID, decision.RuleID, decision.LLMProfileID, decision.Reason)
		return
	}
	f.options.Logger.Printf("policy decision=%s source=%s task_id=%s agent_id=%s policy_id=%s rule_id=%s llm_profile_id=%s",
		decision.Decision, decision.Source, task.TaskID, task.AgentID, decision.PolicyID, decision.RuleID, decision.LLMProfileID)
}

func (f *Flow) requestApproval(ctx context.Context, task *tasks.Task) error {
	if f.interactions == nil {
		_, err := f.manager.BlockTask(task.TaskID, "command approval is required but no interaction communicator is configured")
		return err
	}
	record, err := f.interactions.Request(ctx, interaction.Request{
		TaskID:                task.TaskID,
		InteractionInstanceID: task.InteractionInstanceID,
		Kind:                  interaction.KindApprovalRequest,
		ExpectedResponse:      interaction.ExpectedApproval,
		Subject:               "Approve Proxyma command",
		Message:               "Approve the submitted Proxyma command before execution.",
		RequestedBy:           task.RequestedBy,
		Metadata: map[string]any{
			"target":               formatTarget(task.Target),
			"command":              task.Command,
			"reasoning":            task.Reasoning,
			"policy_id":            policyID(task.Policy),
			"policy_rule_id":       policyRuleID(task.Policy),
			"policy_reason":        policyReason(task.Policy),
			"policy_justification": policyJustification(task.Policy),
		},
	})
	if err != nil {
		_, blockErr := f.manager.BlockTask(task.TaskID, "command approval request failed: "+err.Error())
		if blockErr != nil {
			return blockErr
		}
		return err
	}
	_, err = f.manager.WaitForInteraction(task.TaskID, record.ToSummary())
	return err
}

func policyTarget(target *tasks.Target) policy.Target {
	if target == nil {
		return policy.Target{}
	}
	return policy.Target{
		Hostname: target.Hostname,
		IP:       target.IP,
		PolicyID: target.PolicyID,
	}
}

func targetPolicyID(target *tasks.Target) string {
	if target == nil {
		return ""
	}
	return target.PolicyID
}

func remoteCommand(task *tasks.Task) string {
	if task != nil && task.ExecutionPlan != nil && task.ExecutionPlan.SSHRequest != nil {
		return task.ExecutionPlan.SSHRequest.RemoteCommand
	}
	if task != nil && task.ExecutionPlan != nil && task.ExecutionPlan.WinRMRequest != nil {
		return task.ExecutionPlan.WinRMRequest.PowerShell
	}
	if task == nil {
		return ""
	}
	return task.Command
}

func policyID(decision *tasks.PolicyDecision) string {
	if decision == nil {
		return ""
	}
	return decision.PolicyID
}

func policyRuleID(decision *tasks.PolicyDecision) string {
	if decision == nil {
		return ""
	}
	return decision.RuleID
}

func policyReason(decision *tasks.PolicyDecision) string {
	if decision == nil {
		return ""
	}
	return decision.Reason
}

func policyJustification(decision *tasks.PolicyDecision) string {
	if decision == nil {
		return ""
	}
	return decision.Justification
}

func (f *Flow) applyInteraction(ctx context.Context, record interaction.Interaction) error {
	task, applied, err := f.manager.ApplyInteraction(record)
	if err != nil {
		return err
	}
	if !applied || task == nil {
		return nil
	}
	if task.Status == tasks.StatusAcceptedByUser && task.Approval != nil && task.Approval.Status == tasks.ApprovalStatusApproved {
		_, err := f.manager.StartTask(ctx, task.TaskID)
		return err
	}
	return nil
}

func formatTarget(target *tasks.Target) string {
	if target == nil {
		return ""
	}
	switch {
	case strings.TrimSpace(target.Hostname) != "" && strings.TrimSpace(target.IP) != "":
		return fmt.Sprintf("%s (%s)", target.Hostname, target.IP)
	case strings.TrimSpace(target.Hostname) != "":
		return target.Hostname
	default:
		return target.IP
	}
}
