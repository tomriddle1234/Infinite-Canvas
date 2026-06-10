package handler

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/store"
	"infinite-canvas/app-go/internal/upstream"
)

type SeedreamRequest struct {
	Prompt          string        `json:"prompt"`
	Model           string        `json:"model"`
	Size            string        `json:"size"`
	Count           int           `json:"count"`
	Seed            *int64        `json:"seed"`
	Watermark       bool          `json:"watermark"`
	ReferenceImages []AIReference `json:"reference_images"`
}

type SeedanceRequest struct {
	RunID           string        `json:"run_id"`
	NodeID          string        `json:"node_id"`
	InputSignature  string        `json:"input_signature"`
	Prompt          string        `json:"prompt"`
	Model           string        `json:"model"`
	Duration        int64         `json:"duration"`
	AspectRatio     string        `json:"aspect_ratio"`
	Resolution      string        `json:"resolution"`
	Seed            *int64        `json:"seed"`
	GenerateAudio   bool          `json:"generate_audio"`
	ReturnLastFrame bool          `json:"return_last_frame"`
	ReferenceImages []AIReference `json:"reference_images"`
	ReferenceVideos []AIReference `json:"reference_videos"`
	ReferenceAudios []AIReference `json:"reference_audios"`
}

type SeedanceStatusRequest struct {
	TaskIDs []string `json:"task_ids"`
	RunID   string   `json:"run_id"`
}

type SeedanceTaskListRequest struct {
	RunID          string   `json:"run_id"`
	Model          string   `json:"model"`
	Status         string   `json:"status"`
	PageSize       int      `json:"page_size"`
	SubmittedAfter *float64 `json:"submitted_after"`
	KnownTaskIDs   []string `json:"known_task_ids"`
}

type SeedanceClaimRequest struct {
	RunID          string `json:"run_id"`
	NodeID         string `json:"node_id"`
	TaskID         string `json:"task_id"`
	Model          string `json:"model"`
	InputSignature string `json:"input_signature"`
}

func (h *Handler) NodeSeedream(c *gin.Context) {
	var payload SeedreamRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if strings.TrimSpace(payload.Prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "prompt 不能为空"})
		return
	}
	count := payload.Count
	if count < 1 {
		count = 1
	}
	if count > 8 {
		count = 8
	}
	client := upstream.NewVolcengineClient(h.cfg, h.store)
	images := make([]string, 0, count)
	raws := make([]map[string]any, 0, count)
	modelName := payload.Model
	for index := 0; index < count; index++ {
		seed := payload.Seed
		if seed != nil {
			value := *seed + int64(index)
			seed = &value
		}
		items, raw, resolvedModel, err := client.GenerateSeedream(c.Request.Context(), upstream.SeedreamInput{
			Prompt:          payload.Prompt,
			Model:           payload.Model,
			Size:            payload.Size,
			Seed:            seed,
			Watermark:       payload.Watermark,
			ReferenceImages: referencesToMaps(payload.ReferenceImages),
		})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"detail": "Seedream 调用失败：" + err.Error()})
			return
		}
		modelName = resolvedModel
		raws = append(raws, raw)
		for _, item := range items {
			url, err := upstream.NewClient(h.cfg, h.store).SaveImageItem(item, "seedream_")
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"detail": "保存 Seedream 图片失败：" + err.Error()})
				return
			}
			images = append(images, url)
		}
	}
	raw := any(gin.H{"items": raws})
	if len(raws) == 1 {
		raw = raws[0]
	}
	result := map[string]any{
		"prompt":      payload.Prompt,
		"images":      images,
		"timestamp":   nowSeconds(),
		"type":        "seedream",
		"model":       modelName,
		"provider_id": "volcengine-ark",
		"params": gin.H{
			"size": payload.Size, "count": count, "seed": payload.Seed, "watermark": payload.Watermark,
		},
		"raw": raw,
	}
	_ = h.store.SaveHistory(result)
	h.rt.BroadcastNewImage(result)
	c.JSON(http.StatusOK, result)
}

