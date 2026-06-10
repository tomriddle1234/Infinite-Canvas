package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/store"
)

type APIProviderPayload struct {
	ID                      string           `json:"id"`
	Name                    string           `json:"name"`
	BaseURL                 string           `json:"base_url"`
	Protocol                string           `json:"protocol"`
	ImageGenerationEndpoint string           `json:"image_generation_endpoint"`
	ImageEditEndpoint       string           `json:"image_edit_endpoint"`
	Enabled                 bool             `json:"enabled"`
	Primary                 bool             `json:"primary"`
	ImageModels             []string         `json:"image_models"`
	ChatModels              []string         `json:"chat_models"`
	VideoModels             []string         `json:"video_models"`
	MSLoras                 []map[string]any `json:"ms_loras"`
	MSDefaultsVersion       int              `json:"ms_defaults_version"`
	APIKey                  *string          `json:"api_key"`
	ClearKey                bool             `json:"clear_key"`
}

type FirstPartyKeysPayload struct {
	OpenAIAPIKey              *string `json:"openai_api_key"`
	VolcengineArkAPIKey       *string `json:"volcengine_ark_api_key"`
	VolcengineAccessKeyID     *string `json:"volcengine_access_key_id"`
	VolcengineSecretAccessKey *string `json:"volcengine_secret_access_key"`
	VolcengineProjectName     *string `json:"volcengine_project_name"`
	VolcengineRegion          *string `json:"volcengine_region"`
}

type TestConnectionPayload struct {
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	ProviderID string `json:"provider_id"`
}

func (h *Handler) GetConfig(c *gin.Context) {
	preferredChatModel := h.cfg.ChatModel
	if len(h.cfg.ChatModels) > 0 {
		preferredChatModel = h.cfg.ChatModels[0]
		for _, model := range h.cfg.ChatModels {
			if model == "gpt-5.5" {
				preferredChatModel = model
				break
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"base_url":                      h.cfg.AIBaseURL,
		"chat_model":                    preferredChatModel,
		"image_model":                   h.cfg.ImageModel,
		"chat_models":                   h.cfg.ChatModels,
		"image_models":                  h.cfg.ImageModels,
		"video_models":                  h.cfg.VideoModels,
		"api_providers":                 h.store.PublicAPIProviders(),
		"has_api_key":                   h.cfg.AIAPIKey != "",
		"ms_chat_models":                h.cfg.ModelScopeChatModels,
		"has_ms_key":                    h.cfg.ModelScopeAPIKey != "",
		"has_openai_key":                h.cfg.OpenAIAPIKey != "",
		"openai_key_preview":            store.MaskSecret(h.cfg.OpenAIAPIKey),
		"has_volcengine_ark_key":        h.cfg.VolcengineArkAPIKey != "",
		"volcengine_ark_key_preview":    store.MaskSecret(h.cfg.VolcengineArkAPIKey),
		"has_volcengine_access_key":     h.cfg.VolcengineAccessKeyID != "",
		"volcengine_access_key_preview": store.MaskSecret(h.cfg.VolcengineAccessKeyID),
		"has_volcengine_secret_key":     h.cfg.VolcengineSecretAccessKey != "",
		"volcengine_secret_key_preview": store.MaskSecret(h.cfg.VolcengineSecretAccessKey),
		"volcengine_project_name":       h.cfg.VolcengineProjectName,
		"volcengine_region":             h.cfg.VolcengineRegion,
	})
}

func (h *Handler) ListModels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"chat_models":  h.cfg.ChatModels,
		"image_models": h.cfg.ImageModels,
		"video_models": h.cfg.VideoModels,
	})
}

func (h *Handler) ListProviders(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"providers": h.store.PublicAPIProviders()})
}

