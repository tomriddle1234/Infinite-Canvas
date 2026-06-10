package upstream

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/app-go/internal/config"
	"infinite-canvas/app-go/internal/store"
)

type ImageItem struct {
	Type  string
	Value string
	Raw   map[string]any
}

type Client struct {
	cfg   *config.Config
	store *store.Store
	http  *http.Client
}

func NewClient(cfg *config.Config, stores *store.Store) *Client {
	timeout := time.Duration(cfg.AIRequestTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &Client{cfg: cfg, store: stores, http: &http.Client{Timeout: timeout}}
}

func (c *Client) ChatCompletion(baseURL, apiKey, model string, messages []map[string]any, stream bool) (map[string]any, error) {
	body := map[string]any{"model": model, "messages": messages}
	if stream {
		body["stream"] = true
	}
	return c.postJSON(strings.TrimRight(baseURL, "/")+"/chat/completions", apiKey, body)
}

func (c *Client) GenerateImage(baseURL, apiKey, model, prompt, size, quality string, refs []map[string]any, count int) ([]ImageItem, map[string]any, error) {
	if count < 1 {
		count = 1
	}
	if count > 8 {
		count = 8
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if size == "" {
		size = "1024x1024"
	}
	if len(refs) > 0 {
		items, raw, err := c.imageEdits(baseURL+"/images/edits", apiKey, model, prompt, size, quality, refs, count)
		if err == nil {
			return items, raw, nil
		}
		if strings.EqualFold(model, "gpt-image-2") {
			return nil, nil, err
		}
	}
	body := map[string]any{"model": model, "prompt": prompt, "size": size, "n": count}
	if quality != "" && quality != "auto" {
		body["quality"] = quality
	}
	raw, err := c.postJSON(baseURL+"/images/generations", apiKey, body)
	if err != nil {
		return nil, nil, err
	}
	items, err := extractImageItems(raw)
	return items, raw, err
}

func (c *Client) SaveImageItem(item ImageItem, prefix string) (string, error) {
	filename := prefix + store.NewHexID()[:10] + ".png"
	if item.Type == "b64" {
		data, err := base64.StdEncoding.DecodeString(item.Value)
		if err != nil {
			return "", err
		}
		path := c.store.OutputPathFor(filename, "output")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", err
		}
		return c.store.OutputURLFor(filename, "output"), nil
	}
	if item.Type == "url" {
		resp, err := c.http.Get(item.Value)
		if err != nil {
			return item.Value, nil
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return item.Value, nil
		}
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg") {
			filename = strings.TrimSuffix(filename, ".png") + ".jpg"
		} else if strings.Contains(ct, "webp") {
			filename = strings.TrimSuffix(filename, ".png") + ".webp"
		}
		path := c.store.OutputPathFor(filename, "output")
		out, err := os.Create(path)
		if err != nil {
			return "", err
		}
		defer out.Close()
		if _, err := io.Copy(out, io.LimitReader(resp.Body, 128*1024*1024)); err != nil {
			return "", err
		}
		return c.store.OutputURLFor(filename, "output"), nil
	}
	return "", errors.New("unknown image item type")
}

func (c *Client) postJSON(endpoint, apiKey string, body map[string]any) (map[string]any, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
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

func (c *Client) imageEdits(endpoint, apiKey, model, prompt, size, quality string, refs []map[string]any, count int) ([]ImageItem, map[string]any, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", model)
	_ = writer.WriteField("prompt", prompt)
	_ = writer.WriteField("size", size)
	_ = writer.WriteField("n", strconvItoa(count))
	if quality != "" && quality != "auto" {
		_ = writer.WriteField("quality", quality)
	}
	opened := make([]*os.File, 0)
	defer func() {
		for _, file := range opened {
			_ = file.Close()
		}
	}()
	added := 0
	for _, ref := range refs {
		path := c.store.OutputFileFromURL(ref["url"])
		if path == "" {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		opened = append(opened, file)
		part, err := writer.CreateFormFile("image", filepath.Base(path))
		if err != nil {
			return nil, nil, err
		}
		if _, err := io.Copy(part, file); err != nil {
			return nil, nil, err
		}
		added++
		if added >= 4 {
			break
		}
	}
	if added == 0 {
		return nil, nil, errors.New("参考图必须是本地输出图片")
	}
	_ = writer.Close()
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if resp.StatusCode >= 400 {
		return nil, nil, errors.New(string(data))
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, err
	}
	items, err := extractImageItems(raw)
	return items, raw, err
}

func TextFromChatResponse(raw map[string]any) string {
	data := unwrapAPIMart(raw)
	choices, _ := data["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	first, _ := choices[0].(map[string]any)
	message, _ := first["message"].(map[string]any)
	content := message["content"]
	if text, ok := content.(string); ok {
		return text
	}
	if parts, ok := content.([]any); ok {
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if obj, ok := part.(map[string]any); ok {
				if text, ok := obj["text"].(string); ok && text != "" {
					out = append(out, text)
				}
			}
		}
		return strings.Join(out, "\n")
	}
	return ""
}

func TextDeltaFromChatChunk(raw map[string]any) string {
	choices, _ := raw["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	first, _ := choices[0].(map[string]any)
	delta, _ := first["delta"].(map[string]any)
	if text, ok := delta["content"].(string); ok {
		return text
	}
	return ""
}

func unwrapAPIMart(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	if _, hasChoices := raw["choices"]; !hasChoices {
		if data, ok := raw["data"].(map[string]any); ok {
			return data
		}
	}
	return raw
}

func extractImageItems(raw map[string]any) ([]ImageItem, error) {
	data := raw
	if nested, ok := raw["data"].(map[string]any); ok {
		if deeper, ok := nested["data"].(map[string]any); ok {
			data = deeper
		} else {
			data = nested
		}
	}
	if result, ok := data["result"].(map[string]any); ok {
		if images, ok := result["images"].([]any); ok && len(images) > 0 {
			return imageItemsFromArray(images)
		}
	}
	if arr, ok := data["data"].([]any); ok {
		return imageItemsFromArray(arr)
	}
	if arr, ok := raw["data"].([]any); ok {
		return imageItemsFromArray(arr)
	}
	return nil, errors.New("生图接口没有返回图片数据")
}

func imageItemsFromArray(arr []any) ([]ImageItem, error) {
	out := make([]ImageItem, 0, len(arr))
	for _, item := range arr {
		obj, _ := item.(map[string]any)
		if obj == nil {
			continue
		}
		if value, ok := obj["b64_json"].(string); ok && value != "" {
			out = append(out, ImageItem{Type: "b64", Value: value, Raw: obj})
			continue
		}
		if value, ok := obj["url"].(string); ok && value != "" {
			out = append(out, ImageItem{Type: "url", Value: value, Raw: obj})
			continue
		}
		if urls, ok := obj["url"].([]any); ok && len(urls) > 0 {
			if value, ok := urls[0].(string); ok && value != "" {
				out = append(out, ImageItem{Type: "url", Value: value, Raw: obj})
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("无法识别生图接口返回格式")
	}
	return out, nil
}

func strconvItoa(value int) string { return strconv.Itoa(value) }
