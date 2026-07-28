package tasks

import "strings"

const redactedCredential = "[REDACTED]"

// RedactCredentialValues removes known credential values from Proxyma-generated text.
func RedactCredentialValues(text string, values ...string) string {
	redacted := text
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		redacted = strings.ReplaceAll(redacted, value, redactedCredential)
	}
	return redacted
}
