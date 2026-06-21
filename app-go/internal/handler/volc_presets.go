package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/upstream"
)

// VolcPresetMedia 代理火山模板库/素材库的 TOS 媒体：下载并本地缓存，302 重定向到 /assets/...
func (h *Handler) VolcPresetMedia(c *gin.Context) {
	raw := strings.TrimSpace(c.Query("url"))
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "缺少 url 参数"})
		return
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "非法媒体地址"})
		return
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "ark-common-storage-prod-cn-beijing.tos-cn-beijing.volces.com" && !strings.HasSuffix(host, ".tos-cn-beijing.volces.com") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "仅支持火山 TOS 模板/素材媒体地址"})
		return
	}
	out, err := upstream.CachedPresetMedia(h.cfg, raw)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		return
	}
	if out == "" {
		c.JSON(http.StatusNotFound, gin.H{"detail": "媒体暂不可用"})
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, out)
}
