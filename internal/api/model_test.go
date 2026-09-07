package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"proxyma/internal/interaction"
	"proxyma/internal/sshclient"
	"proxyma/internal/tasks"
	"proxyma/internal/transcriptor"
)

func TestTaskStatusResponseDoesNotExposeInternalExecutionFields(t *testing.T) {
	exitStatus := 0
	createdAt := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	task := &tasks.Task{
		TaskID:      "task-123",
		AgentID:     testAgent.ID,
		Target:      &tasks.Target{Hostname: "local-dev", IP: "127.0.0.1"},
		Command:     "ssh {user}@127.0.0.1 hostname",
		Reasoning:   "Identify the target host",
		RequestedBy: "agent-demo",
		Metadata:    map[string]any{"ticket": "INC-123"},
		Transcription: &transcriptor.Metadata{
			Transcribed:   true,
			CommandFamily: transcriptor.KindSSHRemoteCommand,
			Strategy:      transcriptor.StrategyGoSSHClient,
		},
		Status:    tasks.StatusCompleted,
		CreatedAt: createdAt,
		Result: &tasks.TaskResult{
			Stdout:     "target-host\n",
			Stderr:     "",
			ExitStatus: &exitStatus,
			Truncated:  false,
		},
		PendingInteraction: &interaction.Summary{
			InteractionID: "interaction-123",
			Kind:          interaction.KindQuestion,
			Status:        interaction.StatusPending,
			CreatedAt:     createdAt,
		},
		History: []tasks.LifecycleEvent{{Status: tasks.StatusCreated, At: createdAt}},
		ExecutionPlan: &transcriptor.Plan{
			Kind: transcriptor.KindSSHRemoteCommand,
			SSHRequest: &sshclient.Request{
				Host:          "127.0.0.1",
				Username:      "demo",
				Password:      "demo-password",
				RemoteCommand: "hostname",
			},
			SensitiveValues: []string{"demo", "demo-password"},
		},
		SensitiveValues: []string{"demo", "demo-password"},
	}

	body, err := json.Marshal(taskStatusResponseFromTask(task))
	if err != nil {
		t.Fatal(err)
	}
	responseJSON := string(body)
	for _, forbidden := range []string{
		"ExecutionPlan",
		"execution_plan",
		"SensitiveValues",
		"sensitive_values",
		"demo-password",
		"RemoteCommand",
	} {
		if strings.Contains(responseJSON, forbidden) {
			t.Fatalf("response leaked internal field or value %q in %s", forbidden, responseJSON)
		}
	}
	for _, required := range []string{
		`"task_id":"task-123"`,
		`"command":"ssh {user}@127.0.0.1 hostname"`,
		`"transcription"`,
		`"pending_interaction"`,
		`"history"`,
	} {
		if !strings.Contains(responseJSON, required) {
			t.Fatalf("response missing %q in %s", required, responseJSON)
		}
	}
}

func TestCommandRequestMapsToTaskCommandContext(t *testing.T) {
	request := CommandRequest{
		Command:     "ssh {user}@127.0.0.1 hostname",
		Reasoning:   "Identify the target host",
		Target:      &CommandTargetRequest{IP: "127.0.0.1"},
		RequestedBy: "agent-demo",
		Metadata:    map[string]any{"source": "test"},
	}

	context := commandContextFromRequest(request)
	if context.Command != request.Command || context.Reasoning != request.Reasoning || context.RequestedBy != request.RequestedBy {
		t.Fatalf("context = %#v", context)
	}
	if context.Target == nil || context.Target.IP != "127.0.0.1" {
		t.Fatalf("target = %#v", context.Target)
	}
	if context.Transcription != nil || context.ExecutionPlan != nil || len(context.SensitiveValues) != 0 {
		t.Fatalf("request mapped internal fields: %#v", context)
	}
}
