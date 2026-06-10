package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/store"
)

type CanvasVideoRequest struct {
	Prompt          string        `json:"prompt"`
	ProviderID      string        `json:"provider_id"`
	Model           string        `json:"model"`
	Duration        int           `json:"duration"`
	AspectRatio     string        `json:"aspect_ratio"`
	Resolution      string        `json:"resolution"`
	Size            string        `json:"size"`
	Images          []AIReference `json:"images"`
	Videos          []string      `json:"videos"`
	EnhancePrompt   bool          `json:"enhance_prompt"`
	EnableUpsample  bool          `json:"enable_upsample"`
	Watermark       bool          `json:"watermark"`
	Seed            *int64        `json:"seed"`
	CameraFixed     bool          `json:"camerafixed"`
	ReturnLastFrame bool          `json:"return_last_frame"`
	GenerateAudio   bool          `json:"generate_audio"`
}

func (h *Handler) CanvasVideo(c *gin.Context) {
	var payload CanvasVideoRequest
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "提示词不能为空"})
		return
	}
	provider, err := h.providerForID(firstNonEmptyString(payload.ProviderID, "comfly"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	baseURL := videoAPIRoot(provider, h.cfg.AIBaseURL)
	if baseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": stringValue(provider["name"], stringValue(provider["id"], "")) + " 未配置 Base URL"})
		return
	}
	apiKey := strings.TrimSpace(os.Getenv(store.ProviderKeyEnv(stringValue(provider["id"], ""))))
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "未配置 " + stringValue(provider["name"], stringValue(provider["id"], "")) + " 的 API Key，请在 API 设置中填写。"})
		return
	}

	isAPIMart := isAPIMartProvider(provider)
	model := firstNonEmptyString(payload.Model, "veo3-fast")
	submitURL := videoSubmitURL(baseURL, isAPIMart)
	client := &http.Client{Timeout: time.Duration(maxIntLocal(h.cfg.ImageTaskTimeoutSec, 120)) * time.Second}
	body, err := h.videoRequestBody(client, provider, payload, model, isAPIMart)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	raw, status, err := postVideoJSON(client, submitURL, apiKey, body)
	if err != nil {
		c.JSON(statusOrBadGateway(status), gin.H{"detail": err.Error()})
		return
	}
	taskID := extractTaskID(raw)
	result := raw
	if taskID != "" && len(videoOutputURLs(raw)) == 0 {
		result, err = h.waitForVideoTask(client, provider, taskID, apiKey)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
			return
		}
	}
	urls := videoOutputURLs(result)
	if len(urls) == 0 {
		c.JSON(http.StatusBadGateway, gin.H{"detail": "视频生成成功但没有返回视频"})
		return
	}
	local := make([]string, 0, len(urls))
	for _, remoteURL := range urls {
		local = append(local, h.saveRemoteVideoWithClient(client, remoteURL, "video_"))
	}
	c.JSON(http.StatusOK, gin.H{"videos": local, "task_id": taskID, "raw": result})
}

