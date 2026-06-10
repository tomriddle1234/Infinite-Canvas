package upstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"

	"infinite-canvas/app-go/internal/config"
	"infinite-canvas/app-go/internal/store"
)

var seedreamModels = map[string]string{
	"seedream-4.0": "doubao-seedream-4-0-250828",
	"seedream-4.5": "doubao-seedream-4-5-251128",
	"seedream-5.0": "doubao-seedream-5-0-260128",
}

var seedanceModels = map[string]string{
	"seedance-1.5-pro":  "doubao-seedance-1-5-pro-251215",
	"seedance-2.0":      "doubao-seedance-2-0-260128",
	"seedance-2.0-fast": "doubao-seedance-2-0-fast-260128",
}

type VolcengineClient struct {
	cfg   *config.Config
	store *store.Store
	http  *http.Client
}

type SeedreamInput struct {
	Prompt          string
	Model           string
	Size            string
	Seed            *int64
	Watermark       bool
	ReferenceImages []map[string]any
}

type SeedanceInput struct {
	Prompt          string
	Model           string
	Duration        int64
	AspectRatio     string
	Resolution      string
	Seed            *int64
	GenerateAudio   bool
	ReturnLastFrame bool
	ReferenceImages []map[string]any
	ReferenceVideos []map[string]any
	ReferenceAudios []map[string]any
}

type SeedanceSubmitted struct {
	TaskIDs     []string
	Model       string
	Raw         map[string]any
	SubmittedAt float64
}

func NewVolcengineClient(cfg *config.Config, stores *store.Store) *VolcengineClient {
	timeout := time.Duration(cfg.AIRequestTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &VolcengineClient{cfg: cfg, store: stores, http: &http.Client{Timeout: timeout}}
}

func (c *VolcengineClient) GenerateSeedream(ctx context.Context, input SeedreamInput) ([]ImageItem, map[string]any, string, error) {
	client, err := c.arkClient()
	if err != nil {
		return nil, nil, "", err
	}
	modelName := validVolcengineModel(input.Model, seedreamModels, seedreamModels["seedream-4.5"])
	responseFormat := arkmodel.GenerateImagesResponseFormatBase64
	sequential := arkmodel.SequentialImageGeneration(arkmodel.SequentialImageGenerationDisabled)
	size := NormalizeSeedreamSize(input.Size, modelName)
	req := arkmodel.GenerateImagesRequest{
		Model:                     modelName,
		Prompt:                    input.Prompt,
		Size:                      &size,
		ResponseFormat:            &responseFormat,
		Watermark:                 &input.Watermark,
		SequentialImageGeneration: &sequential,
		Seed:                      input.Seed,
	}
	refs := make([]string, 0, len(input.ReferenceImages))
	for _, ref := range input.ReferenceImages {
		value, err := c.referenceURL(ref)
		if err != nil {
			return nil, nil, "", err
		}
		if value != "" {
			refs = append(refs, value)
		}
		if len(refs) >= 9 {
			break
		}
	}
	if len(refs) == 1 {
		req.Image = refs[0]
	} else if len(refs) > 1 {
		req.Image = refs
	}
	resp, err := client.GenerateImages(ctx, req)
	if err != nil {
		return nil, nil, modelName, err
	}
	raw := structToMap(resp)
	items := make([]ImageItem, 0, len(resp.Data))
	for _, item := range resp.Data {
		if item == nil {
			continue
		}
		obj := structToMap(item)
		if item.B64Json != nil && *item.B64Json != "" {
			items = append(items, ImageItem{Type: "b64", Value: *item.B64Json, Raw: obj})
		} else if item.Url != nil && *item.Url != "" {
			items = append(items, ImageItem{Type: "url", Value: *item.Url, Raw: obj})
		}
	}
	if len(items) == 0 {
		return nil, raw, modelName, errors.New("Seedream 没有返回图片数据")
	}
	return items, raw, modelName, nil
}

func (c *VolcengineClient) SubmitSeedance(ctx context.Context, input SeedanceInput) (SeedanceSubmitted, error) {
	client, err := c.arkClient()
	if err != nil {
		return SeedanceSubmitted{}, err
	}
	modelName := validVolcengineModel(input.Model, seedanceModels, seedanceModels["seedance-1.5-pro"])
	content := []*arkmodel.CreateContentGenerationContentItem{{
		Type: arkmodel.ContentGenerationContentItemTypeText,
		Text: stringPtr(input.Prompt),
	}}
	for i, ref := range input.ReferenceImages {
		value, err := c.referenceURL(ref)
		if err != nil {
			return SeedanceSubmitted{}, err
		}
		if value == "" {
			continue
		}
		role := strings.TrimSpace(stringValueAny(ref["role"]))
		if role == "" {
			role = "reference_image"
			if i == 0 {
				role = "first_frame"
			} else if i == 1 {
				role = "last_frame"
			}
		}
		content = append(content, &arkmodel.CreateContentGenerationContentItem{
			Type:     arkmodel.ContentGenerationContentItemTypeImage,
			ImageURL: &arkmodel.ImageURL{URL: value},
			Role:     &role,
		})
	}
	for _, ref := range input.ReferenceVideos {
		value := strings.TrimSpace(stringValueAny(ref["url"]))
		if value == "" {
			continue
		}
		content = append(content, &arkmodel.CreateContentGenerationContentItem{
			Type:     arkmodel.ContentGenerationContentItemTypeVideo,
			VideoURL: &arkmodel.VideoUrl{Url: value},
		})
	}
	for _, ref := range input.ReferenceAudios {
		value := strings.TrimSpace(stringValueAny(ref["url"]))
		if value == "" {
			continue
		}
		content = append(content, &arkmodel.CreateContentGenerationContentItem{
			Type:     arkmodel.ContentGenerationContentItemTypeAudio,
			AudioURL: &arkmodel.AudioUrl{Url: value},
		})
	}
	duration := seedanceDuration(modelName, input.Duration)
	ratio := nonEmptyString(input.AspectRatio, "16:9")
	resolution := nonEmptyString(input.Resolution, "720p")
	req := arkmodel.CreateContentGenerationTaskRequest{
		Model:           modelName,
		Content:         content,
		ReturnLastFrame: &input.ReturnLastFrame,
		Duration:        &duration,
		Ratio:           &ratio,
		Resolution:      &resolution,
		GenerateAudio:   &input.GenerateAudio,
		Seed:            input.Seed,
	}
	resp, err := client.CreateContentGenerationTask(ctx, req)
	if err != nil {
		return SeedanceSubmitted{}, err
	}
	raw := structToMap(resp)
	if strings.TrimSpace(resp.ID) == "" {
		return SeedanceSubmitted{}, errors.New("Seedance 没有返回 task_id")
	}
	return SeedanceSubmitted{
		TaskIDs:     []string{resp.ID},
		Model:       modelName,
		Raw:         raw,
		SubmittedAt: float64(time.Now().UnixMilli()) / 1000.0,
	}, nil
}

func (c *VolcengineClient) PollSeedance(ctx context.Context, taskIDs []string) (map[string]any, error) {
	client, err := c.arkClient()
	if err != nil {
		return nil, err
	}
	tasks := make([]map[string]any, 0, len(taskIDs))
	videos := make([]string, 0)
	failed := make([]map[string]any, 0)
	pending := make([]string, 0)
	for _, id := range taskIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		resp, err := client.GetContentGenerationTask(ctx, arkmodel.GetContentGenerationTaskRequest{ID: id})
		if err != nil {
			failed = append(failed, map[string]any{"task_id": id, "error": err.Error()})
			continue
		}
		item := seedanceTaskSummary(resp.ID, resp.Model, resp.Status, resp.Content, resp.CreatedAt, resp.UpdatedAt, resp.Error, structToMap(resp))
		tasks = append(tasks, item)
		collectSeedanceStatus(item, &videos, &failed, &pending)
	}
	status := "pending"
	if len(failed) > 0 && len(pending) == 0 && len(videos) == 0 {
		status = "failed"
	} else if len(pending) == 0 && len(failed) == 0 && len(tasks) > 0 {
		status = "succeeded"
	} else if len(videos) > 0 {
		status = "partial"
	}
	return map[string]any{"status": status, "tasks": tasks, "videos": videos, "failed": failed, "pending": pending}, nil
}

