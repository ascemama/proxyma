package policy

import (
	"fmt"
	"regexp"
	"strings"
)

var placeholderPatterns = map[string]string{
	"ipv4":          `(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])(?:\.(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])){3}`,
	"ipv6":          `[0-9A-Fa-f:.]+`,
	"hostname":      `[A-Za-z0-9][A-Za-z0-9.-]*`,
	"port":          `[0-9]{1,5}`,
	"path":          "[^\\s;&|<>$`()]+",
	"relative_path": "(?!/)[^\\s;&|<>$`()]+",
	"service":       `[A-Za-z0-9_.@+-]+`,
	"username":      `[A-Za-z0-9_.@+-]+`,
	"integer":       `-?[0-9]+`,
}

var shellOperatorPattern = regexp.MustCompile("[;&|<>$`()]")

// TemplateMatcher is the compiled representation of a typed command template.
type TemplateMatcher struct {
	pattern *regexp.Regexp
}

// CompileTemplate validates and compiles a typed command template.
func CompileTemplate(template string) (*TemplateMatcher, error) {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil, fmt.Errorf("template is empty")
	}
	if shellOperatorPattern.MatchString(template) {
		return nil, fmt.Errorf("shell operators are not allowed")
	}
	parts := strings.Fields(template)
	if len(parts) == 0 {
		return nil, fmt.Errorf("template is empty")
	}
	var pattern strings.Builder
	pattern.WriteString(`^`)
	for index, part := range parts {
		if index > 0 {
			pattern.WriteString(`\s+`)
		}
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			placeholder := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			if placeholder == "" || !validPlaceholderName(placeholder) {
				return nil, fmt.Errorf("invalid placeholder %q", part)
			}
			placeholderPattern, ok := placeholderPatterns[placeholder]
			if !ok {
				return nil, fmt.Errorf("unsupported placeholder %q", placeholder)
			}
			pattern.WriteString(`(?:` + placeholderPattern + `)`)
			continue
		}
		if strings.ContainsAny(part, "{}") {
			return nil, fmt.Errorf("placeholders must occupy a complete argument")
		}
		pattern.WriteString(regexp.QuoteMeta(part))
	}
	pattern.WriteString(`$`)
	compiled, err := regexp.Compile(pattern.String())
	if err != nil {
		return nil, err
	}
	return &TemplateMatcher{pattern: compiled}, nil
}

func validPlaceholderName(value string) bool {
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

// NormalizeTemplate gives equivalent whitespace-normalized templates a stable key.
func NormalizeTemplate(template string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(template)), " ")
}

// MustCompileTemplate compiles a validated persisted template.
func MustCompileTemplate(template string) *TemplateMatcher {
	matcher, err := CompileTemplate(template)
	if err != nil {
		panic(err)
	}
	return matcher
}

// Match reports whether command matches the complete template.
func (m *TemplateMatcher) Match(command string) bool {
	return m != nil && m.pattern.MatchString(strings.TrimSpace(command))
}
