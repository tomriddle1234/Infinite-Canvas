package handler

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/store"
	"infinite-canvas/app-go/internal/upstream"
)

type AIReference struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type OnlineImageRequest struct {
	Prompt          string        `json:"prompt"`
	ProviderID      string        `json:"provider_id"`
	Model           string        `json:"model"`
	Size            string        `json:"size"`
	Quality         string        `json:"quality"`
	ReferenceImages []AIReference `json:"reference_images"`
}

type GPTImage2Request struct {
	Prompt          string        `json:"prompt"`
	Size            string        `json:"size"`
	Quality         string        `json:"quality"`
	Count           int           `json:"count"`
	ReferenceImages []AIReference `json:"reference_images"`
}

type CanvasLLMRequest struct {
	Message      string           `json:"message"`
	SystemPrompt string           `json:"system_prompt"`
	Model        string           `json:"model"`
	Messages     []map[string]any `json:"messages"`
	Provider     string           `json:"provider"`
	MSModel      string           `json:"ms_model"`
	Images       []string         `json:"images"`
}

type ChatRequest struct {
	ConversationID  string        `json:"conversation_id"`
	Message         string        `json:"message"`
	Model           string        `json:"model"`
	ImageModel      string        `json:"image_model"`
	Mode            string        `json:"mode"`
	Size            string        `json:"size"`
	Quality         string        `json:"quality"`
	ReferenceImages []AIReference `json:"reference_images"`
	Provider        string        `json:"provider"`
	MSModel         string        `json:"ms_model"`
}

func (h *Handler) OnlineImage(c *gin.Context) {
	var payload OnlineImageRequest
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "提示词不能为空"})
		return
	}
	result, err := h.buildOnlineImageResult(payload)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) CreateCanvasImageTask(c *gin.Context) {
	h.cleanupCanvasImageTasks()
	var payload OnlineImageRequest
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "提示词不能为空"})
		return
	}
	taskID := "canvas_img_" + store.NewHexID()
	task := map[string]any{"id": taskID, "type": "online-image", "status": "queued", "created_at": nowSeconds(), "updated_at": nowSeconds(), "result": nil, "error": ""}
	h.tasksMu.Lock()
	h.canvasImageTasks[taskID] = task
	h.tasksMu.Unlock()
	go h.runCanvasImageTask(taskID, payload)
	c.JSON(http.StatusOK, gin.H{"task_id": taskID, "status": "queued"})
}

func (h *Handler) GetCanvasImageTask(c *gin.Context) {
	h.cleanupCanvasImageTasks()
	h.tasksMu.Lock()
	task, ok := h.canvasImageTasks[c.Param("task_id")]
	h.tasksMu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"detail": "画布任务不存在，可能服务已重启或任务已过期"})
		return
	}
	c.JSON(http.StatusOK, task)
}

func (h *Handler) cleanupCanvasImageTasks() {
	ttl := float64(maxIntLocal(h.cfg.ImageTaskTimeoutSec*2, 1800))
	cutoff := nowSeconds() - ttl
	h.tasksMu.Lock()
	defer h.tasksMu.Unlock()
	for id, task := range h.canvasImageTasks {
		status := stringValue(task["status"], "")
		if status != "succeeded" && status != "failed" {
			continue
		}
		if numberValue(task["updated_at"]) < cutoff {
			delete(h.canvasImageTasks, id)
		}
	}
}

func (h *Handler) runCanvasImageTask(taskID string, payload OnlineImageRequest) {
	h.updateCanvasImageTask(taskID, map[string]any{"status": "running", "updated_at": nowSeconds()})
	result, err := h.buildOnlineImageResult(payload)
	if err != nil {
		h.updateCanvasImageTask(taskID, map[string]any{"status": "failed", "error": err.Error(), "updated_at": nowSeconds()})
		return
	}
	h.updateCanvasImageTask(taskID, map[string]any{"status": "succeeded", "result": result, "error": "", "updated_at": nowSeconds()})
}

func (h *Handler) updateCanvasImageTask(taskID string, updates map[string]any) {
	h.tasksMu.Lock()
	defer h.tasksMu.Unlock()
	if task, ok := h.canvasImageTasks[taskID]; ok {
		for k, v := range updates {
			task[k] = v
		}
	}
}

