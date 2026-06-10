package handler

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/config"
	"infinite-canvas/app-go/internal/realtime"
	"infinite-canvas/app-go/internal/store"
)

type Handler struct {
	cfg              *config.Config
	store            *store.Store
	rt               *realtime.Manager
	tasksMu          sync.Mutex
	canvasImageTasks map[string]map[string]any
	comfyMu          sync.Mutex
	comfyNextTaskID  int
	comfyQueue       []map[string]string
	comfyBackendLoad map[string]int
}

func New(cfg *config.Config, stores *store.Store, rt *realtime.Manager) *Handler {
	load := map[string]int{}
	for _, instance := range cfg.ComfyUIInstances {
		load[instance] = 0
	}
	return &Handler{cfg: cfg, store: stores, rt: rt, canvasImageTasks: map[string]map[string]any{}, comfyNextTaskID: 1, comfyQueue: []map[string]string{}, comfyBackendLoad: load}
}

func writeStoreError(c *gin.Context, err error, notFoundDetail string) {
	switch {
	case errors.Is(err, store.ErrBadID):
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的 ID"})
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"detail": notFoundDetail})
	case errors.Is(err, store.ErrCanvasDeleted):
		c.JSON(http.StatusNotFound, gin.H{"detail": "画布已在回收站"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
	}
}

func pathParamWithoutSlash(c *gin.Context, name string) string {
	return strings.TrimPrefix(c.Param(name), "/")
}
