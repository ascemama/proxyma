package transcriptor

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSSH validates and normalizes canonical ssh [options] [user@]host remote-command.
func ParseSSH(command string) (ParsedSSHCommand, *Error) {
	tokens, err := tokenize(command)
	if err != nil {
		return ParsedSSHCommand{}, invalidFormat(err.Error())
	}
	if len(tokens) == 0 {
		return ParsedSSHCommand{}, invalidFormat("command transcriptor cannot ingest an empty command")
	}
	if tokens[0].Text != "ssh" {
		return ParsedSSHCommand{}, invalidFormat("command transcriptor supports canonical ssh remote-command syntax only")
	}

	port := 22
	targetIndex := -1
	for i := 1; i < len(tokens); i++ {
		token := tokens[i].Text
		if token == "--" {
			if i+1 >= len(tokens) {
				return ParsedSSHCommand{}, invalidFormat("command transcriptor expected an ssh target after --")
			}
			targetIndex = i + 1
			break
		}
		if strings.HasPrefix(token, "-") {
			switch token {
			case "-o":
				if i+1 >= len(tokens) {
					return ParsedSSHCommand{}, invalidFormat("command transcriptor expected a value after -o")
				}
				i++
				continue
			case "-p":
				if i+1 >= len(tokens) {
					return ParsedSSHCommand{}, invalidFormat("command transcriptor expected a value after -p")
				}
				value, convErr := strconv.Atoi(tokens[i+1].Text)
				if convErr != nil || value <= 0 {
					return ParsedSSHCommand{}, invalidFormat("command transcriptor expected a positive port after -p")
				}
				port = value
				i++
				continue
			default:
				return ParsedSSHCommand{}, invalidFormat(fmt.Sprintf("command transcriptor cannot ingest ssh option %q", token))
			}
		}
		targetIndex = i
		break
	}

	if targetIndex < 0 || targetIndex >= len(tokens) {
		return ParsedSSHCommand{}, invalidFormat("command transcriptor expected an ssh target host")
	}

	targetToken := tokens[targetIndex]
	targetUser, targetHost := splitTarget(targetToken.Text)
	if strings.TrimSpace(targetHost) == "" {
		return ParsedSSHCommand{}, invalidFormat("command transcriptor expected an ssh target host")
	}

	remoteStart := targetToken.End
	if remoteStart >= len(command) {
		return ParsedSSHCommand{}, invalidFormat("command transcriptor requires a remote command payload")
	}
	remoteCommand := strings.TrimSpace(command[remoteStart:])
	if remoteCommand == "" {
		return ParsedSSHCommand{}, invalidFormat("command transcriptor requires a remote command payload")
	}

	return ParsedSSHCommand{
		TargetUserToken: targetUser,
		TargetHost:      targetHost,
		TargetPort:      port,
		RemoteCommand:   remoteCommand,
	}, nil
}

type token struct {
	Text  string
	Start int
	End   int
}

func tokenize(input string) ([]token, error) {
	var tokens []token
	for i := 0; i < len(input); {
		for i < len(input) && isSpace(input[i]) {
			i++
		}
		if i >= len(input) {
			break
		}
		start := i
		var b strings.Builder
		var quote byte
		for i < len(input) {
			ch := input[i]
			if quote == 0 && isSpace(ch) {
				break
			}
			if ch == '\\' && quote != '\'' && i+1 < len(input) {
				i++
				b.WriteByte(input[i])
				i++
				continue
			}
			if ch == '\'' || ch == '"' {
				if quote == 0 {
					quote = ch
					i++
					continue
				}
				if quote == ch {
					quote = 0
					i++
					continue
				}
			}
			b.WriteByte(ch)
			i++
		}
		if quote != 0 {
			return nil, fmt.Errorf("command transcriptor found an unmatched quote")
		}
		tokens = append(tokens, token{Text: b.String(), Start: start, End: i})
	}
	return tokens, nil
}

func splitTarget(target string) (string, string) {
	target = strings.TrimSpace(target)
	if at := strings.LastIndex(target, "@"); at >= 0 {
		return target[:at], target[at+1:]
	}
	return "", target
}

func isSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func invalidFormat(message string) *Error {
	return &Error{
		Code:      CodeUnsupportedCommandFormat,
		Retryable: true,
		Hint:      "submit canonical ssh [user@]host remote-command format",
		Message:   message,
	}
}