func (h *Handler) NodeSeedance(c *gin.Context) {
	var payload SeedanceRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if strings.TrimSpace(payload.Prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "prompt 不能为空"})
		return
	}
	runID := strings.TrimSpace(payload.RunID)
	if runID != "" {
		existing := h.store.GetSeedanceTask(runID)
		if len(stringSliceAny(existing["task_ids"])) > 0 {
			oldSig := strings.TrimSpace(stringValue(existing["input_signature"], ""))
			newSig := strings.TrimSpace(payload.InputSignature)
			if oldSig != "" && newSig != "" && oldSig != newSig {
				c.JSON(http.StatusConflict, gin.H{"detail": "Seedance run_id 已对应另一组参数，请复制节点或重新提交新任务。"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"status":       "submitted",
				"task_ids":     stringSliceAny(existing["task_ids"]),
				"model":        firstNonEmptyString(stringValue(existing["model"], ""), payload.Model),
				"raw":          firstMapAny(existing["raw"]),
				"submitted_at": firstNonZero(numberValue(existing["submitted_at"]), numberValue(existing["created_at"]), nowSeconds()),
				"run_id":       runID,
				"reused":       true,
			})
			return
		}
		_, _ = h.store.SaveSeedanceTask(map[string]any{
			"run_id":          runID,
			"node_id":         payload.NodeID,
			"input_signature": payload.InputSignature,
			"status":          "submitting",
			"model":           payload.Model,
			"submitted_at":    nowSeconds(),
			"prompt":          truncateRunes(payload.Prompt, 1000),
			"params": gin.H{
				"duration": payload.Duration, "aspect_ratio": payload.AspectRatio, "resolution": payload.Resolution,
				"seed": payload.Seed, "generate_audio": payload.GenerateAudio, "return_last_frame": payload.ReturnLastFrame,
				"reference_images": len(payload.ReferenceImages), "reference_videos": len(payload.ReferenceVideos), "reference_audios": len(payload.ReferenceAudios),
			},
		})
	}
	client := upstream.NewVolcengineClient(h.cfg, h.store)
	submitted, err := client.SubmitSeedance(c.Request.Context(), upstream.SeedanceInput{
		Prompt:          payload.Prompt,
		Model:           payload.Model,
		Duration:        payload.Duration,
		AspectRatio:     payload.AspectRatio,
		Resolution:      payload.Resolution,
		Seed:            payload.Seed,
		GenerateAudio:   payload.GenerateAudio,
		ReturnLastFrame: payload.ReturnLastFrame,
		ReferenceImages: referencesToMaps(payload.ReferenceImages),
		ReferenceVideos: referencesToMaps(payload.ReferenceVideos),
		ReferenceAudios: referencesToMaps(payload.ReferenceAudios),
	})
	if err != nil {
		if runID != "" {
			_, _ = h.store.UpdateSeedanceTask(runID, map[string]any{"status": "submit_failed", "error": err.Error()})
		}
		c.JSON(http.StatusBadGateway, gin.H{"detail": "Seedance 提交失败：" + err.Error()})
		return
	}
	if runID != "" {
		_, _ = h.store.SaveSeedanceTask(map[string]any{
			"run_id": runID, "node_id": payload.NodeID, "input_signature": payload.InputSignature,
			"status": "submitted", "task_ids": submitted.TaskIDs, "model": submitted.Model,
			"raw": submitted.Raw, "submitted_at": submitted.SubmittedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "submitted", "task_ids": submitted.TaskIDs, "model": submitted.Model, "raw": submitted.Raw, "submitted_at": submitted.SubmittedAt, "run_id": runID, "reused": false})
}

func (h *Handler) NodeSeedanceStatus(c *gin.Context) {
	var payload SeedanceStatusRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	taskIDs := cleanStringList(payload.TaskIDs)
	if len(taskIDs) == 0 && strings.TrimSpace(payload.RunID) != "" {
		taskIDs = stringSliceAny(h.store.GetSeedanceTask(payload.RunID)["task_ids"])
	}
	if len(taskIDs) == 0 {
		if strings.TrimSpace(payload.RunID) != "" {
			c.JSON(http.StatusOK, gin.H{"status": "missing", "task_ids": []string{}, "tasks": []any{}, "missing": []gin.H{{"run_id": payload.RunID, "error": "本地未记录 Seedance task_id"}}, "videos": []string{}})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": "缺少 Seedance task_ids"})
		return
	}
	client := upstream.NewVolcengineClient(h.cfg, h.store)
	result, err := client.PollSeedance(c.Request.Context(), taskIDs)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": "Seedance 查询失败：" + err.Error()})
		return
	}
	localVideos := []string{}
	if stringValue(result["status"], "") == "succeeded" {
		for _, url := range stringSliceAny(result["videos"]) {
			localVideos = append(localVideos, h.saveRemoteVideo(url, "seedance_"))
		}
		record := map[string]any{"prompt": "", "videos": localVideos, "outputs": localVideos, "timestamp": nowSeconds(), "type": "seedance", "task_ids": taskIDs, "raw": result}
		_ = h.store.SaveHistory(record)
	}
	if len(localVideos) > 0 {
		result["videos"] = localVideos
	}
	result["task_ids"] = taskIDs
	if strings.TrimSpace(payload.RunID) != "" {
		_, _ = h.store.UpdateSeedanceTask(payload.RunID, map[string]any{"status": result["status"], "task_ids": taskIDs, "videos": result["videos"], "raw_status": result, "last_checked_at": nowSeconds()})
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) NodeSeedanceTasks(c *gin.Context) {
	var payload SeedanceTaskListRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	pageSize := payload.PageSize
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	result, err := upstream.NewVolcengineClient(h.cfg, h.store).ListSeedanceTasks(c.Request.Context(), payload.Model, payload.Status, pageSize)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": "Seedance 任务列表失败：" + err.Error()})
		return
	}
	claimed := h.store.SeedanceClaimedTaskIDs()
	for _, id := range payload.KnownTaskIDs {
		claimed[id] = true
	}
	after := 0.0
	if payload.SubmittedAfter != nil {
		after = *payload.SubmittedAfter
	}
	filtered := []map[string]any{}
	for _, item := range mapsFromAny(result["tasks"]) {
		id := stringValue(item["task_id"], "")
		if id == "" || claimed[id] {
			continue
		}
		if after > 0 && numberValue(item["created_at"]) > 0 && numberValue(item["created_at"]) < after {
			continue
		}
		filtered = append(filtered, item)
	}
	c.JSON(http.StatusOK, gin.H{"tasks": filtered, "total": result["total"]})
}

func (h *Handler) NodeSeedanceClaim(c *gin.Context) {
	var payload SeedanceClaimRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	runID := strings.TrimSpace(payload.RunID)
	taskID := strings.TrimSpace(payload.TaskID)
	if runID == "" || taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "缺少 Seedance run_id 或 task_id"})
		return
	}
	if owner := h.store.FindSeedanceRunByTaskID(taskID); owner != nil && stringValue(owner["run_id"], "") != runID {
		c.JSON(http.StatusConflict, gin.H{"detail": "这个 Seedance task_id 已经绑定到其它任务。"})
		return
	}
	existing := h.store.GetSeedanceTask(runID)
	if ids := stringSliceAny(existing["task_ids"]); len(ids) > 0 && !stringInSlice(taskID, ids) {
		c.JSON(http.StatusConflict, gin.H{"detail": "这个 Seedance run 已经绑定了其它 task_id。"})
		return
	}
	record, err := h.store.SaveSeedanceTask(map[string]any{
		"run_id": runID, "node_id": payload.NodeID, "input_signature": firstNonEmptyString(payload.InputSignature, stringValue(existing["input_signature"], "")),
		"status": "submitted", "task_ids": []string{taskID}, "model": firstNonEmptyString(payload.Model, stringValue(existing["model"], "")), "claimed_at": nowSeconds(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "claimed", "run_id": runID, "task_ids": stringSliceAny(record["task_ids"]), "record": record})
}