func (h *Handler) videoRequestBody(client *http.Client, provider map[string]any, payload CanvasVideoRequest, requestedModel string, isAPIMart bool) (map[string]any, error) {
	if isAPIMart {
		model := requestedModel
		if isAPIMartVeo31Model(requestedModel) {
			model = apimartVeo31Model(requestedModel)
			if model == "veo3.1-lite" && len(payload.Images) > 0 {
				return nil, errors.New("veo3.1-lite 不支持图片输入，请改用 veo3.1-fast 或 veo3.1-quality。")
			}
			body := map[string]any{
				"prompt":       payload.Prompt,
				"model":        model,
				"duration":     8,
				"aspect_ratio": apimartVeo31Aspect(payload.AspectRatio),
				"resolution":   apimartVeo31Resolution(payload.Resolution),
			}
			images := h.apimartVideoImages(client, provider, payload.Images, 3)
			if len(images) > 0 && model != "veo3.1-lite" {
				if model == "veo3.1-quality" && len(images) > 2 {
					images = images[:2]
				}
				body["image_urls"] = images
				if len(images) == 2 {
					body["generation_type"] = "frame"
				} else if len(images) >= 3 && model != "veo3.1-quality" {
					body["generation_type"] = "reference"
				}
				body["official_fallback"] = false
			}
			return body, nil
		}
		body := map[string]any{
			"prompt":     payload.Prompt,
			"model":      firstNonEmptyString(model, "doubao-seedance-2.0"),
			"duration":   payload.Duration,
			"size":       apimartVideoSize(firstNonEmptyString(payload.AspectRatio, payload.Size)),
			"resolution": firstNonEmptyString(payload.Resolution, "480p"),
		}
		roleImages := h.apimartVideoImagesWithRoles(client, provider, payload.Images, 9)
		if len(roleImages) > 0 {
			body["image_with_roles"] = roleImages
		} else if images := h.apimartVideoImages(client, provider, payload.Images, 9); len(images) > 0 {
			body["image_urls"] = images
		}
		if len(payload.Videos) > 0 {
			body["video_urls"] = cleanStringList(payload.Videos)
		}
		if payload.Seed != nil {
			body["seed"] = *payload.Seed
		}
		if payload.ReturnLastFrame {
			body["return_last_frame"] = true
		}
		if payload.GenerateAudio {
			body["generate_audio"] = true
		}
		return body, nil
	}

	body := map[string]any{"prompt": payload.Prompt, "model": requestedModel, "duration": payload.Duration, "watermark": payload.Watermark}
	if payload.AspectRatio != "" {
		body["aspect_ratio"] = payload.AspectRatio
		body["ratio"] = payload.AspectRatio
	}
	if payload.Size != "" {
		body["size"] = payload.Size
	}
	if payload.Resolution != "" {
		body["resolution"] = payload.Resolution
	}
	images := []string{}
	for _, ref := range payload.Images {
		if ref.URL == "" {
			continue
		}
		images = append(images, h.referenceDataOrURL(ref.URL))
		if len(images) >= 4 {
			break
		}
	}
	if len(images) > 0 {
		body["images"] = images
	}
	if len(payload.Videos) > 0 {
		body["videos"] = cleanStringList(payload.Videos)
	}
	if payload.EnhancePrompt {
		body["enhance_prompt"] = true
	}
	if payload.EnableUpsample {
		body["enable_upsample"] = true
	}
	if payload.Seed != nil {
		body["seed"] = *payload.Seed
	}
	if payload.CameraFixed {
		body["camerafixed"] = true
	}
	if payload.ReturnLastFrame {
		body["return_last_frame"] = true
	}
	if payload.GenerateAudio {
		body["generate_audio"] = true
	}
	return body, nil
}