func (h *Handler) SaveProviders(c *gin.Context) {
	var payload []APIProviderPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	if len(payload) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "至少保留一个 API 平台"})
		return
	}

	normalized := make([]map[string]any, 0, len(payload))
	envUpdates := map[string]string{}
	lastPrimary := -1
	for i, item := range payload {
		if item.Primary {
			lastPrimary = i
		}
		provider, err := store.NormalizeProviderForSave(map[string]any{
			"id":                        item.ID,
			"name":                      item.Name,
			"base_url":                  item.BaseURL,
			"protocol":                  item.Protocol,
			"image_generation_endpoint": item.ImageGenerationEndpoint,
			"image_edit_endpoint":       item.ImageEditEndpoint,
			"enabled":                   item.Enabled,
			"primary":                   item.Primary,
			"image_models":              item.ImageModels,
			"chat_models":               item.ChatModels,
			"video_models":              item.VideoModels,
			"ms_loras":                  item.MSLoras,
			"ms_defaults_version":       item.MSDefaultsVersion,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
			return
		}
		for _, existing := range normalized {
			if existing["id"] == provider["id"] {
				c.JSON(http.StatusBadRequest, gin.H{"detail": "API 平台 ID 重复：" + stringValue(provider["id"], "")})
				return
			}
		}
		keyEnv := store.ProviderKeyEnv(stringValue(provider["id"], ""))
		if item.ClearKey {
			envUpdates[keyEnv] = ""
		} else if item.APIKey != nil && *item.APIKey != "" {
			envUpdates[keyEnv] = *item.APIKey
		}
		if provider["id"] == "comfly" {
			envUpdates["COMFLY_BASE_URL"] = stringValue(provider["base_url"], "")
			envUpdates["IMAGE_MODELS"] = joinStringAny(provider["image_models"])
			envUpdates["CHAT_MODELS"] = joinStringAny(provider["chat_models"])
			envUpdates["VIDEO_MODELS"] = joinStringAny(provider["video_models"])
		}
		if provider["id"] == "modelscope" {
			envUpdates["MODELSCOPE_CHAT_MODELS"] = joinStringAny(provider["chat_models"])
		}
		normalized = append(normalized, provider)
	}
	if lastPrimary >= 0 {
		for i := range normalized {
			normalized[i]["primary"] = i == lastPrimary
		}
	}

	if err := h.store.SaveAPIProviders(normalized); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "写入 API 平台配置失败：" + err.Error()})
		return
	}
	if err := h.store.UpdateEnvValues(envUpdates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "写入 env 失败：" + err.Error()})
		return
	}
	h.cfg.RefreshFromEnv()
	c.JSON(http.StatusOK, gin.H{"providers": h.store.PublicAPIProviders()})
}

func (h *Handler) GetFirstPartyKeys(c *gin.Context) {
	c.JSON(http.StatusOK, h.firstPartyKeysResponse())
}

func (h *Handler) TestProviderConnection(c *gin.Context) {
	var payload TestConnectionPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(payload.BaseURL), "/")
	if baseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请先填写请求地址"})
		return
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求地址必须以 http:// 或 https:// 开头"})
		return
	}
	apiKey := strings.TrimSpace(payload.APIKey)
	if apiKey == "" && payload.ProviderID != "" {
		apiKey = os.Getenv(store.ProviderKeyEnv(payload.ProviderID))
	}
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请先填写或保存 API Key"})
		return
	}
	modelURL := modelsEndpoint(baseURL)
	raw, status, err := getJSONWithBearer(modelURL, apiKey, 15*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": 0, "message": truncate(err.Error(), 300)})
		return
	}
	if status >= 400 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": status, "message": truncate(rawText(raw), 300)})
		return
	}
	ids := extractModelIDs(raw)
	grouped := groupModelIDs(ids)
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"status":       status,
		"model_count":  len(ids),
		"image_models": grouped["image"],
		"chat_models":  grouped["chat"],
		"video_models": grouped["video"],
		"all":          ids,
	})
}

func (h *Handler) FetchModelsFromPayload(c *gin.Context) {
	var payload TestConnectionPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	apiKey := strings.TrimSpace(payload.APIKey)
	if apiKey == "" && payload.ProviderID != "" {
		apiKey = os.Getenv(store.ProviderKeyEnv(payload.ProviderID))
	}
	h.fetchModelsFromUpstream(c, payload.BaseURL, apiKey)
}

func (h *Handler) FetchModelsFromSavedProvider(c *gin.Context) {
	providerID := c.Param("provider_id")
	provider, ok := h.providerByID(providerID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "未找到 API 平台：" + providerID})
		return
	}
	if enabled, ok := provider["enabled"].(bool); ok && !enabled {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "API 平台已禁用：" + stringValue(provider["name"], providerID)})
		return
	}
	apiKey := os.Getenv(store.ProviderKeyEnv(stringValue(provider["id"], providerID)))
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": stringValue(provider["name"], providerID) + " 未配置 API Key"})
		return
	}
	h.fetchModelsFromUpstream(c, stringValue(provider["base_url"], ""), apiKey)
}

