package store

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	accessKeyPattern      = regexp.MustCompile(`(?i)\b(AKLT|AKTP)[A-Z0-9]+\b`)
	sensitiveURLQueryKeys = map[string]bool{
		"accesskeyid":          true,
		"signature":            true,
		"x-amz-credential":     true,
		"x-amz-security-token": true,
		"x-amz-signature":      true,
		"x-tos-credential":     true,
		"x-tos-security-token": true,
		"x-tos-signature":      true,
	}
)

func sanitizePersistedMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = sanitizePersistedData(item)
	}
	return out
}

func sanitizePersistedData(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizePersistedMap(typed)
	case []any:
		out := make([]any, len(typed))
		for idx, item := range typed {
			out[idx] = sanitizePersistedData(item)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for idx, item := range typed {
			out[idx] = sanitizePersistedString(item)
		}
		return out
	case string:
		return sanitizePersistedString(typed)
	default:
		return value
	}
}

func sanitizePersistedString(value string) string {
	cleaned := accessKeyPattern.ReplaceAllString(value, `${1}***`)
	if !strings.Contains(cleaned, "://") || !strings.Contains(cleaned, "?") {
		return cleaned
	}

	parsed, err := url.Parse(cleaned)
	if err != nil {
		return cleaned
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		if sensitiveURLQueryKeys[strings.ToLower(key)] {
			query[key] = []string{"***"}
			changed = true
		}
	}
	if !changed {
		return cleaned
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
