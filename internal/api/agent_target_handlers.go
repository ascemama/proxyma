package api

import (
	"errors"
	"net/http"
	"strings"

	"proxyma/internal/targets"
	"proxyma/internal/tasks"
)

func (s *Server) handleResolveTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	requested, err := resolveTargetQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agent, ok := agentFromContext(r.Context())
	if !ok {
		writeAgentAuthError(w, "auth_required", "missing authenticated agent", false, "")
		return
	}
	matchedTarget, err := s.resolveRequestedTarget(&tasks.Target{Hostname: requested.Hostname, IP: requested.IP}, agent.ID)
	if err != nil {
		status, payload := errorResponse(err)
		writeJSON(w, status, payload)
		return
	}
	if !canSubmitCommand(agent, matchedTarget) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "agent is not authorized for target"})
		return
	}
	writeJSON(w, http.StatusOK, resolveTargetResponse(matchedTarget, requested))
}

func resolveTargetQuery(r *http.Request) (*CommandTargetRequest, error) {
	values := r.URL.Query()
	for key, items := range values {
		if key != "hostname" && key != "ip" {
			return nil, errors.New("unsupported target resolve query parameter")
		}
		if len(items) != 1 {
			return nil, errors.New("target resolve query parameters must not be repeated")
		}
	}
	requested := &CommandTargetRequest{
		Hostname: strings.TrimSpace(values.Get("hostname")),
		IP:       strings.TrimSpace(values.Get("ip")),
	}
	if requested.Hostname == "" && requested.IP == "" {
		return nil, errors.New("target is required")
	}
	return requested, nil
}

func resolveTargetResponse(target targets.Target, requested *CommandTargetRequest) ResolveTargetResponse {
	destination := strings.TrimSpace(target.IP)
	if requested != nil {
		if strings.TrimSpace(requested.IP) != "" {
			destination = strings.TrimSpace(requested.IP)
		} else if strings.TrimSpace(requested.Hostname) != "" {
			destination = strings.TrimSpace(requested.Hostname)
		}
	}
	if destination == "" {
		destination = strings.TrimSpace(target.Hostname)
	}
	payloadKind := "posix_shell"
	switch {
	case target.Transport == targets.TransportWinRM:
		payloadKind = "powershell"
	case target.OS == targets.OSWindows:
		payloadKind = "windows_shell"
	}
	return ResolveTargetResponse{
		Target: ResolvedTargetResponse{
			Transport: target.Transport,
			OS:        target.OS,
			Hostname:  target.Hostname,
			IP:        target.IP,
		},
		CanonicalCommand: CanonicalCommandHint{
			Prefix:      target.Transport + " " + tasks.PlaceholderUser + "@" + destination + " ",
			PayloadKind: payloadKind,
		},
	}
}

func (s *Server) matchRequestedTarget(target *tasks.Target, agentID string) (targets.Target, error) {
	return s.matchRequestedTargetWithOptions(target, agentID, true)
}

func (s *Server) resolveRequestedTarget(target *tasks.Target, agentID string) (targets.Target, error) {
	return s.matchRequestedTargetWithOptions(target, agentID, false)
}

func (s *Server) matchRequestedTargetWithOptions(target *tasks.Target, agentID string, revealUnauthorized bool) (targets.Target, error) {
	hostname := strings.TrimSpace(target.Hostname)
	ip := strings.TrimSpace(target.IP)

	var matched targets.Target
	hasMatch := false
	for _, candidate := range []struct {
		label string
		value string
	}{
		{label: "hostname", value: hostname},
		{label: "ip", value: ip},
	} {
		if candidate.value == "" {
			continue
		}
		target, found, duplicate := s.targetRegistry().MatchTargetForAgent(candidate.value, agentID)
		switch {
		case duplicate:
			return targets.Target{}, validationError("multiple targets match the submitted target " + candidate.label)
		case !found:
			if revealUnauthorized {
				if _, globallyFound, globallyDuplicate := s.targetRegistry().MatchTarget(candidate.value); globallyFound || globallyDuplicate {
					return targets.Target{}, forbiddenError("agent is not authorized for target")
				}
			}
			// Resolve is a discovery-sensitive endpoint when revealUnauthorized is
			// false. Do not reveal whether a target exists globally when this agent
			// is not authorized for it.
			return targets.Target{}, notFoundError("unknown target")
		case !hasMatch:
			matched = target
			hasMatch = true
		case !sameMatchedTarget(matched, target):
			return targets.Target{}, validationError("target hostname and ip identify different targets")
		}
	}
	if !hasMatch {
		return targets.Target{}, notFoundError("unknown target")
	}
	return matched, nil
}

func sameMatchedTarget(a, b targets.Target) bool {
	return strings.EqualFold(strings.TrimSpace(a.Hostname), strings.TrimSpace(b.Hostname)) &&
		strings.EqualFold(strings.TrimSpace(a.IP), strings.TrimSpace(b.IP))
}