func (h *Handler) saveRemoteVideo(url, prefix string) string {
	if strings.TrimSpace(url) == "" {
		return url
	}
	client := &http.Client{Timeout: time.Duration(maxIntLocal(h.cfg.ImageTaskTimeoutSec, 120)) * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return url
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return url
	}
	ext := ".mp4"
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type"))) {
	case "video/webm":
		ext = ".webm"
	case "video/quicktime":
		ext = ".mov"
	}
	filename := prefix + store.NewHexID()[:10] + ext
	path := h.store.OutputPathFor(filename, "output")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return url
	}
	out, err := os.Create(path)
	if err != nil {
		return url
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 1024*1024*1024)); err != nil {
		return url
	}
	return h.store.OutputURLFor(filename, "output")
}

func cleanStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func stringSliceAny(value any) []string {
	switch list := value.(type) {
	case []string:
		return cleanStringList(list)
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if text := strings.TrimSpace(stringValue(item, "")); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func mapsFromAny(value any) []map[string]any {
	switch list := value.(type) {
	case []map[string]any:
		return list
	case []any:
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if obj, ok := item.(map[string]any); ok {
				out = append(out, obj)
			}
		}
		return out
	default:
		return nil
	}
}

func firstMapAny(value any) map[string]any {
	if obj, ok := value.(map[string]any); ok {
		return obj
	}
	return map[string]any{}
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func truncateRunes(value string, maxLen int) string {
	runes := []rune(value)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return value
}

func stringInSlice(needle string, values []string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
