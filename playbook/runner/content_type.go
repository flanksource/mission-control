package runner

import "encoding/json"

// extractContentType inspects the action result for content-type information.
// It checks for JSON envelope {content, contentType} in text fields,
// and applies the spec-level contentType as final override.
func extractContentType(data any, actionType string, specContentType string) any {
	if data == nil {
		if specContentType != "" {
			return map[string]any{"contentType": specContentType}
		}
		return data
	}

	b, err := json.Marshal(data)
	if err != nil {
		return data
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return data
	}

	if primaryKey := primaryOutputKey(actionType); primaryKey != "" {
		if raw, ok := m[primaryKey].(string); ok {
			if content, ct := parseContentEnvelope(raw); content != "" {
				m[primaryKey] = content
				m["contentType"] = ct
			}
		}
	}

	if actionType == "http" {
		if headers, ok := m["headers"].(map[string]any); ok {
			if ct, ok := headers["Content-Type"].(string); ok && m["contentType"] == nil {
				m["contentType"] = ct
			}
		}
	}

	if specContentType != "" {
		m["contentType"] = specContentType
	}

	return m
}

func primaryOutputKey(actionType string) string {
	switch actionType {
	case "exec", "pod":
		return "stdout"
	case "http":
		return "content"
	default:
		return ""
	}
}

// parseContentEnvelope checks if s is JSON with "content" and "contentType" keys.
func parseContentEnvelope(s string) (string, string) {
	if len(s) == 0 || s[0] != '{' {
		return "", ""
	}
	var envelope struct {
		Content     string `json:"content"`
		ContentType string `json:"contentType"`
	}
	if err := json.Unmarshal([]byte(s), &envelope); err != nil {
		return "", ""
	}
	if envelope.Content != "" && envelope.ContentType != "" {
		return envelope.Content, envelope.ContentType
	}
	return "", ""
}
