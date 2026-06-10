package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/upstream"
)

type CloudGenRequest struct {
	Prompt     string   `json:"prompt"`
	APIKey     string   `json:"api_key"`
	Model      string   `json:"model"`
	Resolution string   `json:"resolution"`
	Type       string   `json:"type"`
	ImageURLs  []string `json:"image_urls"`
	Loras      any      `json:"loras"`
	ClientID   string   `json:"client_id"`
}

type MSGenerateRequest struct {
	Prompt    string   `json:"prompt"`
	APIKey    string   `json:"api_key"`
	Model     string   `json:"model"`
	ImageURLs []string `json:"image_urls"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Size      string   `json:"size"`
	Loras     any      `json:"loras"`
	ClientID  string   `json:"client_id"`
}

type AngleGenerateRequest struct {
	Prompt   string `json:"prompt"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	ImageURL string `json:"image_url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Size     string `json:"size"`
	ClientID string `json:"client_id"`
}

type AnglePollRequest struct {
	TaskID   string `json:"task_id"`
	APIKey   string `json:"api_key"`
	ClientID string `json:"client_id"`
}

func (h *Handler) GenerateCloud(c *gin.Context) {
	var payload CloudGenRequest
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "提示词不能为空"})
		return
	}
	result, err := upstream.NewModelScopeClient(h.cfg, h.store).Generate(upstream.ModelScopeGenerateInput{
		Prompt: payload.Prompt, APIKey: payload.APIKey, Model: firstNonEmptyString(payload.Model, "Tongyi-MAI/Z-Image-Turbo"), Size: firstNonEmptyString(payload.Resolution, "1024x1024"), Type: firstNonEmptyString(payload.Type, "cloud"), Loras: payload.Loras, Prefix: "cloud_",
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	if images, ok := result["images"].([]string); ok && len(images) > 0 {
		c.JSON(http.StatusOK, gin.H{"url": images[0]})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) ModelScopeGenerate(c *gin.Context) {
	var payload MSGenerateRequest
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "提示词不能为空"})
		return
	}
	result, err := upstream.NewModelScopeClient(h.cfg, h.store).Generate(upstream.ModelScopeGenerateInput{
		Prompt: payload.Prompt, APIKey: payload.APIKey, Model: firstNonEmptyString(payload.Model, "black-forest-labs/FLUX.2-klein-9B"), Size: payload.Size, Width: payload.Width, Height: payload.Height, ImageURLs: payload.ImageURLs, Loras: payload.Loras, Type: "klein", Prefix: "ms_",
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	if images, ok := result["images"].([]string); ok && len(images) > 0 {
		c.JSON(http.StatusOK, gin.H{"url": images[0], "task_id": result["task_id"]})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) AngleGenerate(c *gin.Context) {
	var payload AngleGenerateRequest
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "提示词不能为空"})
		return
	}
	images := []string{}
	if payload.ImageURL != "" {
		images = append(images, payload.ImageURL)
	}
	result, err := upstream.NewModelScopeClient(h.cfg, h.store).Generate(upstream.ModelScopeGenerateInput{
		Prompt: payload.Prompt, APIKey: payload.APIKey, Model: firstNonEmptyString(payload.Model, "Qwen/Qwen-Image-Edit-2511"), Size: payload.Size, Width: payload.Width, Height: payload.Height, ImageURLs: images, Type: "angle", Prefix: "cloud_angle_",
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	if out, ok := result["images"].([]string); ok && len(out) > 0 {
		c.JSON(http.StatusOK, gin.H{"url": out[0], "task_id": result["task_id"]})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) AnglePollStatus(c *gin.Context) {
	var payload AnglePollRequest
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.TaskID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "缺少 task_id"})
		return
	}
	apiKey := strings.TrimSpace(firstNonEmptyString(payload.APIKey, h.cfg.ModelScopeAPIKey))
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "未提供 ModelScope API Key"})
		return
	}
	base := strings.TrimRight(os.Getenv("MODELSCOPE_API_BASE_URL"), "/")
	if base == "" {
		base = "https://api-inference.modelscope.cn/v1"
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	endpoint := base + "/tasks/" + payload.TaskID
	client := &http.Client{Timeout: 30 * time.Second}
	deadline := time.Now().Add(time.Duration(maxIntLocal(h.cfg.ImageTaskTimeoutSec, 600)) * time.Second)
	for time.Now().Before(deadline) {
		raw, err := modelscopeTaskStatus(client, endpoint, apiKey)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
			return
		}
		status := strings.ToUpper(stringValue(raw["task_status"], ""))
		if status == "SUCCEED" {
			images := stringArrayAny(raw["output_images"])
			if len(images) == 0 {
				c.JSON(http.StatusBadGateway, gin.H{"detail": "ModelScope task succeeded but returned no image"})
				return
			}
			local, err := upstream.NewClient(h.cfg, h.store).SaveImageItem(upstream.ImageItem{Type: "url", Value: images[0]}, "cloud_angle_")
			if err != nil {
				local = images[0]
			}
			record := map[string]any{"timestamp": nowSeconds(), "prompt": "Resumed " + payload.TaskID, "images": []string{local}, "type": "angle"}
			_ = h.store.SaveHistory(record)
			c.JSON(http.StatusOK, gin.H{"url": local})
			return
		}
		if status == "FAILED" || status == "FAIL" || status == "ERROR" {
			c.JSON(http.StatusBadGateway, gin.H{"detail": "ModelScope task failed: " + rawTextLocal(raw)})
			return
		}
		time.Sleep(2 * time.Second)
	}
	c.JSON(http.StatusGatewayTimeout, gin.H{"detail": "ModelScope task polling timed out: " + payload.TaskID})
}

func modelscopeTaskStatus(client *http.Client, endpoint, apiKey string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ModelScope-Async-Mode", "true")
	req.Header.Set("X-ModelScope-Task-Type", "image_generation")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode >= 400 {
		return nil, errors.New(string(data))
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func stringArrayAny(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if text := stringValue(item, ""); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
