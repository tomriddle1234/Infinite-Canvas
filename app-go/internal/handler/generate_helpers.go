package handler

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"infinite-canvas/app-go/internal/store"
)

func nowSeconds() float64 {
	return float64(time.Now().UnixMilli()) / 1000.0
}

func referencesToMaps(refs []AIReference) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.URL) == "" {
			continue
		}
		out = append(out, map[string]any{"url": ref.URL, "name": ref.Name, "role": ref.Role})
	}
	return out
}

func appendAny(value any, item map[string]any) []any {
	items, _ := value.([]any)
	if items == nil {
		items = []any{}
	}
	return append(items, item)
}

func (h *Handler) providerForID(providerID string) (map[string]any, error) {
	target := strings.ToLower(strings.TrimSpace(providerID))
	providers := h.store.LoadAPIProviders()
	for _, provider := range providers {
		if stringValue(provider["id"], "") == target {
			if enabled, ok := provider["enabled"].(bool); ok && !enabled {
				return nil, errors.New("API 平台已禁用：" + stringValue(provider["name"], target))
			}
			return provider, nil
		}
	}
	for _, provider := range providers {
		if stringValue(provider["id"], "") != "modelscope" {
			return provider, nil
		}
	}
	if len(providers) > 0 {
		return providers[0], nil
	}
	return nil, errors.New("未找到 API 平台：" + providerID)
}

func providerBaseV1(provider map[string]any, fallback string) string {
	base := strings.TrimRight(stringValue(provider["base_url"], fallback), "/")
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func (h *Handler) resolveChatProvider(providerID, model, msModel string) (string, string, string, error) {
	if providerID == "modelscope" {
		if h.cfg.ModelScopeAPIKey == "" {
			return "", "", "", errors.New("未配置 MODELSCOPE_API_KEY，请在 API 设置中填写。")
		}
		fallback := "MiniMax/MiniMax-M2.7"
		if len(h.cfg.ModelScopeChatModels) > 0 {
			fallback = h.cfg.ModelScopeChatModels[0]
		}
		return h.cfg.ModelScopeChatBaseURL, h.cfg.ModelScopeAPIKey, firstNonEmptyString(firstNonEmptyString(msModel, model), fallback), nil
	}
	provider, err := h.providerForID(firstNonEmptyString(providerID, "comfly"))
	if err != nil {
		return "", "", "", err
	}
	apiKey := strings.TrimSpace(os.Getenv(store.ProviderKeyEnv(stringValue(provider["id"], ""))))
	if apiKey == "" {
		return "", "", "", errors.New("未配置 " + stringValue(provider["name"], stringValue(provider["id"], "")) + " 的 API Key")
	}
	defaultModel := firstStringFromAny(provider["chat_models"], h.cfg.ChatModel)
	return providerBaseV1(provider, h.cfg.AIBaseURL), apiKey, firstNonEmptyString(model, defaultModel), nil
}

func (h *Handler) chatConversation(userID string, payload ChatRequest) (map[string]any, error) {
	if payload.ConversationID != "" {
		return h.store.LoadConversation(userID, payload.ConversationID)
	}
	return h.store.NewConversation(userID, displayTitle(payload.Message))
}

func displayTitle(text string) string {
	title := strings.Join(strings.Fields(text), " ")
	runes := []rune(title)
	if len(runes) > 24 {
		runes = runes[:24]
	}
	if len(runes) == 0 {
		return "新对话"
	}
	return string(runes)
}

func (h *Handler) upstreamMessagesFromConversation(conversation map[string]any) []map[string]any {
	messages := []map[string]any{{"role": "system", "content": "You are a helpful assistant."}}
	rawMessages, _ := conversation["messages"].([]any)
	start := 0
	if len(rawMessages) > h.cfg.MaxHistoryMessages {
		start = len(rawMessages) - h.cfg.MaxHistoryMessages
	}
	for _, raw := range rawMessages[start:] {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		role := stringValue(item["role"], "")
		if role != "user" && role != "assistant" {
			continue
		}
		if item["type"] == "image" {
			continue
		}
		content := stringValue(item["content"], "")
		if content == "" {
			continue
		}
		messages = append(messages, map[string]any{"role": role, "content": content})
	}
	return messages
}

func firstStringFromAny(value any, fallback string) string {
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if text := stringValue(item, ""); text != "" {
				return text
			}
		}
	}
	if list, ok := value.([]string); ok {
		for _, text := range list {
			if text != "" {
				return text
			}
		}
	}
	return fallback
}

func sseEvent(value any) string {
	data, _ := json.Marshal(value)
	return "data: " + string(data) + "\n\n"
}
