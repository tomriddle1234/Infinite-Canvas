package upstream

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/app-go/internal/config"
)

// volcPresetMediaTOSHost 是火山方舟模板库/素材库媒体所在 TOS host（公共可读）。
const volcPresetMediaTOSHost = "ark-common-storage-prod-cn-beijing.tos-cn-beijing.volces.com"

// CachedPresetMedia 把火山模板库/素材库的 TOS 媒体文件下载到本地缓存并返回 /assets/... URL，
// 复用 CachedAssetPreview 的思路，但模板/素材没有 asset_id，所以按 sourceURL 的 sha256 做缓存键，
// 扩展名白名单包含视频/音频/图片。
func CachedPresetMedia(cfg *config.Config, sourceURL string) (string, error) {
	text := strings.TrimSpace(sourceURL)
	if text == "" {
		return "", errors.New("缺少媒体地址")
	}
	if !isHTTPURL(text) {
		return "", errors.New("仅支持 http/https 媒体地址")
	}
	// host 白名单校验，防止变成开放代理
	host := strings.ToLower(hostnameOf(text))
	if host != volcPresetMediaTOSHost && !strings.HasSuffix(host, ".tos-cn-beijing.volces.com") {
		return "", errors.New("仅支持火山 TOS 模板/素材媒体地址")
	}

	cacheDir := filepath.Join(cfg.AssetsDir, "cache", "volc_presets")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(text))
	key := hex.EncodeToString(sum[:])

	// 命中缓存：按 key 前缀扫描（扩展名未知）
	if cached := findCachedPresetMedia(cfg.AssetsDir, cacheDir, key); cached != "" {
		return cached, nil
	}

	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest(http.MethodGet, text, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		// 网络失败时回退已有缓存
		if cached := findCachedPresetMedia(cfg.AssetsDir, cacheDir, key); cached != "" {
			return cached, nil
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if cached := findCachedPresetMedia(cfg.AssetsDir, cacheDir, key); cached != "" {
			return cached, nil
		}
		return "", nil
	}
	// 视频可能较大，限制 256MB
	content, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024*1024))
	if err != nil || len(content) == 0 {
		if cached := findCachedPresetMedia(cfg.AssetsDir, cacheDir, key); cached != "" {
			return cached, nil
		}
		return "", err
	}

	path := presetCacheFile(cacheDir, key, resp.Header.Get("Content-Type"), text)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return cachedAssetURL(cfg.AssetsDir, path), nil
}

func findCachedPresetMedia(assetsDir, cacheDir, key string) string {
	prefix := key + "."
	entries, _ := os.ReadDir(cacheDir)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			return cachedAssetURL(assetsDir, filepath.Join(cacheDir, entry.Name()))
		}
	}
	return ""
}

func presetCacheFile(cacheDir, key, contentType, sourceURL string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	kind := ""
	if strings.Contains(ct, "video") {
		kind = "video"
	} else if strings.Contains(ct, "audio") {
		kind = "audio"
	} else if strings.Contains(ct, "image") {
		kind = "image"
	}
	// URL 扩展名（去掉 query/fragment）
	urlPath := sourceURL
	if i := strings.IndexAny(urlPath, "?#"); i >= 0 {
		urlPath = urlPath[:i]
	}
	urlExt := strings.ToLower(filepath.Ext(urlPath))
	// 无 content-type 时用 URL 扩展名推断 kind
	if kind == "" {
		switch urlExt {
		case ".mp4", ".mov", ".webm", ".m4v":
			kind = "video"
		case ".mp3", ".wav", ".m4a", ".aac", ".ogg":
			kind = "audio"
		default:
			kind = "image"
		}
	}
	// 按 kind 选扩展名，优先用与 kind 匹配的 URL 扩展名；否则用该 kind 的默认扩展名。
	// 注意：视频截帧 URL 形如 xxx.mp4?x-tos-process=video/snapshot,t_0 实际返回图片，
	// content-type=image/jpeg → kind=image → urlExt=.mp4 不在 image 白名单 → 兜底 .jpg，正确。
	ext := ""
	switch kind {
	case "image":
		switch urlExt {
		case ".jpg", ".jpeg", ".jpe", ".png", ".webp", ".gif":
			ext = ".jpg"
		}
		if ext == "" {
			ext = ".jpg"
		}
	case "video":
		switch urlExt {
		case ".mp4", ".mov", ".webm", ".m4v":
			ext = urlExt
		}
		if ext == "" {
			ext = ".mp4"
		}
	case "audio":
		switch urlExt {
		case ".mp3", ".wav", ".m4a", ".aac", ".ogg":
			ext = urlExt
		}
		if ext == "" {
			ext = ".mp3"
		}
	}
	return filepath.Join(cacheDir, key+ext)
}

func hostnameOf(raw string) string {
	// 轻量取 host，避免再依赖 net/url 解析（已在调用方校验 http(s)）
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return s
}
