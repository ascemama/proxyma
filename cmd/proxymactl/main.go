package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"proxyma/internal/agents"
	"proxyma/internal/config"
	"proxyma/internal/targets"
	"proxyma/internal/users"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Stdin, os.LookupEnv))
}

func run(args []string, stdout io.Writer, stderr io.Writer, stdin io.Reader, lookupEnv func(string) (string, bool)) int {
	globalArgs, commandArgs, err := splitGlobalArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if len(commandArgs) < 2 {
		fmt.Fprintln(stderr, "error: expected resource and action")
		return 2
	}
	if commandArgs[0] == "certificates" || commandArgs[0] == "config" {
		result, err := dispatchTransportCommand(commandArgs)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	cfg, err := config.Load(globalArgs, lookupEnv)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	paths := users.Paths{
		UserDir:   cfg.UserDir,
		AgentDir:  cfg.AgentDir,
		TargetDir: cfg.TargetDir,
		PolicyDir: cfg.PolicyDir,
	}
	result, err := dispatch(paths, commandArgs, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func dispatch(paths users.Paths, args []string, stdin io.Reader) (users.MutationResult, error) {
	resource := args[0]
	action := args[1]
	rest := args[2:]

	switch resource {
	case "users":
		return dispatchUsers(paths, action, rest, stdin)
	case "agents":
		return dispatchAgents(paths, action, rest, stdin)
	case "targets":
		return dispatchTargets(paths, action, rest, stdin)
	default:
		return users.MutationResult{}, fmt.Errorf("unsupported resource %q", resource)
	}
}

func dispatchUsers(paths users.Paths, action string, args []string, stdin io.Reader) (users.MutationResult, error) {
	switch action {
	case "create":
		file, passwordStdin, err := createUserArgs(args)
		if err != nil {
			return users.MutationResult{}, err
		}
		if passwordStdin && file == "-" {
			return users.MutationResult{}, fmt.Errorf("users create cannot use --file - with --password-stdin")
		}
		var user users.User
		fields, err := readPayloadFields(file, stdin, &user)
		if err != nil {
			return users.MutationResult{}, err
		}
		if !fields["admin"] {
			return users.MutationResult{}, fmt.Errorf("users create requires admin")
		}
		if passwordStdin {
			password, err := readPassword(stdin)
			if err != nil {
				return users.MutationResult{}, err
			}
			user, err = users.ResetPassword(user, password)
			if err != nil {
				return users.MutationResult{}, err
			}
		} else if !fields["passwd_hash"] {
			return users.MutationResult{}, fmt.Errorf("users create requires passwd_hash or --password-stdin")
		}
		return users.CreateUser(paths, user, nil)
	case "update":
		id, file, err := updateArgs("users update", args)
		if err != nil {
			return users.MutationResult{}, err
		}
		var user users.User
		fields, err := readPayloadFields(file, stdin, &user)
		if err != nil {
			return users.MutationResult{}, err
		}
		if !fields["admin"] {
			return users.MutationResult{}, fmt.Errorf("users update requires admin")
		}
		if fields["passwd_hash"] && strings.TrimSpace(user.PasswdHash) == "" {
			return users.MutationResult{}, fmt.Errorf("passwd_hash is empty")
		}
		return users.UpdateUser(paths, id, user)
	case "reset-password":
		id, err := resetPasswordArgs(args)
		if err != nil {
			return users.MutationResult{}, err
		}
		password, err := readPassword(stdin)
		if err != nil {
			return users.MutationResult{}, err
		}
		return users.ResetUserPassword(paths, id, password)
	case "delete":
		id, err := deleteArg("users delete", args)
		if err != nil {
			return users.MutationResult{}, err
		}
		return users.DeleteUser(paths, id)
	default:
		return users.MutationResult{}, fmt.Errorf("unsupported users action %q", action)
	}
}

func dispatchAgents(paths users.Paths, action string, args []string, stdin io.Reader) (users.MutationResult, error) {
	switch action {
	case "create":
		file, err := createFileArg("agents create", args)
		if err != nil {
			return users.MutationResult{}, err
		}
		var agent agents.Agent
		if err := readPayload(file, stdin, &agent); err != nil {
			return users.MutationResult{}, err
		}
		return users.CreateAgent(paths, agent, nil)
	case "update":
		id, file, err := updateArgs("agents update", args)
		if err != nil {
			return users.MutationResult{}, err
		}
		var agent agents.Agent
		if err := readPayload(file, stdin, &agent); err != nil {
			return users.MutationResult{}, err
		}
		return users.UpdateAgent(paths, id, agent)
	case "delete":
		id, err := deleteArg("agents delete", args)
		if err != nil {
			return users.MutationResult{}, err
		}
		return users.DeleteAgent(paths, id)
	default:
		return users.MutationResult{}, fmt.Errorf("unsupported agents action %q", action)
	}
}

func dispatchTargets(paths users.Paths, action string, args []string, stdin io.Reader) (users.MutationResult, error) {
	switch action {
	case "create":
		file, err := createFileArg("targets create", args)
		if err != nil {
			return users.MutationResult{}, err
		}
		var target targets.Target
		if err := readPayload(file, stdin, &target); err != nil {
			return users.MutationResult{}, err
		}
		return users.CreateTarget(paths, target, nil)
	case "update":
		id, file, err := updateArgs("targets update", args)
		if err != nil {
			return users.MutationResult{}, err
		}
		var target targets.Target
		if err := readPayload(file, stdin, &target); err != nil {
			return users.MutationResult{}, err
		}
		return users.UpdateTarget(paths, id, target)
	case "delete":
		id, err := deleteArg("targets delete", args)
		if err != nil {
			return users.MutationResult{}, err
		}
		return users.DeleteTarget(paths, id)
	default:
		return users.MutationResult{}, fmt.Errorf("unsupported targets action %q", action)
	}
}

func createUserArgs(args []string) (string, bool, error) {
	fs := flag.NewFlagSet("users create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "JSON input file, or - for stdin")
	passwordStdin := fs.Bool("password-stdin", false, "read initial password from stdin")
	if err := fs.Parse(args); err != nil {
		return "", false, err
	}
	if fs.NArg() != 0 {
		return "", false, fmt.Errorf("users create does not accept positional arguments")
	}
	if strings.TrimSpace(*file) == "" {
		return "", false, fmt.Errorf("users create requires --file")
	}
	return *file, *passwordStdin, nil
}

func createFileArg(name string, args []string) (string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "JSON input file, or - for stdin")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() != 0 {
		return "", fmt.Errorf("%s does not accept positional arguments", name)
	}
	if strings.TrimSpace(*file) == "" {
		return "", fmt.Errorf("%s requires --file", name)
	}
	return *file, nil
}

func updateArgs(name string, args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("%s requires id", name)
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		return "", "", fmt.Errorf("%s requires id", name)
	}
	file, err := createFileArg(name, args[1:])
	return id, file, err
}

func deleteArg(name string, args []string) (string, error) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("%s requires exactly one id", name)
	}
	return strings.TrimSpace(args[0]), nil
}