func (h *Handler) ProbeAsyncEndpoint(c *gin.Context) {
	var payload TestConnectionPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(payload.BaseURL), "/")
	if baseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请先填写请求地址"})
		return
	}
	apiKey := strings.TrimSpace(payload.APIKey)
	if apiKey == "" && payload.ProviderID != "" {
		apiKey = os.Getenv(store.ProviderKeyEnv(payload.ProviderID))
	}
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请先填写或保存 API Key"})
		return
	}
	tasksBase := baseURL
	if !strings.HasSuffix(tasksBase, "/v1") {
		tasksBase += "/v1"
	}
	raw, status, err := getJSONWithBearer(tasksBase+"/tasks/healthcheck_probe_do_not_submit", apiKey, 15*time.Second)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": truncate(err.Error(), 300)})
		return
	}
	errMsg := ""
	if errObj, ok := raw["error"].(map[string]any); ok {
		errMsg = strings.ToLower(stringValue(errObj["message"], ""))
	} else if raw["error"] != nil {
		errMsg = strings.ToLower(stringValue(raw["error"], ""))
	}
	switch {
	case status == 400 && strings.Contains(errMsg, "invalid task id"):
		c.JSON(http.StatusOK, gin.H{"ok": true, "status_code": status, "message": "异步任务端点可用，API Key 已通过认证", "raw": raw})
	case status == 401 || status == 403:
		c.JSON(http.StatusOK, gin.H{"ok": false, "status_code": status, "message": "API Key 无效或无权限", "raw": raw})
	case status == 404:
		c.JSON(http.StatusOK, gin.H{"ok": false, "status_code": status, "message": "平台不支持 /v1/tasks/ 端点，可能不是 APIMart 异步协议", "raw": raw})
	case status >= 400 && status < 500:
		c.JSON(http.StatusOK, gin.H{"ok": nil, "status_code": status, "message": "端点返回 " + strconv.Itoa(status) + "，请查看原始响应判断", "raw": raw})
	case status < 300:
		c.JSON(http.StatusOK, gin.H{"ok": true, "status_code": status, "message": "端点返回 " + strconv.Itoa(status) + "（意外成功）", "raw": raw})
	default:
		c.JSON(http.StatusOK, gin.H{"ok": false, "status_code": status, "message": "服务端错误 " + strconv.Itoa(status), "raw": raw})
	}
}

func (h *Handler) SaveFirstPartyKeys(c *gin.Context) {
	var payload FirstPartyKeysPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	updates := map[string]string{}
	if payload.OpenAIAPIKey != nil {
		updates["OPENAI_API_KEY"] = *payload.OpenAIAPIKey
	}
	if payload.VolcengineArkAPIKey != nil {
		updates["VOLCENGINE_ARK_API_KEY"] = *payload.VolcengineArkAPIKey
	}
	if payload.VolcengineAccessKeyID != nil {
		updates["VOLCENGINE_ACCESS_KEY_ID"] = *payload.VolcengineAccessKeyID
	}
	if payload.VolcengineSecretAccessKey != nil {
		updates["VOLCENGINE_SECRET_ACCESS_KEY"] = *payload.VolcengineSecretAccessKey
	}
	if payload.VolcengineProjectName != nil {
		updates["VOLCENGINE_PROJECT_NAME"] = firstNonEmptyString(*payload.VolcengineProjectName, "default")
	}
	if payload.VolcengineRegion != nil {
		updates["VOLCENGINE_REGION"] = firstNonEmptyString(*payload.VolcengineRegion, "cn-beijing")
	}
	if err := h.store.UpdateEnvValues(updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "写入 env 失败：" + err.Error()})
		return
	}
	h.cfg.RefreshFromEnv()
	c.JSON(http.StatusOK, h.firstPartyKeysResponse())
}

func (h *Handler) GetGlobalToken(c *gin.Context) {
	if h.cfg.ModelScopeAPIKey != "" {
		c.JSON(http.StatusOK, gin.H{"token": h.cfg.ModelScopeAPIKey})
		return
	}
	data, err := os.ReadFile(h.cfg.GlobalConfigFile)
	if err == nil {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil {
			c.JSON(http.StatusOK, gin.H{"token": stringValue(cfg["modelscope_token"], "")})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"token": ""})
}

