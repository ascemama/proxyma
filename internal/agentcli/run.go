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

	"proxyma/internal/buildinfo"
)

func Run(args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("proxyma-agent", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	configPath := global.String("config", "", "path to harness v2 configuration")
	pretty := global.Bool("pretty", false, "indent JSON output")
	if err := global.Parse(args); err != nil {
		return writeFailure(stdout, "", cliError("invalid_usage", err.Error(), 2), *pretty)
	}
	remaining := global.Args()
	if len(remaining) == 0 {
		return writeFailure(stdout, "", cliError("invalid_usage", "a command is required", 2), *pretty)
	}
	command := remaining[0]
	commandArgs := remaining[1:]
	if command == "version" {
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
		if err = decodeInput(inputPath, &input); err == nil {
			envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Submit(ctx, input) })
		}
	case "resume":
		if len(commandArgs) != 0 {
			err = cliError("invalid_usage", "resume does not accept arguments", 2)
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
		if err = decodeInput(inputPath, &input); err == nil {
			envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Update(input) })
		}
	case "task-complete":
		inputPath, parseErr := parseInputFlag(command, commandArgs)
		if parseErr != nil {
			err = parseErr
			break
		}
		var input CompleteInput
		if err = decodeInput(inputPath, &input); err == nil {
			envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Complete(input) })
		}
	case "inspect":
		envelope, err = runInspect(ctx, service, commandArgs)
	case "doctor":
		if len(commandArgs) != 0 {
			err = cliError("invalid_usage", "doctor does not accept arguments", 2)
			break
		}
		envelope, err = service.WithStateLock(func() (Envelope, error) { return service.Doctor(ctx) })
	default:
		err = cliError("invalid_usage", fmt.Sprintf("unknown command %q", command), 2)
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
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return Envelope{}, cliError("invalid_usage", "inspect accepts --task or --target", 2)
	}
	if (*taskID == "") == (*targetValue == "") {
		return Envelope{}, cliError("invalid_usage", "inspect requires exactly one of --task or --target", 2)
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
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return "", cliError("invalid_usage", command+" accepts only --input", 2)
	}
	return *input, nil
}

func parseSingleFlag(command string, args []string, name string) (string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	value := flags.String(name, "", name)
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(*value) == "" {
		return "", cliError("invalid_usage", fmt.Sprintf("%s requires --%s", command, name), 2)
	}
	return *value, nil
}

func decodeInput(path string, target any) error {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = os.Stdin
	} else {
		opened, err := os.Open(path)
		if err != nil {
			return cliError("invalid_input", err.Error(), 2)
		}
		file = opened
		defer file.Close()
		reader = file
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return cliError("invalid_input", err.Error(), 2)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return cliError("invalid_input", "input must contain exactly one JSON object", 2)
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
		Error:           &SafeError{Code: typed.code, Message: typed.message, Retryable: typed.retryable, Hint: typed.hint},
	}
	writeEnvelope(output, envelope, pretty)
	return typed.exitCode
}

func writeEnvelope(output io.Writer, envelope Envelope, pretty bool) {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	_ = encoder.Encode(envelope)
}
