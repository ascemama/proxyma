package vmworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"proxyma/internal/buildinfo"
)

type cliOptions struct {
	db, proxyConfig, profile, input, worker, finding, step, operation, report, asset, inspectKind, output, backupDir string
	pretty, jsonOutput, actionRequired, confirmReset, force, checkProxyma                                            bool
	leaseSeconds                                                                                                     int
}

func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("vm-workflow", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	o := cliOptions{}
	flags.StringVar(&o.db, "db", os.Getenv("VM_WORKFLOW_DB"), "workflow SQLite database")
	flags.StringVar(&o.proxyConfig, "proxyma-config", "", "Proxyma client configuration")
	flags.StringVar(&o.profile, "profile", "", "import profile")
	flags.StringVar(&o.input, "input", "", "JSON or report input")
	flags.StringVar(&o.worker, "worker-id", "", "worker identity")
	flags.StringVar(&o.finding, "finding-key", "", "finding key")
	flags.StringVar(&o.step, "step-id", "", "step ID")
	flags.StringVar(&o.operation, "operation-id", "", "workflow operation ID")
	flags.StringVar(&o.report, "report-id", "", "report ID")
	flags.StringVar(&o.asset, "asset-id", "", "asset ID")
	flags.StringVar(&o.inspectKind, "kind", "finding", "inspect kind: finding, plan, operation, report, access, events")
	flags.StringVar(&o.output, "output", "", "output path")
	flags.StringVar(&o.backupDir, "backup-dir", "", "backup directory")
	flags.BoolVar(&o.pretty, "pretty", false, "pretty JSON")
	flags.BoolVar(&o.jsonOutput, "json", true, "JSON output")
	flags.BoolVar(&o.actionRequired, "action-required", false, "exit based on actionable work")
	flags.BoolVar(&o.confirmReset, "confirm-reset", false, "confirm destructive reset")
	flags.BoolVar(&o.force, "force", false, "override safety checks")
	flags.BoolVar(&o.checkProxyma, "check-proxyma", false, "check Proxyma during doctor")
	flags.IntVar(&o.leaseSeconds, "lease-seconds", 900, "lease duration")
	if err := flags.Parse(normalizeGlobalArgs(args)); err != nil {
		return writeError(stdout, "", err, o.pretty)
	}
	remaining := flags.Args()
	if len(remaining) == 0 {
		return writeError(stdout, "", fmt.Errorf("a command is required"), o.pretty)
	}
	command := remaining[0]
	sub := remaining[1:]
	if command == "version" {
		info := buildinfo.Current()
		return writeEnvelope(stdout, Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: command, Data: map[string]any{"version": info.Version, "commit": info.Commit, "schema_version": SchemaVersion}}, o.pretty)
	}
	if command == "help" {
		return writeEnvelope(stdout, Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: command, Data: map[string]any{"commands": []string{"version", "doctor", "status", "inspect", "import", "reconcile-access", "pulse", "ready", "claim", "lease-renew", "lease-release", "plan", "step", "operation", "finding", "backup", "restore", "reset"}}}, o.pretty)
	}
	db, err := OpenDatabase(o.db)
	if err != nil {
		return writeError(stdout, command, err, o.pretty)
	}
	defer db.Close()
	var proxy Proxyma
	needsProxy := command == "reconcile-access" || command == "pulse"
	if command == "operation" && len(sub) > 0 && sub[0] == "execute" {
		needsProxy = true
	}
	if command == "doctor" && o.checkProxyma {
		needsProxy = true
	}
	if needsProxy {
		proxy, err = newClientProxyma(o.proxyConfig)
		if err != nil {
			return writeError(stdout, command, err, o.pretty)
		}
	}
	service := NewService(db, proxy)
	ctx := context.Background()
	var data any
	action := ""
	switch command {
	case "import":
		if o.profile == "" || o.input == "" {
			return writeError(stdout, command, fmt.Errorf("import requires --profile and --input"), o.pretty)
		}
		data, err = ImportReport(ctx, db, o.profile, o.input)
	case "reconcile-access":
		data, err = service.ReconcileAccess(ctx)
	case "pulse":
		data, err = service.Pulse(ctx)
		if err == nil && o.actionRequired {
			actionData, _ := data.(map[string]any)
			if status, ok := actionData["status"].(StatusSummary); ok {
				if status.ActionRequired {
					action = "action_required"
				} else {
					action = "idle"
				}
			}
		}
	case "status", "ready":
		data, err = service.Status(ctx)
	case "claim":
		data, err = service.Claim(ctx, o.worker, time.Duration(o.leaseSeconds)*time.Second)
	case "lease-renew":
		data, err = nil, service.RenewLease(ctx, o.finding, o.worker, time.Duration(o.leaseSeconds)*time.Second)
	case "lease-release":
		data, err = nil, service.ReleaseLease(ctx, o.finding, o.worker)
	case "show":
		data, err = service.ShowFinding(ctx, o.finding)
	case "inspect":
		switch o.inspectKind {
		case "finding":
			data, err = service.ShowFinding(ctx, o.finding)
		case "plan":
			data, err = service.InspectPlan(ctx, o.finding)
		case "operation":
			data, err = service.InspectOperation(ctx, o.operation)
		case "report":
			data, err = service.InspectReport(ctx, o.report)
		case "access":
			data, err = service.InspectAccess(ctx, o.asset)
		case "events":
			data, err = service.InspectEvents(ctx, o.finding)
		default:
			err = fmt.Errorf("unsupported inspect kind %q", o.inspectKind)
		}
	case "plan":
		data, err = runPlan(ctx, service, sub, o.input, o.finding, o.worker)
	case "step":
		data, err = runStep(ctx, service, sub, o.step, o.input, o.worker)
	case "operation":
		data, err = runOperation(ctx, service, sub, o.input, o.operation, o.worker)
	case "finding":
		data, err = runFinding(ctx, service, sub, o.finding, o.input, o.worker)
	case "doctor":
		data, err = service.Doctor(ctx, o.checkProxyma)
	case "backup":
		data, err = db.Backup(ctx, o.output)
	case "restore":
		if o.input == "" {
			return writeError(stdout, command, fmt.Errorf("restore requires --input backup path"), o.pretty)
		}
		if err := db.Close(); err != nil {
			return writeError(stdout, command, err, o.pretty)
		}
		old, e := RestoreDatabase(ctx, o.db, o.input)
		data, err = map[string]any{"previous_database": old}, e
	case "reset":
		path, e := db.Reset(ctx, o.backupDir, o.confirmReset, o.force)
		data, err = map[string]any{"quarantined_database": path}, e
	default:
		return writeError(stdout, command, fmt.Errorf("unknown command %q", command), o.pretty)
	}
	if err != nil {
		return writeError(stdout, command, err, o.pretty)
	}
	code := writeEnvelope(stdout, Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: command, Action: action, Data: data}, o.pretty)
	if command == "pulse" && o.actionRequired && action == "idle" {
		return 1
	}
	return code
}