func (c *VolcengineClient) ListSeedanceTasks(ctx context.Context, modelName, status string, pageSize int) (map[string]any, error) {
	client, err := c.arkClient()
	if err != nil {
		return nil, err
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	filter := &arkmodel.ListContentGenerationTasksFilter{}
	if mapped := validVolcengineModel(modelName, seedanceModels, ""); mapped != "" {
		filter.Model = &mapped
	}
	if status = strings.TrimSpace(status); status != "" && status != "all" {
		filter.Status = &status
	}
	resp, err := client.ListContentGenerationTasks(ctx, arkmodel.ListContentGenerationTasksRequest{
		PageSize: &pageSize,
		Filter:   filter,
	})
	if err != nil {
		return nil, err
	}
	tasks := make([]map[string]any, 0, len(resp.Items))
	for _, item := range resp.Items {
		tasks = append(tasks, seedanceTaskSummary(item.ID, item.Model, item.Status, item.Content, item.CreatedAt, item.UpdatedAt, item.FailureReason, structToMap(item)))
	}
	return map[string]any{"tasks": tasks, "total": resp.Total}, nil
}

func (c *VolcengineClient) arkClient() (*arkruntime.Client, error) {
	key := strings.TrimSpace(c.cfg.VolcengineArkAPIKey)
	if key == "" {
		return nil, errors.New("未配置 VOLCENGINE_ARK_API_KEY，请在 API 设置中填写。")
	}
	opts := []arkruntime.ConfigOption{arkruntime.WithTimeout(time.Duration(c.cfg.AIRequestTimeoutSec) * time.Second)}
	if c.cfg.VolcengineArkBaseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(c.cfg.VolcengineArkBaseURL))
	}
	return arkruntime.NewClientWithApiKey(key, opts...), nil
}

