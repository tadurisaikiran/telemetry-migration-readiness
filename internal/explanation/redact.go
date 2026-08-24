package explanation

import "regexp"

var redactionPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{
		pattern:     regexp.MustCompile(`(?is)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`),
		replacement: "[REDACTED PRIVATE KEY]",
	},
	{
		pattern:     regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`),
		replacement: "Bearer [REDACTED]",
	},
	{
		pattern:     regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
		replacement: "[REDACTED AWS ACCESS KEY]",
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key|authorization)(\s*[:=]\s*)([^\s,;]+)`),
		replacement: `${1}${2}[REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+:[^/@\s]+@`),
		replacement: `${1}[REDACTED]@`,
	},
}

// Redact removes common credential forms from text before it crosses the AI
// process boundary. It is defense in depth, not permission to send unrelated
// repository content.
func Redact(value string) string {
	for _, candidate := range redactionPatterns {
		value = candidate.pattern.ReplaceAllString(value, candidate.replacement)
	}
	return value
}
