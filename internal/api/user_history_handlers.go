package api

import (
	"fmt"
	"net/http"
	"net/url"
	"proxyma/internal/agents"
	"proxyma/internal/interaction/history"
	"proxyma/internal/targets"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleUserHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	if s.InteractionHistory == nil {
		writeError(w, http.StatusInternalServerError, "history store is not configured")
		return
	}
	query, err := userHistoryQuery(r, user.ID, user.Admin, s.agentRegistry(), s.targetRegistry())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_history_query"})
		return
	}
	page, err := s.InteractionHistory.ListHistory(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_history_query"})
		return
	}
	writeJSON(w, http.StatusOK, UserHistoryResponse{Items: page.Items, NextCursor: page.NextCursor})
}

func userHistoryQuery(r *http.Request, userID string, admin bool, agentRegistry agents.Registry, targetRegistry targets.Registry) (history.HistoryQuery, error) {
	values := r.URL.Query()
	limit, err := interactionHistoryLimit(values)
	if err != nil {
		return history.InteractionQuery{}, err
	}
	entryType := strings.TrimSpace(values.Get("entry_type"))
	if entryType != "" && entryType != history.EntryTypeCommand && entryType != history.EntryTypeInteraction {
		return history.HistoryQuery{}, fmt.Errorf("entry_type must be command or interaction")
	}
	query := history.HistoryQuery{
		Visibility:            history.VisibilityScope{Admin: admin},
		EntryType:             entryType,
		AgentID:               strings.TrimSpace(values.Get("agent_id")),
		InteractionInstanceID: strings.TrimSpace(values.Get("interaction_instance_id")),
		Status:                commaValues(values.Get("status")),
		Kind:                  commaValues(values.Get("kind")),
		Target:                strings.TrimSpace(values.Get("target")),
		Text:                  strings.TrimSpace(values.Get("q")),
		CommandPattern:        strings.TrimSpace(values.Get("command_pattern")),
		CommandPatternMode:    strings.TrimSpace(values.Get("command_pattern_mode")),
		Limit:                 limit,
		Cursor:                strings.TrimSpace(values.Get("cursor")),
	}
	if query.CommandPatternMode == "" {
		query.CommandPatternMode = history.CommandPatternLiteral
	}
	switch query.CommandPatternMode {
	case history.CommandPatternLiteral, history.CommandPatternGlob, history.CommandPatternRegex:
	default:
		return history.HistoryQuery{}, fmt.Errorf("command_pattern_mode must be literal, glob, or regex")
	}
	if admin {
		query.InteractionInstanceCreatorUserID = strings.TrimSpace(values.Get("interaction_instance_creator_user_id"))
	} else if strings.TrimSpace(values.Get("interaction_instance_creator_user_id")) != "" {
		return history.InteractionQuery{}, fmt.Errorf("interaction_instance_creator_user_id is admin-only")
	}
	if query.AgentID != "" && !admin {
		agent, ok := agentRegistry.Find(query.AgentID)
		if !ok || !containsUserID(agent.UserIDs, userID) {
			return history.InteractionQuery{}, fmt.Errorf("agent_id is not visible to this user")
		}
	}
	if !admin {
		for _, agent := range agentRegistry.All() {
			if containsUserID(agent.UserIDs, userID) {
				query.Visibility.AgentIDs = append(query.Visibility.AgentIDs, agent.ID)
			}
		}
		for _, target := range targetRegistry.All() {
			if containsUserID(target.UserIDs, userID) {
				query.Visibility.TargetIDs = append(query.Visibility.TargetIDs, target.ID)
			}
		}
	}
	if raw := strings.TrimSpace(values.Get("created_from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return history.InteractionQuery{}, fmt.Errorf("created_from must be RFC3339")
		}
		query.CreatedFrom = &parsed
	}
	if raw := strings.TrimSpace(values.Get("created_to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return history.InteractionQuery{}, fmt.Errorf("created_to must be RFC3339")
		}
		query.CreatedTo = &parsed
	}
	if query.CreatedFrom != nil && query.CreatedTo != nil && query.CreatedFrom.After(*query.CreatedTo) {
		return history.InteractionQuery{}, fmt.Errorf("created_from must be before created_to")
	}
	if query.CommandPatternMode == history.CommandPatternRegex && query.CommandPattern != "" && query.AgentID == "" && (query.CreatedFrom == nil || query.CreatedTo == nil) {
		return history.InteractionQuery{}, fmt.Errorf("regex command search requires agent_id or a created_from/created_to interval")
	}
	return query, nil
}

func interactionHistoryLimit(values url.Values) (int, error) {
	raw := strings.TrimSpace(values.Get("limit"))
	if raw == "" {
		return history.DefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, strconv.ErrSyntax
	}
	return history.NormalizeLimit(limit), nil
}

func commaValues(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func containsUserID(values []string, userID string) bool {
	userID = strings.TrimSpace(userID)
	for _, value := range values {
		if strings.TrimSpace(value) == userID {
			return true
		}
	}
	return false
}
