package execution

import (
	"encoding/json"
	"regexp"
	"strings"
)

var sensitiveText = regexp.MustCompile(`(?i)(authorization|bearer\s|sk-[a-z0-9]|access.?key|password|refresh.?token|access.?token|id.?token|api.?key|client.?secret|private.?key|credential|[?&]code=|://[^\s/]+:[^\s/]+@)`)

func sanitizeEvent(v ProgressEvent, known ...string) (ProgressEvent, error) {
	switch v.Kind {
	case "status", "progress", "tool_start", "tool_result", "approval_wait", "pr", "checkpoint", "cleanup":
	default:
		return v, ErrInvalid
	}
	if containsSensitive(v.Summary, known) {
		v.Summary = "[redacted sensitive content]"
	}
	if len(v.Details) > 0 {
		var fields map[string]json.RawMessage
		if json.Unmarshal(v.Details, &fields) != nil {
			return v, ErrInvalid
		}
		clean := map[string]any{}
		for _, key := range []string{"tool", "status", "message", "result"} {
			raw, ok := fields[key]
			if !ok {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				continue
			}
			if containsSensitive(value, known) {
				value = "[redacted sensitive content]"
			}
			clean[key] = strings.TrimSpace(value)
		}
		v.Details, _ = json.Marshal(clean)
	}
	return v, nil
}

func containsSensitive(value string, known []string) bool {
	if sensitiveText.MatchString(value) {
		return true
	}
	for _, secret := range known {
		if secret != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}