func normalizeGlobalArgs(args []string) []string {
	valueFlags := map[string]bool{"--db": true, "--proxyma-config": true, "--profile": true, "--input": true, "--worker-id": true, "--finding-key": true, "--step-id": true, "--operation-id": true, "--report-id": true, "--asset-id": true, "--kind": true, "--output": true, "--backup-dir": true, "--lease-seconds": true}
	boolFlags := map[string]bool{"--pretty": true, "--json": true, "--action-required": true, "--confirm-reset": true, "--force": true, "--check-proxyma": true}
	var globals, rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := arg
		if index := strings.IndexByte(arg, '='); index >= 0 {
			name = arg[:index]
		}
		if valueFlags[name] {
			globals = append(globals, arg)
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				globals = append(globals, args[i])
			}
			continue
		}
		if boolFlags[name] {
			globals = append(globals, arg)
			continue
		}
		rest = append(rest, arg)
	}
	return append(globals, rest...)
}

func runPlan(ctx context.Context, service *Service, args []string, inputPath, findingKey, worker string) (any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("plan action is required")
	}
	if args[0] != "create" && args[0] != "revise" && args[0] != "show" {
		return nil, fmt.Errorf("unsupported plan action")
	}
	if args[0] == "show" {
		return service.InspectPlan(ctx, findingKey)
	}
	var input PlanInput
	if err := readJSON(inputPath, &input); err != nil {
		return nil, err
	}
	if input.WorkerID == "" {
		input.WorkerID = worker
	}
	return service.PlanCreate(ctx, input, args[0] == "revise")
}
func runStep(ctx context.Context, service *Service, args []string, id, inputPath, worker string) (any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("step action is required")
	}
	var payload struct {
		ResultSummary        string `json:"result_summary"`
		VerificationEvidence string `json:"verification_evidence"`
	}
	if inputPath != "" {
		if err := readJSON(inputPath, &payload); err != nil {
			return nil, err
		}
	}
	return service.Step(ctx, args[0], id, payload.ResultSummary, payload.VerificationEvidence, worker)
}
func runOperation(ctx context.Context, service *Service, args []string, inputPath, operationID, worker string) (any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("operation action is required")
	}
	if args[0] == "show" {
		return service.InspectOperation(ctx, operationID)
	}
	if args[0] != "execute" {
		return nil, fmt.Errorf("unsupported operation action")
	}
	var input OperationInput
	if err := readJSON(inputPath, &input); err != nil {
		return nil, err
	}
	if input.WorkerID == "" {
		input.WorkerID = worker
	}
	return service.ExecuteOperation(ctx, input)
}
func runFinding(ctx context.Context, service *Service, args []string, key, inputPath, worker string) (any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("finding action is required")
	}
	var payload struct {
		Reason             string `json:"reason"`
		RequiredNextAction string `json:"required_next_action"`
	}
	if inputPath != "" {
		if err := readJSON(inputPath, &payload); err != nil {
			return nil, err
		}
	}
	return map[string]any{"finding_key": key, "status": args[0]}, service.FindingTransition(ctx, key, args[0], payload.Reason, payload.RequiredNextAction, worker)
}

func readJSON(path string, target any) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("--input is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}
func writeEnvelope(output io.Writer, value Envelope, pretty bool) int {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	_ = encoder.Encode(value)
	return 0
}
func writeError(output io.Writer, command string, err error, pretty bool) int {
	code, retryable := "internal_error", false
	if typed, ok := err.(*workflowError); ok {
		code, retryable = typed.Code, typed.Retryable
	}
	if errors.Is(err, os.ErrNotExist) {
		code = "not_found"
	}
	returnCode := 2
	if code == "invalid_input" || code == "config_error" {
		returnCode = 2
	}
	if code == "no_work" {
		returnCode = 1
	}
	_ = writeEnvelope(output, Envelope{ProtocolVersion: ProtocolVersion, OK: false, Command: command, Error: &SafeError{Code: code, Message: err.Error(), Retryable: retryable}}, pretty)
	return returnCode
}