func (h *Handler) NodeGPTImage2(c *gin.Context) {
	var payload GPTImage2Request
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "提示词不能为空"})
		return
	}
	apiKey := strings.TrimSpace(h.cfg.OpenAIAPIKey)
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "未配置 OPENAI_API_KEY，请先在 API 设置中填写。"})
		return
	}
	client := upstream.NewClient(h.cfg, h.store)
	refs := referencesToMaps(payload.ReferenceImages)
	items, raw, err := client.GenerateImage(h.cfg.OpenAIAPIBaseURL, apiKey, "gpt-image-2", payload.Prompt, firstNonEmptyString(payload.Size, "1024x1024"), payload.Quality, refs, payload.Count)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	images, err := h.saveImageItems(client, items, "gpt_image2_")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	result := map[string]any{"prompt": payload.Prompt, "images": images, "timestamp": nowSeconds(), "type": "gpt-image-2", "model": "gpt-image-2", "provider_id": "openai", "params": gin.H{"size": payload.Size, "quality": payload.Quality, "count": payload.Count, "reference_images": refs}, "raw": raw}
	_ = h.store.SaveHistory(result)
	if h.rt != nil {
		h.rt.BroadcastNewImage(result)
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) CanvasLLM(c *gin.Context) {
	var payload CanvasLLMRequest
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Message) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "文本不能为空"})
		return
	}
	baseURL, apiKey, model, err := h.resolveChatProvider(payload.Provider, payload.Model, payload.MSModel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	messages := []map[string]any{{"role": "system", "content": firstNonEmptyString(payload.SystemPrompt, "You are a helpful assistant.")}}
	start := 0
	if len(payload.Messages) > h.cfg.MaxHistoryMessages {
		start = len(payload.Messages) - h.cfg.MaxHistoryMessages
	}
	for _, item := range payload.Messages[start:] {
		if role := stringValue(item["role"], ""); role == "user" || role == "assistant" {
			if content := stringValue(item["content"], ""); content != "" {
				messages = append(messages, map[string]any{"role": role, "content": content})
			}
		}
	}
	messages = append(messages, map[string]any{"role": "user", "content": payload.Message})
	raw, err := upstream.NewClient(h.cfg, h.store).ChatCompletion(baseURL, apiKey, model, messages, false)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	text := strings.TrimSpace(upstream.TextFromChatResponse(raw))
	if text == "" {
		text = "接口返回了空回复。"
	}
	c.JSON(http.StatusOK, gin.H{"text": text, "model": model, "raw_usage": raw["usage"]})
}

func (h *Handler) Chat(c *gin.Context) {
	var payload ChatRequest
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Message) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "文本不能为空"})
		return
	}
	userID := store.SafeUserID(c.GetHeader("X-User-Id"), c.ClientIP())
	conversation, err := h.chatConversation(userID, payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	userMessage := map[string]any{"id": store.NewHexID(), "role": "user", "content": payload.Message, "created_at": store.NowMS(), "attachments": referencesToMaps(payload.ReferenceImages), "mode": firstNonEmptyString(payload.Mode, "chat")}
	conversation["messages"] = appendAny(conversation["messages"], userMessage)
	conversation["updated_at"] = store.NowMS()
	_ = h.store.SaveConversation(userID, conversation)

	var assistant map[string]any
	if payload.Mode == "image" {
		imagePayload := OnlineImageRequest{Prompt: payload.Message, ProviderID: payload.Provider, Model: firstNonEmptyString(payload.ImageModel, payload.Model), Size: payload.Size, Quality: payload.Quality, ReferenceImages: payload.ReferenceImages}
		result, err := h.buildOnlineImageResult(imagePayload)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
			return
		}
		images, _ := result["images"].([]string)
		imageURL := ""
		if len(images) > 0 {
			imageURL = images[0]
		}
		assistant = map[string]any{"id": store.NewHexID(), "role": "assistant", "type": "image", "content": payload.Message, "image_url": imageURL, "created_at": store.NowMS(), "model": result["model"], "raw_usage": result["raw_usage"]}
	} else {
		baseURL, apiKey, model, err := h.resolveChatProvider(payload.Provider, payload.Model, payload.MSModel)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
			return
		}
		upstreamMessages := h.upstreamMessagesFromConversation(conversation)
		raw, err := upstream.NewClient(h.cfg, h.store).ChatCompletion(baseURL, apiKey, model, upstreamMessages, false)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
			return
		}
		text := strings.TrimSpace(upstream.TextFromChatResponse(raw))
		if text == "" {
			text = "接口返回了空回复。"
		}
		assistant = map[string]any{"id": store.NewHexID(), "role": "assistant", "content": text, "created_at": store.NowMS(), "model": model, "raw_usage": raw["usage"]}
	}
	conversation["messages"] = appendAny(conversation["messages"], assistant)
	conversation["updated_at"] = store.NowMS()
	_ = h.store.SaveConversation(userID, conversation)
	c.JSON(http.StatusOK, gin.H{"conversation": conversation, "message": assistant})
}

