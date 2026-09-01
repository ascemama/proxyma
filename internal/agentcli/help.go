package agentcli

import "fmt"

func helpEnvelope(command string) (Envelope, error) {
	help, ok := helpFor(command)
	if !ok {
		return Envelope{}, usageError("", fmt.Sprintf("unknown command %q", command))
	}
	envelopeCommand := command
	if envelopeCommand == "" {
		envelopeCommand = "help"
	}
	return Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: envelopeCommand, Help: help}, nil
}

func helpFor(command string) (*HelpInfo, bool) {
	switch command {
	case "":
		return &HelpInfo{
			Usage:   "proxyma-agent [--config PATH] [--pretty] <command> [options]",
			Summary: "Operate durable Proxyma agent tasks through the harness v2 protocol.",
			Flags: []HelpFlag{
				{Name: "--config", Value: "PATH", Description: "Harness v2 configuration; must appear before the command."},
				{Name: "--pretty", Description: "Indent the JSON response; must appear before the command."},
				{Name: "--help, -h", Description: "Show this help without loading configuration."},
			},
			Commands: []HelpCommand{
				{Name: "resume", Summary: "Recover interrupted work and poll every open operation once."},
				{Name: "submit", Summary: "Create or extend a logical task from JSON input."},
				{Name: "poll", Summary: "Poll one logical task by client task ID."},
				{Name: "task-update", Summary: "Update agent-owned task fields from JSON input."},
				{Name: "task-complete", Summary: "Complete a logical task from JSON input."},
				{Name: "inspect", Summary: "Inspect a logical task or safely resolve a target."},
				{Name: "doctor", Summary: "Validate local configuration, state, credentials, and server capability."},
				{Name: "version", Summary: "Show CLI and protocol versions."},
				{Name: "help", Summary: "Show global or command-specific help."},
			},
			Notes: []string{"Run proxyma-agent <command> --help for accepted flags and input fields."},
		}, true
	case "help":
		return &HelpInfo{
			Usage:   "proxyma-agent help [command]",
			Summary: "Show global help or detailed help for one command.",
			Example: "proxyma-agent help task-complete",
		}, true
	case "version":
		return &HelpInfo{
			Usage:   "proxyma-agent version",
			Summary: "Return CLI version, commit, protocol version, and supported state schema.",
			Notes:   []string{"This command does not load harness configuration."},
		}, true
	case "resume":
		return &HelpInfo{
			Usage:   "proxyma-agent resume",
			Summary: "Recover valid journals, safely retry uncertain submissions, and poll all open operations once.",
			Notes:   []string{"Run this before starting new Proxyma work.", "The response contains only changed or actionable tasks plus unchanged counts."},
		}, true
	case "poll":
		return &HelpInfo{
			Usage:   "proxyma-agent poll --task CLIENT_TASK_ID",
			Summary: "Poll every non-final Proxyma operation for one logical client task.",
			Flags: []HelpFlag{
				{Name: "--task", Value: "CLIENT_TASK_ID", Required: true, Description: "Logical client_task_id returned by the CLI."},
			},
			Example: "proxyma-agent poll --task=c20e42a9-fb6f-4517-a201-639280b55444",
			Notes:   []string{"poll does not accept a revision; it always loads the latest local task revision.", "Do not use the Proxyma server task ID."},
		}, true
	case "inspect":
		return &HelpInfo{
			Usage:   "proxyma-agent inspect (--task CLIENT_TASK_ID | --target HOST_OR_IP)",
			Summary: "Read authoritative client task state or resolve safe target metadata without submission.",
			Flags: []HelpFlag{
				{Name: "--task", Value: "CLIENT_TASK_ID", Description: "Logical client task to read."},
				{Name: "--target", Value: "HOST_OR_IP", Description: "Hostname or IP to resolve safely."},
			},
			Notes: []string{"Supply exactly one of --task or --target."},
		}, true
	case "doctor":
		return &HelpInfo{
			Usage:   "proxyma-agent doctor",
			Summary: "Validate configuration, credentials, state recovery, locking, server health, and required capability.",
		}, true
	case "submit":
		return &HelpInfo{
			Usage:   "proxyma-agent submit --input FILE_OR_DASH",
			Summary: "Create or extend a logical task, persist intent, and submit one idempotent Proxyma operation.",
			Flags:   inputFlagHelp(),
			InputFields: []HelpField{
				{Name: "protocol_version", Type: "string", Description: "Optional; when present must be \"1\"."},
				{Name: "client_task_id", Type: "UUID string", Description: "Required only when extending an existing logical task."},
				{Name: "expected_revision", Type: "integer", Description: "Required with client_task_id; use the latest returned task revision."},
				{Name: "user_goal", Type: "string", Description: "Required for a new task."},
				{Name: "target", Type: "object", Required: true, Description: "Exactly one of hostname or ip."},
				{Name: "payload", Type: "string", Required: true, Description: "Remote payload only; never an SSH or WinRM wrapper."},
				{Name: "reasoning", Type: "string", Required: true, Description: "Why this operation is necessary."},
				{Name: "requested_by", Type: "string", Required: true, Description: "Harness identifier, for example codex or claude-code."},
				{Name: "metadata", Type: "object", Description: "Optional non-secret metadata."},
			},
			Example: map[string]any{
				"user_goal":    "Check uptime on the managed machine",
				"target":       map[string]any{"hostname": "server.example"},
				"payload":      "uptime",
				"reasoning":    "Answer the user's uptime question",
				"requested_by": "<harness-name>",
			},
			Notes: []string{"Use --input - to read exactly one JSON object from stdin.", "For an existing task, copy output revision into input expected_revision."},
		}, true
	case "task-update":
		return &HelpInfo{
			Usage:   "proxyma-agent task-update --input FILE_OR_DASH",
			Summary: "Update agent-owned logical task fields using optimistic revision control.",
			Flags:   inputFlagHelp(),
			InputFields: []HelpField{
				{Name: "protocol_version", Type: "string", Description: "Optional; when present must be \"1\"."},
				{Name: "client_task_id", Type: "UUID string", Required: true, Description: "Logical client task to update."},
				{Name: "expected_revision", Type: "integer", Required: true, Description: "Latest revision returned by the CLI."},
				{Name: "remaining_work", Type: "string", Description: "Updated description of work still required."},
				{Name: "status", Type: "string", Description: "Optional: pending, blocked, or failed."},
				{Name: "blocked_reason", Type: "string", Description: "Required with status blocked."},
				{Name: "required_next_action", Type: "string", Description: "Required with status blocked."},
				{Name: "failure_reason", Type: "string", Description: "Required with status failed."},
			},
			Example: map[string]any{"client_task_id": "c20e42a9-fb6f-4517-a201-639280b55444", "expected_revision": 5, "remaining_work": "Interpret the result"},
			Notes:   []string{"Use --input - to read exactly one JSON object from stdin.", "The field is expected_revision, not revision."},
		}, true
	case "task-complete":
		return &HelpInfo{
			Usage:   "proxyma-agent task-complete --input FILE_OR_DASH",
			Summary: "Record a final summary and complete a logical task when no operation is in flight.",
			Flags:   inputFlagHelp(),
			InputFields: []HelpField{
				{Name: "protocol_version", Type: "string", Description: "Optional; when present must be \"1\"."},
				{Name: "client_task_id", Type: "UUID string", Required: true, Description: "Logical client task to complete."},
				{Name: "expected_revision", Type: "integer", Required: true, Description: "Latest revision returned by the CLI."},
				{Name: "final_summary", Type: "string", Required: true, Description: "Final result that satisfies the user goal."},
			},
			Example: map[string]any{"client_task_id": "c20e42a9-fb6f-4517-a201-639280b55444", "expected_revision": 5, "final_summary": "The managed machine has been up for 48 minutes."},
			Notes:   []string{"Use --input - to read exactly one JSON object from stdin.", "The field is expected_revision, not revision.", "Direct --task, --revision, or --summary flags are not accepted."},
		}, true
	default:
		return nil, false
	}
}

func inputFlagHelp() []HelpFlag {
	return []HelpFlag{{Name: "--input", Value: "FILE_OR_DASH", Required: true, Description: "Strict JSON input file, or - for stdin; defaults to -."}}
}
