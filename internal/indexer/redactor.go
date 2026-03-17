package indexer

import (
	"regexp"
	"strings"
)

type Redactor struct {
	tokenPatterns []*regexp.Regexp
	sshKeyPath    *regexp.Regexp
	urlPassword   *regexp.Regexp
	keyValuePat   *regexp.Regexp
}

func NewRedactor(secretPatterns []string) *Redactor {
	r := &Redactor{
		sshKeyPath:  regexp.MustCompile(`(?i)(IdentityFile\s+)\S+`),
		urlPassword: regexp.MustCompile(`(://[^:]+:)([^@]+)(@)`),
		keyValuePat: regexp.MustCompile(`(?i)((?:password|secret|token|key)\s*=\s*)(\S+)`),
	}
	for _, pat := range secretPatterns {
		escaped := regexp.QuoteMeta(pat)
		re := regexp.MustCompile(`(?i)` + escaped + `[\w-]+`)
		r.tokenPatterns = append(r.tokenPatterns, re)
	}
	return r
}

func (r *Redactor) Redact(s string) string {
	// 1. SSH key paths
	s = r.sshKeyPath.ReplaceAllString(s, "${1}[REDACTED_KEY_PATH]")

	// 2. Passwords in URLs
	s = r.urlPassword.ReplaceAllString(s, "${1}[REDACTED]${3}")

	// 3. Pattern-based token redaction
	for _, re := range r.tokenPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}

	// 4. Key=value pairs with sensitive key names
	// Only apply if there wasn't already a redaction at this position
	if !strings.Contains(s, "[REDACTED]") {
		s = r.keyValuePat.ReplaceAllString(s, "${1}[REDACTED]")
	}

	return s
}

func (r *Redactor) RedactFields(rawAction, contextLines *string) {
	if rawAction != nil {
		*rawAction = r.Redact(*rawAction)
	}
	if contextLines != nil {
		*contextLines = r.Redact(*contextLines)
	}
}
