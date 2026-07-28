package tasks

import (
	"errors"
	"fmt"
	"strings"

	"proxyma/internal/targets"
	"proxyma/internal/transcriptor"
)

// ValidateCommandPolicy enforces Proxyma's canonical remote SSH policy.
func ValidateCommandPolicy(command string, target targets.Target) error {
	var targetHost, targetUser, remoteCommand string
	switch target.Transport {
	case targets.TransportSSH:
		parsed, err := transcriptor.ParseSSH(command)
		if err != nil {
			return errors.New(err.Error())
		}
		targetHost, targetUser, remoteCommand = parsed.TargetHost, parsed.TargetUserToken, parsed.RemoteCommand
	case targets.TransportWinRM:
		parsed, err := transcriptor.ParseWinRM(command)
		if err != nil {
			return errors.New(err.Error())
		}
		targetHost, targetUser, remoteCommand = parsed.TargetHost, parsed.TargetUserToken, parsed.PowerShell
	default:
		return fmt.Errorf("unsupported target transport %q", target.Transport)
	}
	if !target.MatchesTarget(targetHost) {
		return fmt.Errorf("command target host conflicts with matched target")
	}
	if targetUser != "" && targetUser != PlaceholderUser {
		return fmt.Errorf("command must use %s when a target username is supplied", PlaceholderUser)
	}
	if strings.TrimSpace(remoteCommand) == "" {
		return fmt.Errorf("command must include a remote command payload")
	}
	return nil
}
