package sqlite

import (
	"fmt"
	"regexp"
	"strings"

	"proxyma/internal/interaction/history"
)

type historyQueryPlan struct {
	sql          string
	args         []any
	postFilter   func(history.HistoryItem) bool
	needsAllRows bool
}

func buildHistoryQuery(query history.HistoryQuery) (historyQueryPlan, error) {
	parts := []string{"1 = 1"}
	args := make([]any, 0)
	if !query.Visibility.Admin {
		visibility := make([]string, 0, len(query.Visibility.AgentIDs)+len(query.Visibility.TargetIDs))
		if len(query.Visibility.AgentIDs) > 0 {
			visibility = append(visibility, "agent_id IN ("+placeholders(len(query.Visibility.AgentIDs))+")")
			for _, id := range query.Visibility.AgentIDs {
				args = append(args, id)
			}
		}
		if len(query.Visibility.TargetIDs) > 0 {
			visibility = append(visibility, "target_id IN ("+placeholders(len(query.Visibility.TargetIDs))+")")
			for _, id := range query.Visibility.TargetIDs {
				args = append(args, id)
			}
		}
		if len(visibility) == 0 {
			parts = append(parts, "1 = 0")
		} else {
			parts = append(parts, "("+strings.Join(visibility, " OR ")+")")
		}
	}
	if query.EntryType != "" {
		parts = append(parts, "entry_type = ?")
		args = append(args, query.EntryType)
	}
	if query.AgentID != "" {
		parts = append(parts, "agent_id = ?")
		args = append(args, query.AgentID)
	}
	if query.InteractionInstanceID != "" {
		parts = append(parts, "interaction_instance_id = ?")
		args = append(args, query.InteractionInstanceID)
	}
	if query.InteractionInstanceCreatorUserID != "" {
		parts = append(parts, "interaction_instance_creator_user_id = ?")
		args = append(args, query.InteractionInstanceCreatorUserID)
	}
	if len(query.Status) > 0 {
		parts = append(parts, "status IN ("+placeholders(len(query.Status)+0)+")")
		for _, value := range query.Status {
			args = append(args, value)
		}
	}
	if len(query.Kind) > 0 {
		parts = append(parts, "kind IN ("+placeholders(len(query.Kind))+")")
		for _, value := range query.Kind {
			args = append(args, value)
		}
	}
	if query.Target != "" {
		pattern := "%" + escapeLike(query.Target) + "%"
		parts = append(parts, "(target_hostname LIKE ? ESCAPE '\\' OR target_ip LIKE ? ESCAPE '\\' OR target_id LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern, pattern)
	}
	if query.CreatedFrom != nil {
		parts = append(parts, "created_at >= ?")
		args = append(args, query.CreatedFrom.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	}
	if query.CreatedTo != nil {
		parts = append(parts, "created_at <= ?")
		args = append(args, query.CreatedTo.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	}
	if query.Text != "" {
		parts = append(parts, "((entry_type = 'command' AND EXISTS (SELECT 1 FROM commands_fts WHERE commands_fts.task_id = history_id AND commands_fts MATCH ?)) OR (entry_type = 'interaction' AND EXISTS (SELECT 1 FROM interactions_fts WHERE interactions_fts.interaction_id = history_id AND interactions_fts MATCH ?)))")
		args = append(args, ftsQuery(query.Text), ftsQuery(query.Text))
	}
	var postFilter func(history.HistoryItem) bool
	needsAllRows := false
	if query.CommandPattern != "" {
		switch query.CommandPatternMode {
		case "", history.CommandPatternLiteral:
			parts = append(parts, "(entry_type <> 'command' OR command = ?)")
			args = append(args, query.CommandPattern)
		case history.CommandPatternGlob:
			parts = append(parts, "(entry_type <> 'command' OR command LIKE ? ESCAPE '\\')")
			args = append(args, globToLike(query.CommandPattern))
		case history.CommandPatternRegex:
			expression, err := regexp.Compile(query.CommandPattern)
			if err != nil {
				return historyQueryPlan{}, fmt.Errorf("invalid command regex: %w", err)
			}
			postFilter = func(item history.HistoryItem) bool {
				return item.EntryType != history.EntryTypeCommand || expression.MatchString(item.Command)
			}
			needsAllRows = true
		default:
			return historyQueryPlan{}, fmt.Errorf("unsupported command pattern mode %q", query.CommandPatternMode)
		}
	}
	if cursor, err := history.DecodeCursor(query.Cursor); err != nil {
		return historyQueryPlan{}, err
	} else if query.Cursor != "" {
		parts = append(parts, "(created_at < ? OR (created_at = ? AND history_id < ?))")
		created := cursor.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		args = append(args, created, created, cursor.HistoryID)
	}
	limit := history.NormalizeLimit(query.Limit)
	if !needsAllRows {
		args = append(args, limit+1)
	}
	limitSQL := ""
	if !needsAllRows {
		limitSQL = " LIMIT ?"
	}
	sqlText := `WITH history AS (
		SELECT 'command' AS entry_type, c.task_id AS history_id, '' AS interaction_id,
			c.task_id, c.agent_id, c.agent_name, c.interaction_instance_id, '' AS interaction_instance_creator_user_id,
			'' AS provider, '' AS kind, '' AS expected_response, c.status, '' AS subject, '' AS message,
			c.harness_name, NULL AS approved, '' AS answer, '' AS responded_by, '' AS cancel_reason,
			c.target_id, c.target_hostname, c.target_ip, c.command, c.result_class AS result,
			c.policy_justification, COALESCE(c.completed_at, c.created_at) AS created_at,
			c.completed_at, NULL AS expires_at, NULL AS responded_at, c.created_at AS task_created_at
		FROM commands c
		UNION ALL
		SELECT 'interaction', i.interaction_id, i.interaction_id, i.task_id,
			COALESCE(c.agent_id, ''), COALESCE(c.agent_name, ''), i.interaction_instance_id,
			i.interaction_instance_creator_user_id, i.provider, i.kind, i.expected_response, i.status,
			i.subject, i.message, i.harness_name, i.approved, i.answer, i.responded_by, i.cancel_reason,
			COALESCE(c.target_id, ''), COALESCE(c.target_hostname, ''), COALESCE(c.target_ip, ''),
			'', '', '', i.created_at, NULL, i.expires_at, i.responded_at, COALESCE(c.created_at, i.created_at)
		FROM interactions i
		LEFT JOIN commands c ON c.task_id = i.task_id
	)
	SELECT entry_type, history_id, interaction_id, task_id, agent_id, agent_name,
		interaction_instance_id, interaction_instance_creator_user_id, provider, kind, expected_response,
		status, subject, message, harness_name, approved, answer, responded_by, cancel_reason,
		target_id, target_hostname, target_ip, command, result, policy_justification,
		created_at, completed_at, expires_at, responded_at, task_created_at
	FROM history
	WHERE ` + strings.Join(parts, " AND ") + `
	ORDER BY created_at DESC, history_id DESC` + limitSQL
	return historyQueryPlan{sql: sqlText, args: args, postFilter: postFilter, needsAllRows: needsAllRows}, nil
}

func placeholders(count int) string {
	if count <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	return strings.ReplaceAll(value, "_", `\_`)
}

func globToLike(value string) string {
	var builder strings.Builder
	for _, character := range value {
		switch character {
		case '*':
			builder.WriteByte('%')
		case '?':
			builder.WriteByte('_')
		default:
			builder.WriteString(escapeLike(string(character)))
		}
	}
	return builder.String()
}

func ftsQuery(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
