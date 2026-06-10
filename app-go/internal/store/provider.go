package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var providerEnvPattern = regexp.MustCompile(`[^A-Za-z0-9]`)
var providerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{2,40}$`)

func (s *Store) LoadAPIProviders() []map[string]any {
	defaults := s.defaultAPIProviders()
	data, err := os.ReadFile(s.cfg.APIProvidersFile)
	if os.IsNotExist(err) {
		return defaults
	}
	if err != nil {
		return defaults
	}
	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return defaults
	}
	if len(raw) == 0 {
		return defaults
	}
	return s.mergeDefaultAPIProviders(raw)
}

func (s *Store) PublicAPIProviders() []map[string]any {
	providers := s.LoadAPIProviders()
	out := make([]map[string]any, 0, len(providers))
	for _, provider := range providers {
		out = append(out, publicProvider(provider))
	}
	return out
}

func (s *Store) SaveAPIProviders(providers []map[string]any) error {
	s.providerMu.Lock()
	defer s.providerMu.Unlock()
	return writeJSON(s.cfg.APIProvidersFile, providers)
}

func (s *Store) UpdateEnvValues(updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}
	s.envMu.Lock()
	defer s.envMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.cfg.APIEnvFile), 0o755); err != nil {
		return err
	}

	lines := []string{}
	file, err := os.Open(s.cfg.APIEnvFile)
	if err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		_ = file.Close()
		if err := scanner.Err(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	seen := map[string]bool{}
	next := make([]string, 0, len(lines)+len(updates))
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") || !strings.Contains(line, "=") {
			next = append(next, line)
			continue
		}
		key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		if value, ok := updates[key]; ok {
			next = append(next, key+"="+envQuote(value))
			_ = os.Setenv(key, value)
			seen[key] = true
		} else {
			next = append(next, line)
		}
	}
	for key, value := range updates {
		if seen[key] {
			continue
		}
		next = append(next, key+"="+envQuote(value))
		_ = os.Setenv(key, value)
	}
	return os.WriteFile(s.cfg.APIEnvFile, []byte(strings.Join(next, "\n")+"\n"), 0o644)
}

func (s *Store) defaultAPIProviders() []map[string]any {
	return []map[string]any{
		{
			"id":                        "modelscope",
			"name":                      "ModelScope",
			"base_url":                  s.cfg.ModelScopeChatBaseURL,
			"protocol":                  "openai",
			"image_generation_endpoint": "",
			"image_edit_endpoint":       "",
			"enabled":                   true,
			"primary":                   false,
			"image_models":              defaultModelScopeImageModels(),
			"chat_models":               s.cfg.ModelScopeChatModels,
			"video_models":              []string{},
			"ms_loras":                  defaultModelScopeLoras(),
			"ms_defaults_version":       3,
		},
	}
}

func (s *Store) mergeDefaultAPIProviders(providers []map[string]any) []map[string]any {
	merged := make([]map[string]any, 0, len(providers)+1)
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		merged = append(merged, normalizeProvider(provider))
	}

	var current map[string]any
	for _, provider := range merged {
		if stringValue(provider["id"], "") == "modelscope" {
			current = provider
			break
		}
	}
	if current == nil {
		return append(merged, s.defaultAPIProviders()[0])
	}
	if stringValue(current["base_url"], "") == "" {
		current["base_url"] = s.cfg.ModelScopeChatBaseURL
	}
	if number(current["ms_defaults_version"]) < 3 {
		current["image_models"] = dedupeAny(append(anySlice(defaultModelScopeImageModels()), anySliceValue(current["image_models"])...))
		current["chat_models"] = dedupeAny(append(anySlice([]string{
			"Qwen/Qwen3-235B-A22B",
			"Qwen/Qwen3-VL-235B-A22B-Instruct",
			"MiniMax/MiniMax-M2.7:MiniMax",
		}), anySliceValue(current["chat_models"])...))
		current["ms_loras"] = append(mapSlice(defaultModelScopeLoras()), anySliceValue(current["ms_loras"])...)
		current["ms_defaults_version"] = 3
	}
	return merged
}

func normalizeProvider(provider map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range provider {
		out[key] = value
	}
	id := strings.ToLower(strings.TrimSpace(stringValue(out["id"], "")))
	out["id"] = id
	out["name"] = strings.TrimSpace(stringValue(out["name"], id))
	out["base_url"] = strings.TrimRight(strings.TrimSpace(stringValue(out["base_url"], "")), "/")
	protocol := strings.ToLower(strings.TrimSpace(stringValue(out["protocol"], "openai")))
	if protocol != "apimart" {
		protocol = "openai"
	}
	out["protocol"] = protocol
	if _, ok := out["enabled"]; !ok {
		out["enabled"] = true
	}
	if _, ok := out["primary"]; !ok {
		out["primary"] = false
	}
	out["image_models"] = dedupeAny(anySliceValue(out["image_models"]))
	out["chat_models"] = dedupeAny(anySliceValue(out["chat_models"]))
	out["video_models"] = dedupeAny(anySliceValue(out["video_models"]))
	if _, ok := out["ms_loras"]; !ok {
		out["ms_loras"] = []any{}
	}
	if _, ok := out["ms_defaults_version"]; !ok {
		out["ms_defaults_version"] = 0
	}
	if _, ok := out["image_generation_endpoint"]; !ok {
		out["image_generation_endpoint"] = ""
	}
	if _, ok := out["image_edit_endpoint"]; !ok {
		out["image_edit_endpoint"] = ""
	}
	return out
}

func NormalizeProviderForSave(provider map[string]any) (map[string]any, error) {
	out := normalizeProvider(provider)
	id := stringValue(out["id"], "")
	if !providerIDPattern.MatchString(id) {
		return nil, errors.New("API 平台 ID 不合法：" + firstNonEmpty(id, "(empty)"))
	}
	name := stringValue(out["name"], id)
	baseURL := stringValue(out["base_url"], "")
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, errors.New(name + " 的 Base URL 需要以 http:// 或 https:// 开头")
	}
	out["name"] = trimStringRunes(name, id, 60)
	out["image_generation_endpoint"] = normalizeEndpoint(out["image_generation_endpoint"], "文生图端口")
	out["image_edit_endpoint"] = normalizeEndpoint(out["image_edit_endpoint"], "图生图/编辑端口")
	return out, nil
}

func normalizeEndpoint(value any, label string) string {
	endpoint := strings.TrimSpace(stringValue(value, ""))
	if endpoint == "" {
		return ""
	}
	if len(endpoint) > 300 || strings.ContainsAny(endpoint, " \t\r\n") {
		return ""
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return strings.TrimRight(endpoint, "/")
	}
	if !strings.HasPrefix(endpoint, "/") {
		return ""
	}
	_ = label
	return endpoint
}

func publicProvider(provider map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range provider {
		out[key] = value
	}
	keyEnv := ProviderKeyEnv(stringValue(provider["id"], ""))
	key := os.Getenv(keyEnv)
	out["has_key"] = key != ""
	out["key_preview"] = MaskSecret(key)
	out["key_env"] = keyEnv
	return out
}

func ProviderKeyEnv(providerID string) string {
	if providerID == "comfly" {
		return "COMFLY_API_KEY"
	}
	if providerID == "modelscope" {
		return "MODELSCOPE_API_KEY"
	}
	return "API_PROVIDER_" + strings.ToUpper(providerEnvPattern.ReplaceAllString(providerID, "_")) + "_KEY"
}

func MaskSecret(value string) string {
	if value == "" {
		return ""
	}
	tail := value
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return "••••••••" + tail
}

func envQuote(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\r\n#'\"") {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
	}
	return value
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func defaultModelScopeImageModels() []string {
	return []string{
		"Tongyi-MAI/Z-Image-Turbo",
		"Qwen/Qwen-Image-2512",
		"Qwen/Qwen-Image-Edit-2511",
		"black-forest-labs/FLUX.2-klein-9B",
	}
}

func defaultModelScopeLoras() []map[string]any {
	return []map[string]any{
		{
			"id":           "Daniel8152/film",
			"name":         "Z-Image Film",
			"target_model": "Tongyi-MAI/Z-Image-Turbo",
			"strength":     0.8,
			"enabled":      true,
			"note":         "",
		},
		{
			"id":           "Daniel8152/Qwen-Image-2512-Film",
			"name":         "Qwen Image 2512 Film",
			"target_model": "Qwen/Qwen-Image-2512",
			"strength":     0.8,
			"enabled":      true,
			"note":         "",
		},
		{
			"id":           "Daniel8152/Klein-enhance",
			"name":         "Klein enhance",
			"target_model": "black-forest-labs/FLUX.2-klein-9B",
			"strength":     0.8,
			"enabled":      true,
			"note":         "",
		},
	}
}

func anySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func mapSlice(values []map[string]any) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func anySliceValue(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		return anySlice(typed)
	default:
		return []any{}
	}
}

func dedupeAny(values []any) []any {
	seen := map[string]bool{}
	out := make([]any, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(stringValue(value, ""))
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	return out
}