func (h *Handler) apimartVideoImages(client *http.Client, provider map[string]any, refs []AIReference, limit int) []string {
	out := []string{}
	for _, ref := range refs {
		up := h.uploadImageForAPIMart(client, provider, ref.URL)
		if validAPIMartVideoImage(up) {
			out = append(out, up)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (h *Handler) apimartVideoImagesWithRoles(client *http.Client, provider map[string]any, refs []AIReference, limit int) []map[string]any {
	out := []map[string]any{}
	for _, ref := range refs {
		role := strings.TrimSpace(ref.Role)
		if role != "first_frame" && role != "last_frame" && role != "reference_image" {
			continue
		}
		up := h.uploadImageForAPIMart(client, provider, ref.URL)
		if validAPIMartVideoImage(up) {
			out = append(out, map[string]any{"url": up, "role": role})
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (h *Handler) uploadImageForAPIMart(client *http.Client, provider map[string]any, refURL string) string {
	refURL = strings.TrimSpace(refURL)
	if validAPIMartVideoImage(refURL) {
		return refURL
	}
	path := h.store.OutputFileFromURL(refURL)
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return ""
	}
	if _, err := io.Copy(part, io.LimitReader(file, 10*1024*1024)); err != nil {
		return ""
	}
	_ = writer.Close()
	req, err := http.NewRequest(http.MethodPost, videoAPIRoot(provider, h.cfg.AIBaseURL)+"/v1/uploads/images", body)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv(store.ProviderKeyEnv(stringValue(provider["id"], ""))))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return ""
	}
	var raw any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&raw); err != nil {
		return ""
	}
	return extractAPIMartAssetURL(raw)
}

func (h *Handler) referenceDataOrURL(rawURL string) string {
	text := strings.TrimSpace(rawURL)
	if text == "" || strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") || strings.HasPrefix(text, "data:") {
		return text
	}
	path := h.store.OutputFileFromURL(text)
	if path == "" {
		return text
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return text
	}
	return "data:" + store.ContentTypeForPath(path) + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func postVideoJSON(client *http.Client, endpoint, apiKey string, body map[string]any) (map[string]any, int, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, errors.New(string(data))
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

func (h *Handler) waitForVideoTask(client *http.Client, provider map[string]any, taskID, apiKey string) (map[string]any, error) {
	taskURL := videoTaskURL(provider, h.cfg.AIBaseURL, taskID)
	deadline := time.Now().Add(time.Duration(maxIntLocal(h.cfg.ImageTaskTimeoutSec, 120)) * time.Second)
	delay := 2 * time.Second
	var last map[string]any
	for time.Now().Before(deadline) {
		time.Sleep(delay)
		raw, status, err := getVideoJSON(client, taskURL, apiKey)
		if err != nil {
			return nil, err
		}
		if status >= 400 {
			return nil, errors.New(rawTextLocal(raw))
		}
		last = raw
		state := strings.ToUpper(videoTaskStatus(raw))
		if videoTaskSucceeded(state) || (state == "" && len(videoOutputURLs(raw)) > 0) {
			return raw, nil
		}
		if videoTaskFailed(state) {
			return nil, errors.New("视频生成任务失败：" + rawTextLocal(raw))
		}
		if delay < 12*time.Second {
			delay = time.Duration(float64(delay) * 1.6)
		}
	}
	return nil, errors.New("视频生成任务超时：" + firstNonEmptyString(rawTextLocal(last), taskID))
}

func getVideoJSON(client *http.Client, endpoint, apiKey string) (map[string]any, int, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if raw == nil {
		raw = map[string]any{"raw": string(data)}
	}
	return raw, resp.StatusCode, nil
}

func (h *Handler) saveRemoteVideoWithClient(client *http.Client, remoteURL, prefix string) string {
	if strings.TrimSpace(remoteURL) == "" || strings.HasPrefix(remoteURL, "/assets/") || strings.HasPrefix(remoteURL, "/output/") {
		return remoteURL
	}
	resp, err := client.Get(remoteURL)
	if err != nil {
		return remoteURL
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return remoteURL
	}
	ext := ".mp4"
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	switch {
	case strings.Contains(ct, "webm"):
		ext = ".webm"
	case strings.Contains(ct, "quicktime"), strings.Contains(ct, "mov"):
		ext = ".mov"
	}
	filename := prefix + store.NewHexID()[:10] + ext
	path := h.store.OutputPathFor(filename, "output")
	out, err := os.Create(path)
	if err != nil {
		return remoteURL
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 1024*1024*1024)); err != nil {
		return remoteURL
	}
	return h.store.OutputURLFor(filename, "output")
}

func videoAPIRoot(provider map[string]any, fallback string) string {
	base := strings.TrimRight(stringValue(provider["base_url"], fallback), "/")
	if strings.HasSuffix(base, "/v1") || strings.HasSuffix(base, "/v2") {
		base = base[:strings.LastIndex(base, "/")]
	}
	return base
}

func videoSubmitURL(baseURL string, isAPIMart bool) string {
	if isAPIMart {
		if strings.HasSuffix(baseURL, "/v1") {
			return baseURL + "/videos/generations"
		}
		return baseURL + "/v1/videos/generations"
	}
	return baseURL + "/v2/videos/generations"
}

func videoTaskURL(provider map[string]any, fallback, taskID string) string {
	base := videoAPIRoot(provider, fallback)
	if isAPIMartProvider(provider) {
		return base + "/v1/tasks/" + url.PathEscape(taskID) + "?language=zh"
	}
	return base + "/v2/videos/generations/" + url.PathEscape(taskID)
}

func isAPIMartProvider(provider map[string]any) bool {
	return stringValue(provider["protocol"], "") == "apimart" || strings.Contains(strings.ToLower(stringValue(provider["base_url"], "")), "apimart.ai")
}

func extractTaskID(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	if text := stringValue(raw["task_id"], ""); text != "" {
		return text
	}
	if text := stringValue(raw["id"], ""); strings.HasPrefix(text, "task") {
		return text
	}
	if nested, ok := raw["data"].(map[string]any); ok {
		return extractTaskID(nested)
	}
	if list, ok := raw["data"].([]any); ok && len(list) > 0 {
		if first, ok := list[0].(map[string]any); ok {
			return extractTaskID(first)
		}
	}
	return ""
}

func videoOutputURLs(raw map[string]any) []string {
	seen := map[string]bool{}
	out := []string{}
	var collect func(any)
	collect = func(value any) {
		switch typed := value.(type) {
		case string:
			if typed != "" && !seen[typed] && (strings.HasPrefix(typed, "http://") || strings.HasPrefix(typed, "https://") || strings.HasPrefix(typed, "/output/") || strings.HasPrefix(typed, "/assets/")) {
				out = append(out, typed)
				seen[typed] = true
			}
		case []any:
			for _, item := range typed {
				collect(item)
			}
		case map[string]any:
			for _, key := range []string{"url", "video_url", "videoUrl", "mp4_url", "mp4Url", "output", "output_url", "outputUrl", "download_url", "downloadUrl", "video", "src", "uri", "preview_url", "previewUrl"} {
				collect(typed[key])
			}
			collect(typed["videos"])
			collect(typed["outputs"])
		}
	}
	collect(raw)
	if data, ok := raw["data"].(map[string]any); ok {
		collect(data)
	}
	if data, ok := raw["data"].([]any); ok {
		collect(data)
	}
	return out
}

func videoTaskStatus(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	if data, ok := raw["data"].(map[string]any); ok {
		if text := firstNonEmptyString(stringValue(data["status"], ""), stringValue(data["task_status"], "")); text != "" {
			return text
		}
	}
	return firstNonEmptyString(stringValue(raw["status"], ""), stringValue(raw["task_status"], ""))
}

func videoTaskSucceeded(status string) bool {
	switch status {
	case "SUCCESS", "SUCCEED", "SUCCEEDED", "COMPLETED", "COMPLETE", "DONE", "FINISHED", "FINISH", "OK", "READY":
		return true
	default:
		return false
	}
}

func videoTaskFailed(status string) bool {
	switch status {
	case "FAILURE", "FAILED", "FAIL", "ERROR", "ERRORED", "CANCELED", "CANCELLED", "TIMEOUT", "TIMEDOUT", "REJECTED", "EXPIRED":
		return true
	default:
		return false
	}
}

func extractAPIMartAssetURL(value any) string {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if found := extractAPIMartAssetURL(item); found != "" {
				return found
			}
		}
	case map[string]any:
		for _, key := range []string{"url", "asset_url", "assetUrl", "uri", "file_url", "fileUrl"} {
			if text := stringValue(typed[key], ""); validAPIMartVideoImage(text) {
				return text
			}
		}
		for _, key := range []string{"asset_id", "assetId", "file_id", "fileId", "id"} {
			if text := stringValue(typed[key], ""); text != "" {
				if strings.HasPrefix(text, "asset://") {
					return text
				}
				return "asset://" + text
			}
		}
		for _, key := range []string{"data", "file", "asset", "result"} {
			if found := extractAPIMartAssetURL(typed[key]); found != "" {
				return found
			}
		}
	}
	return ""
}