func (h *Handler) fetchModelsFromUpstream(c *gin.Context, baseURL, apiKey string) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请先填写请求地址"})
		return
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求地址必须以 http:// 或 https:// 开头"})
		return
	}
	if strings.TrimSpace(apiKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请先填写或保存 API Key"})
		return
	}
	raw, status, err := getJSONWithBearer(modelsEndpoint(baseURL), apiKey, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": "请求上游模型列表失败：" + err.Error()})
		return
	}
	if status >= 400 {
		c.JSON(status, gin.H{"detail": "上游 /v1/models 失败：" + truncate(rawText(raw), 300)})
		return
	}
	ids := extractModelIDs(raw)
	grouped := groupModelIDs(ids)
	c.JSON(http.StatusOK, gin.H{
		"total":        len(ids),
		"image_models": grouped["image"],
		"chat_models":  grouped["chat"],
		"video_models": grouped["video"],
		"all":          ids,
	})
}

func (h *Handler) providerByID(providerID string) (map[string]any, bool) {
	target := strings.ToLower(strings.TrimSpace(providerID))
	for _, provider := range h.store.LoadAPIProviders() {
		if stringValue(provider["id"], "") == target {
			return provider, true
		}
	}
	return nil, false
}

func (h *Handler) GetComfyUIInstances(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"instances": h.cfg.ComfyUIInstances})
}

func (h *Handler) firstPartyKeysResponse() gin.H {
	return gin.H{
		"has_openai_key":                h.cfg.OpenAIAPIKey != "",
		"openai_key_preview":            store.MaskSecret(h.cfg.OpenAIAPIKey),
		"has_volcengine_ark_key":        h.cfg.VolcengineArkAPIKey != "",
		"volcengine_ark_key_preview":    store.MaskSecret(h.cfg.VolcengineArkAPIKey),
		"has_volcengine_access_key":     h.cfg.VolcengineAccessKeyID != "",
		"volcengine_access_key_preview": store.MaskSecret(h.cfg.VolcengineAccessKeyID),
		"has_volcengine_secret_key":     h.cfg.VolcengineSecretAccessKey != "",
		"volcengine_secret_key_preview": store.MaskSecret(h.cfg.VolcengineSecretAccessKey),
		"volcengine_project_name":       h.cfg.VolcengineProjectName,
		"volcengine_region":             h.cfg.VolcengineRegion,
	}
}

func joinStringAny(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringValue(item, ""); text != "" {
			out = append(out, text)
		}
	}
	return strings.Join(out, ",")
}

func firstNonEmptyString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func modelsEndpoint(baseURL string) string {
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/models"
	}
	return baseURL + "/v1/models"
}

func getJSONWithBearer(endpoint, apiKey string, timeout time.Duration) (map[string]any, int, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(body) == 0 {
		return map[string]any{}, resp.StatusCode, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return map[string]any{"raw": string(body)}, resp.StatusCode, nil
	}
	return raw, resp.StatusCode, nil
}

func rawText(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	if text := stringValue(raw["raw"], ""); text != "" {
		return text
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(data)
}

func extractModelIDs(raw map[string]any) []string {
	var items any
	if raw != nil {
		items = raw["data"]
		if items == nil {
			items = raw["models"]
		}
		if items == nil {
			items = raw["list"]
		}
	}
	list, ok := items.([]any)
	if !ok {
		return []string{}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, item := range list {
		id := ""
		if text, ok := item.(string); ok {
			id = text
		} else if obj, ok := item.(map[string]any); ok {
			id = firstNonEmptyString(stringValue(obj["id"], ""), firstNonEmptyString(stringValue(obj["name"], ""), stringValue(obj["model"], "")))
		}
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func groupModelIDs(ids []string) map[string][]string {
	grouped := map[string][]string{"image": {}, "chat": {}, "video": {}}
	for _, id := range ids {
		grouped[classifyModelID(id)] = append(grouped[classifyModelID(id)], id)
	}
	return grouped
}

func classifyModelID(modelID string) string {
	lc := strings.ToLower(modelID)
	videoKeys := []string{"veo", "sora", "wan2", "wanx", "doubao-seedance", "doubao-1", "kling", "hailuo", "video", "t2v-", "i2v-", "s2v"}
	for _, key := range videoKeys {
		if strings.Contains(lc, key) {
			return "video"
		}
	}
	imageKeys := []string{"image", "dalle", "dall-e", "imagen", "flux", "stable", "sdxl", "midjourney", "nano-banana", "ideogram", "fal-ai", "z-image", "qwen-image", "klein"}
	for _, key := range imageKeys {
		if strings.Contains(lc, key) {
			return "image"
		}
	}
	return "chat"
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
