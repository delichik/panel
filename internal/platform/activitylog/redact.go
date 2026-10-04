package activitylog

import (
	"regexp"
	"strings"
)

var secretAssignments = regexp.MustCompile(`(?i)(password|passwd|private[_-]?key|passphrase|access[_-]?token|refresh[_-]?token|api[_-]?key|secret|authorization)(\s*[=:]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)
var bearerTokens = regexp.MustCompile(`(?i)\bBearer\s+[^\s"',;]+`)
var pemPrivate = regexp.MustCompile(`(?s)-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----.*?-----END (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----`)

func Redact(value string) string {
	value = pemPrivate.ReplaceAllString(value, "[REDACTED PRIVATE KEY]")
	value = secretAssignments.ReplaceAllString(value, "${1}${2}[REDACTED]")
	return bearerTokens.ReplaceAllString(value, "Bearer [REDACTED]")
}
func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	for _, s := range []string{"password", "passwd", "privatekey", "passphrase", "accesstoken", "refreshtoken", "apikey", "secret", "authorization", "environment", "env", "params", "runtimespec"} {
		if key == s {
			return true
		}
	}
	return false
}
func redactValue(value any) any {
	switch x := value.(type) {
	case string:
		return Redact(x)
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			if sensitiveKey(k) {
				out[k] = "[REDACTED]"
			} else {
				out[k] = redactValue(v)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = redactValue(v)
		}
		return out
	default:
		return value
	}
}
