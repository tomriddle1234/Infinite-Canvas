package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/upstream"
)

type AssetGroupsRequest struct {
	GroupType string `json:"group_type"`
	Query     string `json:"query"`
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
}

type AssetsListRequest struct {
	GroupType string   `json:"group_type"`
	GroupID   string   `json:"group_id"`
	Query     string   `json:"query"`
	Statuses  []string `json:"statuses"`
	Page      int      `json:"page"`
	PageSize  int      `json:"page_size"`
}

type AssetDetailRequest struct {
	AssetID string `json:"asset_id"`
}

type PresetPortraitSearchRequest struct {
	Query    string `json:"query"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (h *Handler) VolcengineAssetGroups(c *gin.Context) {
	var payload AssetGroupsRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	result, err := upstream.NewVolcengineAssetClient(h.cfg).ListAssetGroups(firstNonEmptyString(payload.GroupType, "LivenessFace"), payload.Query, payload.Page, payload.PageSize)
	writeVolcengineAssetResult(c, result, err)
}

func (h *Handler) VolcengineAssetsList(c *gin.Context) {
	var payload AssetsListRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	statuses := payload.Statuses
	if len(statuses) == 0 {
		statuses = []string{"Active"}
	}
	result, err := upstream.NewVolcengineAssetClient(h.cfg).ListAssets(firstNonEmptyString(payload.GroupType, "LivenessFace"), payload.GroupID, payload.Query, statuses, payload.Page, payload.PageSize)
	writeVolcengineAssetResult(c, result, err)
}

func (h *Handler) VolcengineAssetDetail(c *gin.Context) {
	var payload AssetDetailRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	result, err := upstream.NewVolcengineAssetClient(h.cfg).GetAsset(payload.AssetID)
	writeVolcengineAssetResult(c, result, err)
}

func (h *Handler) VolcenginePresetPortraitsSearch(c *gin.Context) {
	var payload PresetPortraitSearchRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	result, err := upstream.NewVolcengineAssetClient(h.cfg).SearchPresetVirtualPortraits(payload.Query, payload.Page, payload.PageSize)
	writeVolcengineAssetResult(c, result, err)
}

func (h *Handler) VolcengineAssetPreview(c *gin.Context) {
	assetID := strings.TrimSpace(c.Param("asset_id"))
	url, err := upstream.NewVolcengineAssetClient(h.cfg).CachedAssetPreview(assetID, c.Query("refresh") == "true" || c.Query("refresh") == "1")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	if url == "" {
		c.JSON(http.StatusNotFound, gin.H{"detail": "素材预览图暂不可用"})
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func writeVolcengineAssetResult(c *gin.Context, result map[string]any, err error) {
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "未配置") || strings.Contains(err.Error(), "不支持") || strings.Contains(err.Error(), "缺少") {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