func (c *VolcengineClient) referenceURL(ref map[string]any) (string, error) {
	value := strings.TrimSpace(stringValueAny(ref["url"]))
	if value == "" || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "data:") {
		return value, nil
	}
	path := c.store.OutputFileFromURL(value)
	if path == "" {
		return value, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取参考图失败：%w", err)
	}
	mime := store.ContentTypeForPath(path)
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func validVolcengineModel(value string, aliases map[string]string, fallback string) string {
	text := strings.TrimSpace(value)
	if mapped := aliases[text]; mapped != "" {
		return mapped
	}
	if text != "" {
		return text
	}
	return fallback
}

func seedanceDuration(model string, duration int64) int64 {
	if duration <= 0 {
		duration = 5
	}
	maxDuration := int64(15)
	text := strings.ToLower(model)
	if strings.Contains(text, "seedance-1-5") || strings.Contains(text, "seedance-1.5") {
		maxDuration = 12
	}
	if duration > maxDuration {
		return maxDuration
	}
	return duration
}

func seedanceTaskSummary(id, modelName, status string, content arkmodel.Content, createdAt, updatedAt int64, errObj any, raw map[string]any) map[string]any {
	video := firstNonEmptyVolc(content.VideoURL, content.FileURL)
	item := map[string]any{
		"task_id":        id,
		"id":             id,
		"model":          modelName,
		"status":         status,
		"video_url":      video,
		"last_frame_url": content.LastFrameURL,
		"created_at":     createdAt,
		"updated_at":     updatedAt,
		"raw":            raw,
	}
	if errObj != nil {
		item["error"] = structToMap(errObj)
	}
	return item
}

func collectSeedanceStatus(item map[string]any, videos *[]string, failed *[]map[string]any, pending *[]string) {
	status := strings.ToLower(stringValueAny(item["status"]))
	id := stringValueAny(item["task_id"])
	if status == arkmodel.StatusSucceeded {
		if video := stringValueAny(item["video_url"]); video != "" {
			*videos = append(*videos, video)
		}
		return
	}
	if status == arkmodel.StatusFailed || status == arkmodel.StatusCancelled {
		*failed = append(*failed, item)
		return
	}
	if id != "" {
		*pending = append(*pending, id)
	}
}

func NormalizeSeedreamSize(size, model string) string {
	width, height := parseSizePair(size)
	if width == 0 || height == 0 {
		return "2048x2048"
	}
	minPixels := int64(1280 * 720)
	maxPixels := int64(4096 * 4096)
	modelText := strings.ToLower(model)
	if strings.Contains(modelText, "4-5") || strings.Contains(modelText, "4.5") {
		minPixels = 3686400
	}
	if strings.Contains(modelText, "5-0") || strings.Contains(modelText, "5.0") {
		minPixels = 3686400
		maxPixels = 10404496
	}
	ratio := float64(width) / float64(height)
	if ratio < 1.0/16.0 {
		ratio = 1.0 / 16.0
		height = int(float64(width) / ratio)
	} else if ratio > 16 {
		ratio = 16
		width = int(float64(height) * ratio)
	}
	pixels := int64(width * height)
	if pixels < minPixels {
		grow := sqrt(float64(minPixels) / float64(maxInt(1, width*height)))
		width = maxInt(16, int((float64(width)*grow+15)/16)*16)
		height = maxInt(16, int((float64(height)*grow+15)/16)*16)
	} else if pixels > maxPixels {
		shrink := sqrt(float64(maxPixels) / float64(maxInt(1, width*height)))
		width = maxInt(16, int((float64(width)*shrink)/16)*16)
		height = maxInt(16, int((float64(height)*shrink)/16)*16)
	}
	for int64(width*height) < minPixels {
		if width <= height {
			width += 16
		} else {
			height += 16
		}
	}
	for int64(width*height) > maxPixels && width > 16 && height > 16 {
		if width >= height {
			width -= 16
		} else {
			height -= 16
		}
	}
	return fmt.Sprintf("%dx%d", width, height)
}

func parseSizePair(size string) (int, int) {
	var width, height int
	if _, err := fmt.Sscanf(strings.ToLower(strings.ReplaceAll(size, "*", "x")), "%dx%d", &width, &height); err != nil {
		return 0, 0
	}
	return width, height
}

func structToMap(value any) map[string]any {
	data, _ := json.Marshal(value)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func stringPtr(value string) *string { return &value }

func stringValueAny(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func nonEmptyString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func firstNonEmptyVolc(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sqrt(value float64) float64 {
	z := value
	if z <= 0 {
		return 0
	}
	for i := 0; i < 20; i++ {
		z -= (z*z - value) / (2 * z)
	}
	return z
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