func resetPasswordArgs(args []string) (string, error) {
	var id string
	passwordStdin := false
	for _, arg := range args {
		switch {
		case arg == "--password-stdin":
			passwordStdin = true
		case strings.HasPrefix(arg, "-"):
			return "", fmt.Errorf("unsupported users reset-password flag %q", arg)
		case id == "":
			id = strings.TrimSpace(arg)
		default:
			return "", fmt.Errorf("users reset-password requires exactly one id")
		}
	}
	if !passwordStdin {
		return "", fmt.Errorf("users reset-password requires --password-stdin")
	}
	if id == "" {
		return "", fmt.Errorf("users reset-password requires exactly one id")
	}
	return id, nil
}

func readPassword(stdin io.Reader) ([]byte, error) {
	password, err := io.ReadAll(stdin)
	if err != nil {
		return nil, err
	}
	if len(password) > 0 && password[len(password)-1] == '\n' {
		password = password[:len(password)-1]
		if len(password) > 0 && password[len(password)-1] == '\r' {
			password = password[:len(password)-1]
		}
	}
	if len(password) == 0 {
		return nil, fmt.Errorf("password is required")
	}
	return password, nil
}

func readPayload(path string, stdin io.Reader, target any) error {
	_, err := readPayloadFields(path, stdin, target)
	return err
}

func readPayloadFields(path string, stdin io.Reader, target any) (map[string]bool, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, ok := raw["id"]; ok {
		return nil, fmt.Errorf("input JSON must not include id")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	fields := make(map[string]bool, len(raw))
	for field := range raw {
		fields[field] = true
	}
	return fields, nil
}

func splitGlobalArgs(args []string) ([]string, []string, error) {
	var global []string
	for i := 0; i < len(args); {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			return global, args[i:], nil
		}
		if arg == "--no-config" {
			global = append(global, arg)
			i++
			continue
		}
		if strings.Contains(arg, "=") {
			global = append(global, arg)
			i++
			continue
		}
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("%s requires a value", arg)
		}
		global = append(global, arg, args[i+1])
		i += 2
	}
	return global, nil, nil
}