func (h *Handler) ChatStream(c *gin.Context) {
	// Minimal compatible stream: produce a normal completion and emit meta/delta/done SSE events.
	var payload ChatRequest
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Message) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "文本不能为空"})
		return
	}
	payload.Mode = "chat"
	userID := store.SafeUserID(c.GetHeader("X-User-Id"), c.ClientIP())
	conversation, err := h.chatConversation(userID, payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	userMessage := map[string]any{"id": store.NewHexID(), "role": "user", "content": payload.Message, "created_at": store.NowMS(), "attachments": referencesToMaps(payload.ReferenceImages), "mode": "chat"}
	conversation["messages"] = appendAny(conversation["messages"], userMessage)
	conversation["updated_at"] = store.NowMS()
	_ = h.store.SaveConversation(userID, conversation)
	baseURL, apiKey, model, err := h.resolveChatProvider(payload.Provider, payload.Model, payload.MSModel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	raw, err := upstream.NewClient(h.cfg, h.store).ChatCompletion(baseURL, apiKey, model, h.upstreamMessagesFromConversation(conversation), false)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	text := strings.TrimSpace(upstream.TextFromChatResponse(raw))
	if text == "" {
		text = "接口返回了空回复。"
	}
	assistant := map[string]any{"id": store.NewHexID(), "role": "assistant", "content": text, "created_at": store.NowMS(), "model": model, "raw_usage": raw["usage"]}
	conversation["messages"] = appendAny(conversation["messages"], assistant)
	conversation["updated_at"] = store.NowMS()
	_ = h.store.SaveConversation(userID, conversation)
	c.Header("Content-Type", "text/event-stream")
	_, _ = c.Writer.WriteString(sseEvent(gin.H{"type": "meta", "conversation": conversation}))
	_, _ = c.Writer.WriteString(sseEvent(gin.H{"type": "delta", "delta": text}))
	_, _ = c.Writer.WriteString(sseEvent(gin.H{"type": "done", "conversation": conversation, "message": assistant}))
}

func (h *Handler) buildOnlineImageResult(payload OnlineImageRequest) (map[string]any, error) {
	provider, err := h.providerForID(firstNonEmptyString(payload.ProviderID, "comfly"))
	if err != nil {
		return nil, err
	}
	apiKey := os.Getenv(store.ProviderKeyEnv(stringValue(provider["id"], "")))
	if apiKey == "" {
		return nil, errors.New("未配置 " + stringValue(provider["name"], stringValue(provider["id"], "")) + " 的 API Key")
	}
	model := firstNonEmptyString(payload.Model, firstStringFromAny(provider["image_models"], h.cfg.ImageModel))
	baseURL := providerBaseV1(provider, h.cfg.AIBaseURL)
	client := upstream.NewClient(h.cfg, h.store)
	items, raw, err := client.GenerateImage(baseURL, apiKey, model, payload.Prompt, firstNonEmptyString(payload.Size, "1024x1024"), payload.Quality, referencesToMaps(payload.ReferenceImages), 1)
	if err != nil {
		return nil, err
	}
	images, err := h.saveImageItems(client, items, "online_")
	if err != nil {
		return nil, err
	}
	result := map[string]any{"prompt": payload.Prompt, "images": images, "timestamp": nowSeconds(), "type": "online", "model": model, "provider_id": provider["id"], "provider_name": provider["name"], "params": gin.H{"provider_id": provider["id"], "model": model, "size": payload.Size, "quality": payload.Quality, "reference_images": referencesToMaps(payload.ReferenceImages)}, "raw_usage": raw["usage"]}
	_ = h.store.SaveHistory(result)
	if h.rt != nil {
		h.rt.BroadcastNewImage(result)
	}
	return result, nil
}

func (h *Handler) saveImageItems(client *upstream.Client, items []upstream.ImageItem, prefix string) ([]string, error) {
	images := make([]string, 0, len(items))
	for _, item := range items {
		url, err := client.SaveImageItem(item, prefix)
		if err != nil {
			return nil, err
		}
		images = append(images, url)
	}
	return images, nil
}
