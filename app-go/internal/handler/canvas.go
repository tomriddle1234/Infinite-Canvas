package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type CanvasCreateRequest struct {
	Title string `json:"title"`
	Icon  string `json:"icon"`
	Kind  string `json:"kind"`
}

type CanvasSaveRequest struct {
	Title         string           `json:"title"`
	Icon          string           `json:"icon"`
	Nodes         []map[string]any `json:"nodes"`
	Connections   []map[string]any `json:"connections"`
	Viewport      map[string]any   `json:"viewport"`
	Logs          []map[string]any `json:"logs"`
	Settings      map[string]any   `json:"settings"`
	BaseUpdatedAt *int64           `json:"base_updated_at"`
	ClientID      string           `json:"client_id"`
}

func (h *Handler) ListCanvases(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"canvases": h.store.ListCanvases(false)})
}

func (h *Handler) CreateCanvas(c *gin.Context) {
	var payload CanvasCreateRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	canvas, err := h.store.NewCanvas(payload.Title, payload.Icon, payload.Kind)
	if err != nil {
		writeStoreError(c, err, "画布不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"canvas": canvas})
}

func (h *Handler) ListTrashedCanvases(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"canvases":       h.store.ListCanvases(true),
		"retention_days": 30,
	})
}

func (h *Handler) GetCanvas(c *gin.Context) {
	canvas, err := h.store.LoadCanvas(c.Param("id"), false)
	if err != nil {
		writeStoreError(c, err, "画布不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"canvas": canvas})
}

func (h *Handler) UpdateCanvas(c *gin.Context) {
	var payload CanvasSaveRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}

	canvas, err := h.store.LoadCanvas(c.Param("id"), false)
	if err != nil {
		writeStoreError(c, err, "画布不存在")
		return
	}

	currentUpdatedAt := int64(numberValue(canvas["updated_at"]))
	if payload.BaseUpdatedAt != nil && *payload.BaseUpdatedAt > 0 && currentUpdatedAt > 0 && *payload.BaseUpdatedAt < currentUpdatedAt {
		c.JSON(http.StatusConflict, gin.H{"detail": gin.H{
			"message":    "画布已被其他页面更新，已拒绝旧版本覆盖。",
			"canvas":     canvas,
			"updated_at": currentUpdatedAt,
		}})
		return
	}

	canvas["title"] = trimRunes(firstNonEmpty(payload.Title, stringValue(canvas["title"], "未命名画布")), 80)
	canvas["icon"] = trimRunes(firstNonEmpty(payload.Icon, stringValue(canvas["icon"], "layers")), 32)
	canvas["nodes"] = payload.Nodes
	canvas["connections"] = payload.Connections
	canvas["viewport"] = payload.Viewport
	if canvas["viewport"] == nil {
		canvas["viewport"] = map[string]any{}
	}
	canvas["logs"] = lastLogs(payload.Logs, 500)
	canvas["settings"] = payload.Settings
	if canvas["settings"] == nil {
		canvas["settings"] = map[string]any{}
	}
	if stringValue(canvas["kind"], "") == "" {
		canvas["kind"] = "classic"
	}

	if err := h.store.SaveCanvas(canvas); err != nil {
		writeStoreError(c, err, "画布不存在")
		return
	}
	if h.rt != nil {
		h.rt.BroadcastCanvasUpdated(c.Param("id"), canvas["updated_at"], payload.ClientID)
	}
	c.JSON(http.StatusOK, gin.H{"canvas": canvas})
}

func (h *Handler) DeleteCanvas(c *gin.Context) {
	if err := h.store.SoftDeleteCanvas(c.Param("id")); err != nil {
		writeStoreError(c, err, "画布不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) RestoreCanvas(c *gin.Context) {
	canvas, err := h.store.RestoreCanvas(c.Param("id"))
	if err != nil {
		writeStoreError(c, err, "画布不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"canvas": canvas})
}

func (h *Handler) PurgeCanvas(c *gin.Context) {
	if err := h.store.PurgeCanvas(c.Param("id")); err != nil {
		writeStoreError(c, err, "画布不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) GetCanvasMeta(c *gin.Context) {
	canvas, err := h.store.LoadCanvas(c.Param("id"), false)
	if err != nil {
		writeStoreError(c, err, "画布不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":         canvas["id"],
		"updated_at": valueOr(canvas["updated_at"], 0),
		"title":      stringValue(canvas["title"], "未命名画布"),
		"icon":       stringValue(canvas["icon"], "layers"),
		"kind":       stringValue(canvas["kind"], "classic"),
	})
}

func valueOr(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}

func stringValue(value any, fallback string) string {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func trimRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		runes = runes[:max]
	}
	return string(runes)
}

func lastLogs(logs []map[string]any, max int) []map[string]any {
	if len(logs) <= max {
		if logs == nil {
			return []map[string]any{}
		}
		return logs
	}
	return logs[len(logs)-max:]
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case float32:
		return float64(typed)
	default:
		return 0
	}
}
