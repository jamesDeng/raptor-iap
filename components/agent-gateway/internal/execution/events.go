package execution

import (
	"encoding/json"
	"regexp"
	"strings"
)

var sensitiveText = regexp.MustCompile(`(?i)(authorization|bearer\s|sk-[a-z0-9]|access.?key|password|refresh.?token|[?&]code=|://[^\s/]+:[^\s/]+@)`)

func sanitizeEvent(v ProgressEvent) (ProgressEvent, error) {
	switch v.Kind {
	case "status", "progress", "tool_start", "tool_result", "approval_wait", "pr", "checkpoint", "cleanup":
	default:
		return v, ErrInvalid
	}
	if sensitiveText.MatchString(v.Summary) {
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
			if sensitiveText.MatchString(value) {
				value = "[redacted sensitive content]"
			}
			clean[key] = strings.TrimSpace(value)
		}
		v.Details, _ = json.Marshal(clean)
	}
	return v, nil
}
