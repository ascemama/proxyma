package agentcli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"proxyma/internal/buildinfo"
)

func Run(args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("proxyma-agent", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	configPath := global.String("config", "", "path to harness v2 configuration")
	pretty := global.Bool("pretty", false, "indent JSON output")
	if err := global.Parse(args); err != nil {
		if err == flag.ErrHelp {
			envelope, _ := helpEnvelope("")
			writeEnvelope(stdout, envelope, *pretty)
			return 0
		}
		return writeFailure(stdout, "", usageError("", err.Error()), *pretty)
	}
	remaining := global.Args()
	if len(remaining) == 0 {
		return writeFailure(stdout, "", usageError("", "a command is required"), *pretty)
	}
	command := remaining[0]
	commandArgs := remaining[1:]
	if isHelpRequest(commandArgs) {
		envelope, helpErr := helpEnvelope(command)
		if helpErr != nil {
			return writeFailure(stdout, command, helpErr, *pretty)
		}
		writeEnvelope(stdout, envelope, *pretty)
		return 0
	}
	if command == "help" {
		if len(commandArgs) > 1 {
			return writeFailure(stdout, command, usageError("help", "help accepts at most one command name"), *pretty)
		}
		helpCommand := ""
		if len(commandArgs) == 1 {
			helpCommand = commandArgs[0]
		}
		envelope, helpErr := helpEnvelope(helpCommand)
		if helpErr != nil {
			return writeFailure(stdout, command, helpErr, *pretty)
		}
		writeEnvelope(stdout, envelope, *pretty)
		return 0
	}
	if command == "version" {
		if len(commandArgs) != 0 {
			return writeFailure(stdout, command, usageError(command, "version does not accept arguments"), *pretty)
		}
		info := buildinfo.Current()
		writeEnvelope(stdout, Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: "version", Version: info.Version, Commit: info.Commit, StateSchema: StateVersion}, *pretty)
		return 0
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return writeFailure(stdout, command, &commandError{code: "config_error", message: err.Error(), exitCode: 2}, *pretty)
	}
	service := NewService(cfg)
	ctx := context.Background()
	var envelope Envelope
	switch command {
	case "submit":
		inputPath, parseErr := parseInputFlag(command, commandArgs)
		if parseErr != nil {
			err = parseErr
			break
		}
		var input SubmitInput
		if err = decodeInput(command, inputPath, &input); err == nil {
			envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Submit(ctx, input) })
		}
	case "resume":
		if len(commandArgs) != 0 {
			err = usageError(command, "resume does not accept arguments")
			break
		}
		envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Resume(ctx) })
	case "poll":
		id, parseErr := parseSingleFlag(command, commandArgs, "task")
		if parseErr != nil {
			err = parseErr
			break
		}
		envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Poll(ctx, id) })
	case "task-update":
		inputPath, parseErr := parseInputFlag(command, commandArgs)
		if parseErr != nil {
			err = parseErr
			break
		}
		var input UpdateInput
		if err = decodeInput(command, inputPath, &input); err == nil {
			envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Update(input) })
		}
	case "task-complete":
		inputPath, parseErr := parseInputFlag(command, commandArgs)
		if parseErr != nil {
			err = parseErr
			break
		}
		var input CompleteInput
		if err = decodeInput(command, inputPath, &input); err == nil {
			envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Complete(input) })
		}
	case "inspect":
		envelope, err = runInspect(ctx, service, commandArgs)
	case "doctor":
		if len(commandArgs) != 0 {
			err = usageError(command, "doctor does not accept arguments")
			break
		}
		envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Doctor(ctx) })
	default:
		err = usageError("", fmt.Sprintf("unknown command %q", command))
	}
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return writeFailure(stdout, command, err, *pretty)
	}
	writeEnvelope(stdout, envelope, *pretty)
	return 0
}

func runInspect(ctx context.Context, service *Service, args []string) (Envelope, error) {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	taskID := flags.String("task", "", "client task ID")
	targetValue := flags.String("target", "", "target hostname or IP")
	if err := flags.Parse(args); err != nil {
		return Envelope{}, usageError("inspect", "inspect: "+err.Error())
	}
	if flags.NArg() != 0 {
		return Envelope{}, usageError("inspect", "inspect received unexpected positional arguments")
	}
	if (*taskID == "") == (*targetValue == "") {
		return Envelope{}, usageError("inspect", "inspect requires exactly one of --task or --target")
	}
	if *taskID != "" {
		return service.WithStateLock(func() (Envelope, error) { return service.InspectTask(*taskID) })
	}
	target := Target{Hostname: *targetValue}
	if net.ParseIP(*targetValue) != nil {
		target = Target{IP: *targetValue}
	}
	return service.InspectTarget(ctx, target)
}

func parseInputFlag(command string, args []string) (string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	input := flags.String("input", "-", "JSON input file or - for stdin")
	if err := flags.Parse(args); err != nil {
		return "", usageError(command, command+": "+err.Error())
	}
	if flags.NArg() != 0 {
		return "", usageError(command, command+" accepts only --input FILE_OR_DASH")
	}
	return *input, nil
}

func parseSingleFlag(command string, args []string, name string) (string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	value := flags.String(name, "", name)
	if err := flags.Parse(args); err != nil {
		return "", usageError(command, command+": "+err.Error())
	}
	if flags.NArg() != 0 {
		return "", usageError(command, command+" received unexpected positional arguments")
	}
	if strings.TrimSpace(*value) == "" {
		return "", usageError(command, fmt.Sprintf("%s requires --%s", command, name))
	}
	return *value, nil
}

func decodeInput(command, path string, target any) error {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = os.Stdin
	} else {
		opened, err := os.Open(path)
		if err != nil {
			return inputError(command, err.Error())
		}
		file = opened
		defer file.Close()
		reader = file
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return inputError(command, err.Error())
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return inputError(command, "input must contain exactly one JSON object")
	}
	return nil
}

func writeFailure(output io.Writer, command string, err error, pretty bool) int {
	typed, ok := err.(*commandError)
	if !ok {
		typed = &commandError{code: "internal_error", message: err.Error(), exitCode: 1}
	}
	envelope := Envelope{
		ProtocolVersion: ProtocolVersion,
		OK:              false,
		Command:         command,
		Task:            typed.task,
		Error:           &SafeError{Code: typed.code, Message: typed.message, Retryable: typed.retryable, Hint: typed.hint, At: time.Now().UTC()},
	}
	writeEnvelope(output, envelope, pretty)
	return typed.exitCode
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h")
}

func usageError(command, message string) *commandError {
	hint := "Run \"proxyma-agent --help\" for usage."
	if command != "" {
		hint = fmt.Sprintf("Run \"proxyma-agent %s --help\" for usage and examples.", command)
	}
	return &commandError{code: "invalid_usage", message: message, hint: hint, exitCode: 2}
}

func inputError(command, message string) *commandError {
	return &commandError{
		code:     "invalid_input",
		message:  message,
		hint:     fmt.Sprintf("Run \"proxyma-agent %s --help\" for the accepted JSON fields and an example.", command),
		exitCode: 2,
	}
}

func writeEnvelope(output io.Writer, envelope Envelope, pretty bool) {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	_ = encoder.Encode(envelope)
}
