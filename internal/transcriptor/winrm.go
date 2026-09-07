package transcriptor

import "strings"

// ParseWinRM validates canonical winrm [user@]host PowerShell-payload syntax.
func ParseWinRM(command string) (ParsedWinRMCommand, *Error) {
	first, firstEnd := nextWord(command, 0)
	if first != "winrm" {
		return ParsedWinRMCommand{}, invalidWinRM("command transcriptor supports canonical winrm remote-command syntax only")
	}
	target, targetEnd := nextWord(command, firstEnd)
	if target == "" {
		return ParsedWinRMCommand{}, invalidWinRM("command transcriptor expected a WinRM target host")
	}
	user, host := splitTarget(target)
	if strings.TrimSpace(host) == "" {
		return ParsedWinRMCommand{}, invalidWinRM("command transcriptor expected a WinRM target host")
	}
	payload := strings.TrimSpace(command[targetEnd:])
	if payload == "" {
		return ParsedWinRMCommand{}, invalidWinRM("command transcriptor requires a PowerShell payload")
	}
	return ParsedWinRMCommand{TargetUserToken: user, TargetHost: host, PowerShell: payload}, nil
}

func nextWord(input string, start int) (string, int) {
	for start < len(input) && isSpace(input[start]) {
		start++
	}
	end := start
	for end < len(input) && !isSpace(input[end]) {
		end++
	}
	return input[start:end], end
}

func invalidWinRM(message string) *Error {
	return &Error{
		Code:      CodeUnsupportedCommandFormat,
		Retryable: true,
		Hint:      "submit canonical winrm {user}@host PowerShell-payload format",
		Message:   message,
	}
}
