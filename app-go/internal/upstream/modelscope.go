package upstream

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"infinite-canvas/app-go/internal/config"
	"infinite-canvas/app-go/internal/store"
)

type ModelScopeClient struct {
	cfg   *config.Config
	store *store.Store
	http  *http.Client
}

func NewModelScopeClient(cfg *config.Config, stores *store.Store) *ModelScopeClient {
	return &ModelScopeClient{cfg: cfg, store: stores, http: &http.Client{Timeout: 30 * time.Second}}
}

type ModelScopeGenerateInput struct {
	Prompt    string
	APIKey    string
	Model     string
	Size      string
	Width     int
	Height    int
	ImageURLs []string
	Loras     any
	Type      string
	Prefix    string
}

func (c *ModelScopeClient) Generate(input ModelScopeGenerateInput) (map[string]any, error) {
	key := strings.TrimSpace(input.APIKey)
	if key == "" {
		key = strings.TrimSpace(c.cfg.ModelScopeAPIKey)
	}
	if key == "" {
		return nil, errors.New("未配置 ModelScope API Key，请在 API 设置中填写。")
	}
	model := input.Model
	if model == "" {
		model = "Tongyi-MAI/Z-Image-Turbo"
	}
	payload := map[string]any{"model": model, "prompt": strings.TrimSpace(input.Prompt)}
	if input.Width > 0 && input.Height > 0 {
		payload["width"] = input.Width
		payload["height"] = input.Height
		payload["size"] = firstNonEmpty(input.Size, strconvItoaLocal(input.Width)+"x"+strconvItoaLocal(input.Height))
	} else if input.Size != "" {
		payload["size"] = input.Size
	}
	if len(input.ImageURLs) > 0 {
		payload["image_url"] = input.ImageURLs
	}
	if input.Loras != nil {
		payload["loras"] = input.Loras
	}
	apiRoot := modelScopeAPIRoot()
	raw, err := c.postJSON(apiRoot+"/images/generations", key, payload, true)
	if err != nil {
		return nil, err
	}
	taskID := stringFromAny(raw["task_id"])
	if taskID == "" {
		if items, err := extractImageItems(raw); err == nil {
			client := NewClient(c.cfg, c.store)
			images := []string{}
			for _, item := range items {
				url, saveErr := client.SaveImageItem(item, firstNonEmpty(input.Prefix, "ms_"))
				if saveErr != nil {
					return nil, saveErr
				}
				images = append(images, url)
			}
			return c.result(input, model, images, "", raw), nil
		}
		return nil, errors.New("ModelScope 未返回 task_id")
	}
	taskRaw, err := c.waitTask(key, taskID)
	if err != nil {
		return nil, err
	}
	outputs, _ := taskRaw["output_images"].([]any)
	if len(outputs) == 0 {
		return nil, errors.New("ModelScope 成功但没有返回图片")
	}
	firstURL := stringFromAny(outputs[0])
	client := NewClient(c.cfg, c.store)
	localURL, err := client.SaveImageItem(ImageItem{Type: "url", Value: firstURL}, firstNonEmpty(input.Prefix, "ms_"))
	if err != nil {
		return nil, err
	}
	return c.result(input, model, []string{localURL}, taskID, taskRaw), nil
}

func (c *ModelScopeClient) result(input ModelScopeGenerateInput, model string, images []string, taskID string, raw map[string]any) map[string]any {
	resultType := firstNonEmpty(input.Type, "klein")
	result := map[string]any{
		"timestamp": nowFloat(),
		"prompt":    input.Prompt,
		"images":    images,
		"type":      resultType,
		"model":     model,
		"task_id":   taskID,
		"raw":       raw,
	}
	_ = c.store.SaveHistory(result)
	return result
}

func (c *ModelScopeClient) postJSON(endpoint, apiKey string, payload map[string]any, async bool) (map[string]any, error) {
	data, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if async {
		req.Header.Set("X-ModelScope-Async-Mode", "true")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode >= 400 {
		return nil, errors.New(string(body))
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *ModelScopeClient) waitTask(apiKey, taskID string) (map[string]any, error) {
	deadline := time.Now().Add(time.Duration(c.cfg.AIRequestTimeoutSec) * time.Second)
	if c.cfg.AIRequestTimeoutSec <= 0 {
		deadline = time.Now().Add(120 * time.Second)
	}
	var last map[string]any
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		req, _ := http.NewRequest(http.MethodGet, modelScopeAPIRoot()+"/tasks/"+taskID, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("X-ModelScope-Task-Type", "image_generation")
		resp, err := c.http.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, errors.New(string(body))
		}
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			continue
		}
		last = raw
		status := strings.ToUpper(stringFromAny(raw["task_status"]))
		if status == "SUCCEED" || status == "SUCCESS" || status == "SUCCEEDED" {
			return raw, nil
		}
		if status == "FAILED" || status == "FAIL" || status == "ERROR" || status == "CANCELED" || status == "CANCELLED" || status == "TIMEOUT" || status == "REVOKED" {
			return nil, errors.New("MS task " + status + ": " + rawTextLocal(raw))
		}
	}
	return nil, errors.New("MS 生图超时：" + rawTextLocal(last))
}

func stringFromAny(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func nowFloat() float64 {
	return float64(time.Now().UnixMilli()) / 1000.0
}

func rawTextLocal(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	data, _ := json.Marshal(raw)
	return string(data)
}

func strconvItoaLocal(value int) string {
	return fmt.Sprintf("%d", value)
}

func modelScopeAPIRoot() string {
	root := strings.TrimRight(os.Getenv("MODELSCOPE_API_BASE_URL"), "/")
	if root == "" {
		root = "https://api-inference.modelscope.cn/v1"
	}
	if !strings.HasSuffix(root, "/v1") {
		root += "/v1"
	}
	return root
}