func validAPIMartVideoImage(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "asset://")
}

func isAPIMartVeo31Model(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "veo3.1")
}

func apimartVeo31Model(model string) string {
	value := strings.ToLower(strings.TrimSpace(model))
	aliases := map[string]string{"veo3.1": "veo3.1-fast", "veo3.1-pro": "veo3.1-quality", "veo3.1-preview": "veo3.1-fast"}
	if aliases[value] != "" {
		value = aliases[value]
	}
	if value == "" {
		value = "veo3.1-fast"
	}
	if value == "veo3.1-fast" || value == "veo3.1-quality" || value == "veo3.1-lite" {
		return value
	}
	return "veo3.1-fast"
}

func apimartVeo31Aspect(aspect string) string {
	if aspect == "9:16" {
		return "9:16"
	}
	return "16:9"
}

func apimartVeo31Resolution(resolution string) string {
	value := strings.ToLower(strings.TrimSpace(resolution))
	aliases := map[string]string{"": "720p", "auto": "720p", "480p": "720p", "780p": "720p", "1080": "1080p", "4k": "4k"}
	if mapped, ok := aliases[value]; ok {
		value = mapped
	}
	if value == "720p" || value == "1080p" || value == "4k" {
		return value
	}
	return "720p"
}

func apimartVideoSize(size string) string {
	value := strings.TrimSpace(size)
	if value == "keep_ratio" {
		return "adaptive"
	}
	switch value {
	case "16:9", "9:16", "1:1", "4:3", "3:4", "21:9", "adaptive":
		return value
	default:
		return "16:9"
	}
}

func statusOrBadGateway(status int) int {
	if status >= 400 && status < 500 {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

func rawTextLocal(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	data, _ := json.Marshal(raw)
	return string(data)
}

func maxIntLocal(a, b int) int {
	if a > b {
		return a
	}
	return b
}
