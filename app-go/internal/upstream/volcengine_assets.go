package upstream

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"infinite-canvas/app-go/internal/config"
)

const volcengineAssetVersion = "2024-01-01"

var safeAssetCacheName = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
var assetIDPattern = regexp.MustCompile(`^asset-[a-zA-Z0-9_-]+$`)

var presetVirtualFilterCache = struct {
	sync.Mutex
	items map[string]presetVirtualFilterCacheEntry
}{items: map[string]presetVirtualFilterCacheEntry{}}

type presetVirtualFilterCacheEntry struct {
	created time.Time
	data    map[string]any
}

type VolcengineAssetClient struct {
	cfg  *config.Config
	http *http.Client
}

type PresetPortraitFilters struct {
	Gender      string
	Country     string
	Occupation  string
	Temperament string
}

func NewVolcengineAssetClient(cfg *config.Config) *VolcengineAssetClient {
	return &VolcengineAssetClient{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *VolcengineAssetClient) ListAssetGroups(groupType, query string, page, pageSize int) (map[string]any, error) {
	cleanGroupType, err := normalizeAssetGroupType(groupType)
	if err != nil {
		return nil, err
	}
	filter := map[string]any{"GroupType": cleanGroupType}
	if text := strings.TrimSpace(query); text != "" {
		filter["Name"] = truncateString(text, 64)
	}
	body := map[string]any{"Filter": filter, "PageNumber": pageClamp(page, 1, 100), "PageSize": pageClamp(pageSize, 20, 100), "ProjectName": c.projectName("")}
	raw, err := c.assetCall("ListAssetGroups", body)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, item := range anyListFromMap(raw, "Items") {
		if obj, ok := item.(map[string]any); ok {
			items = append(items, normalizeAssetGroup(obj))
		}
	}
	return map[string]any{"items": items, "total": intValue(raw["TotalCount"], len(items)), "page": intValue(raw["PageNumber"], body["PageNumber"]), "page_size": intValue(raw["PageSize"], body["PageSize"])}, nil
}

func (c *VolcengineAssetClient) ListAssets(groupType, groupID, query string, statuses []string, page, pageSize int) (map[string]any, error) {
	cleanGroupType, err := normalizeAssetGroupType(groupType)
	if err != nil {
		return nil, err
	}
	filter := map[string]any{"GroupType": cleanGroupType}
	if text := strings.TrimSpace(groupID); text != "" {
		filter["GroupIds"] = []string{text}
	}
	if text := strings.TrimSpace(query); text != "" {
		filter["Name"] = truncateString(text, 64)
	}
	cleanStatuses, err := normalizeAssetStatuses(statuses)
	if err != nil {
		return nil, err
	}
	if len(cleanStatuses) > 0 {
		filter["Statuses"] = cleanStatuses
	}
	body := map[string]any{"Filter": filter, "PageNumber": pageClamp(page, 1, 100), "PageSize": pageClamp(pageSize, 24, 100), "ProjectName": c.projectName("")}
	raw, err := c.assetCall("ListAssets", body)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, item := range anyListFromMap(raw, "Items") {
		if obj, ok := item.(map[string]any); ok {
			items = append(items, normalizeAsset(obj))
		}
	}
	return map[string]any{"items": items, "total": intValue(raw["TotalCount"], len(items)), "page": intValue(raw["PageNumber"], body["PageNumber"]), "page_size": intValue(raw["PageSize"], body["PageSize"])}, nil
}

func (c *VolcengineAssetClient) GetAsset(assetID string) (map[string]any, error) {
	text := strings.TrimSpace(strings.TrimPrefix(assetID, "asset://"))
	if text == "" {
		return nil, errors.New("缺少素材 Asset ID")
	}
	raw, err := c.assetCall("GetAsset", map[string]any{"Id": text, "ProjectName": c.projectName("")})
	if err != nil {
		return nil, err
	}
	return normalizeAsset(raw), nil
}

func (c *VolcengineAssetClient) SearchPresetVirtualPortraits(query string, filters PresetPortraitFilters, page, pageSize int) (map[string]any, error) {
	if hasPresetPortraitFilters(filters) {
		return c.searchPresetVirtualPortraitsLocalFiltered(query, filters, page, pageSize)
	}
	apiFilters := []map[string]any{{"Field": "metadata.type", "Op": "must", "Conds": map[string]any{"StrValues": []string{"portrait"}}}}
	body := map[string]any{
		"Query":       map[string]any{},
		"Filters":     apiFilters,
		"PageNum":     pageClamp(page, 1, 100),
		"PageSize":    pageClamp(pageSize, 24, 100),
		"ProjectName": c.projectName(""),
	}
	if text := strings.TrimSpace(query); text != "" {
		body["Query"] = map[string]any{"Text": text}
	} else {
		body["SortBy"] = "score"
		body["SortOrder"] = "desc"
	}
	raw, err := c.assetCall("ListMediaAssetGroup", body)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, item := range anyListFromMap(raw, "Items") {
		if obj, ok := item.(map[string]any); ok {
			normalized := normalizePresetVirtualAsset(obj)
			// 上游对部分素材不暴露可用的 asset_id；这些素材既不能被 Seedance 通过
			// asset:// 引用，也会让前端选中态产生 dataset="" 碰撞（多卡同时高亮、无法取消）。
			if firstStringFromMap(normalized, "asset_id") == "" {
				continue
			}
			items = append(items, normalized)
		}
	}
	return map[string]any{"items": items, "total": intValue(firstValue(raw["Total"], raw["TotalCount"]), len(items)), "page": intValue(firstValue(raw["PageNum"], raw["PageNumber"]), body["PageNum"]), "page_size": intValue(raw["PageSize"], body["PageSize"]), "source": "volcengine_list_media_asset_group"}, nil
}

func (c *VolcengineAssetClient) searchPresetVirtualPortraitsLocalFiltered(query string, filters PresetPortraitFilters, page, pageSize int) (map[string]any, error) {
	cleanPage := pageClamp(page, 1, 100)
	cleanPageSize := pageClamp(pageSize, 24, 100)
	offset := (cleanPage - 1) * cleanPageSize
	limit := offset + cleanPageSize
	matched := 0
	out := []map[string]any{}
	sourceTotal := 0
	for scanPage := 1; scanPage <= 100; scanPage++ {
		result, err := c.SearchPresetVirtualPortraits(query, PresetPortraitFilters{}, scanPage, 100)
		if err != nil {
			return nil, err
		}
		sourceTotal = intValue(result["total"], sourceTotal)
		items := mapListValue(result["items"])
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			if !matchesPresetPortraitFilters(item, filters) {
				continue
			}
			if matched >= offset && matched < limit {
				out = append(out, item)
			}
			matched++
		}
		if len(items) < 100 || (sourceTotal > 0 && scanPage*100 >= sourceTotal) {
			break
		}
	}
	return map[string]any{"items": out, "total": matched, "page": cleanPage, "page_size": cleanPageSize, "source": "volcengine_list_media_asset_group_local_filter"}, nil
}

func (c *VolcengineAssetClient) PresetVirtualPortraitFilters(query string) (map[string]any, error) {
	cacheKey := c.projectName("") + "|" + strings.TrimSpace(query)
	presetVirtualFilterCache.Lock()
	if entry, ok := presetVirtualFilterCache.items[cacheKey]; ok && time.Since(entry.created) < 30*time.Minute {
		presetVirtualFilterCache.Unlock()
		return entry.data, nil
	}
	presetVirtualFilterCache.Unlock()

	const pageSize = 100
	sets := map[string]map[string]int{
		"gender":      {},
		"country":     {},
		"occupation":  {},
		"temperament": {},
	}
	total := 0
	scanned := 0
	for page := 1; page <= 100; page++ {
		result, err := c.SearchPresetVirtualPortraits(query, PresetPortraitFilters{}, page, pageSize)
		if err != nil {
			return nil, err
		}
		items, _ := result["items"].([]map[string]any)
		if items == nil {
			if rawItems, ok := result["items"].([]any); ok {
				items = make([]map[string]any, 0, len(rawItems))
				for _, item := range rawItems {
					if obj, ok := item.(map[string]any); ok {
						items = append(items, obj)
					}
				}
			}
		}
		total = intValue(result["total"], total)
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			metadata, _ := item["metadata"].(map[string]any)
			addFilterValue(sets["gender"], metadataText(metadata, "Gender"))
			addFilterValue(sets["country"], metadataText(metadata, "Country"))
			addFilterValue(sets["occupation"], metadataText(metadata, "Occupation"))
			addFilterValue(sets["temperament"], metadataText(metadata, "Temperament"))
		}
		scanned += len(items)
		if len(items) < pageSize || (total > 0 && scanned >= total) {
			break
		}
	}
	result := map[string]any{
		"gender":      sortedFilterOptions(sets["gender"]),
		"country":     sortedFilterOptions(sets["country"]),
		"occupation":  sortedFilterOptions(sets["occupation"]),
		"temperament": sortedFilterOptions(sets["temperament"]),
		"total":       total,
		"scanned":     scanned,
	}
	presetVirtualFilterCache.Lock()
	presetVirtualFilterCache.items[cacheKey] = presetVirtualFilterCacheEntry{created: time.Now(), data: result}
	presetVirtualFilterCache.Unlock()
	return result, nil
}

func (c *VolcengineAssetClient) CachedAssetPreview(assetID string, refresh bool) (string, error) {
	text := strings.TrimSpace(strings.TrimPrefix(assetID, "asset://"))
	if text == "" {
		return "", nil
	}
	cacheDir := filepath.Join(c.cfg.AssetsDir, "cache", "volcengine_assets")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	if !refresh {
		if cached := findCachedAssetPreview(c.cfg.AssetsDir, cacheDir, text); cached != "" {
			return cached, nil
		}
	}
	info, err := c.GetAsset(text)
	if err != nil {
		if cached := findCachedAssetPreview(c.cfg.AssetsDir, cacheDir, text); cached != "" {
			return cached, nil
		}
		return "", err
	}
	sourceURL := stringFromMap(info, "url")
	if sourceURL == "" || !isHTTPURL(sourceURL) {
		return "", nil
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Get(sourceURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", nil
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
	if err != nil || len(content) == 0 {
		return "", err
	}
	path := cacheFileForAsset(cacheDir, text, resp.Header.Get("Content-Type"), sourceURL)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return cachedAssetURL(c.cfg.AssetsDir, path), nil
}

func (c *VolcengineAssetClient) assetCall(action string, body map[string]any) (map[string]any, error) {
	ak := strings.TrimSpace(c.cfg.VolcengineAccessKeyID)
	sk := strings.TrimSpace(c.cfg.VolcengineSecretAccessKey)
	if ak == "" || sk == "" {
		return nil, errors.New("未配置火山引擎 AK/SK，请先在 API 设置中填写 Access Key ID / Secret Access Key。")
	}
	bodyData, _ := json.Marshal(body)
	bodyStr := string(bodyData)
	region := c.region("")
	host := "ark." + region + ".volcengineapi.com"
	headers := signVolcengineAssetHeaders(ak, sk, action, bodyStr, region, host)
	endpoint := "https://" + host + "/?Action=" + url.QueryEscape(action) + "&Version=" + url.QueryEscape(volcengineAssetVersion)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(bodyData))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求火山 %s 失败：%w", action, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("火山 %s 返回非 JSON（%d）：%s", action, resp.StatusCode, truncateString(string(data), 300))
	}
	if meta, ok := payload["ResponseMetadata"].(map[string]any); ok {
		if errObj, ok := meta["Error"].(map[string]any); ok {
			return nil, fmt.Errorf("火山 %s 失败：%s %s", action, stringFromMap(errObj, "Code"), stringFromMap(errObj, "Message"))
		}
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("火山 %s 失败（%d）：%s", action, resp.StatusCode, truncateString(string(data), 300))
	}
	if result, ok := payload["Result"].(map[string]any); ok {
		return result, nil
	}
	return payload, nil
}

func signVolcengineAssetHeaders(ak, sk, action, bodyStr, region, host string) map[string]string {
	now := time.Now().UTC()
	xDate := now.Format("20060102T150405Z")
	shortDate := xDate[:8]
	payloadHash := sha256Hex([]byte(bodyStr))
	canonicalQuery := "Action=" + url.QueryEscape(action) + "&Version=" + url.QueryEscape(volcengineAssetVersion)
	canonicalHeaders := "content-type:application/json\nhost:" + host + "\nx-content-sha256:" + payloadHash + "\nx-date:" + xDate + "\n"
	signedHeaders := "content-type;host;x-content-sha256;x-date"
	canonicalRequest := strings.Join([]string{http.MethodPost, "/", canonicalQuery, canonicalHeaders, signedHeaders, payloadHash}, "\n")
	scope := shortDate + "/" + region + "/ark/request"
	stringToSign := strings.Join([]string{"HMAC-SHA256", xDate, scope, sha256Hex([]byte(canonicalRequest))}, "\n")
	kDate := volcHMAC([]byte(sk), shortDate)
	kRegion := volcHMAC(kDate, region)
	kService := volcHMAC(kRegion, "ark")
	kSigning := volcHMAC(kService, "request")
	signature := hex.EncodeToString(volcHMAC(kSigning, stringToSign))
	return map[string]string{
		"Content-Type":     "application/json",
		"Host":             host,
		"X-Date":           xDate,
		"X-Content-Sha256": payloadHash,
		"Authorization":    "HMAC-SHA256 Credential=" + ak + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature,
	}
}

func normalizeAssetGroupType(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "LivenessFace", nil
	}
	if value != "LivenessFace" && value != "AIGC" {
		return "", fmt.Errorf("不支持的人像库类型：%s。支持 LivenessFace 真人认证人像和 AIGC 虚拟人像。", value)
	}
	return value, nil
}

func normalizeAssetStatuses(values []string) ([]string, error) {
	allowed := map[string]bool{"Active": true, "Processing": true, "Failed": true}
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		if !allowed[text] {
			return nil, fmt.Errorf("不支持的素材状态：%s", text)
		}
		if !seen[text] {
			out = append(out, text)
			seen[text] = true
		}
	}
	return out, nil
}

func normalizeAssetGroup(item map[string]any) map[string]any {
	id := firstStringFromMap(item, "Id", "GroupId")
	return map[string]any{"id": id, "name": firstNonEmptyVolc(firstStringFromMap(item, "Name", "Title"), id, "未命名素材组"), "title": stringFromMap(item, "Title"), "description": stringFromMap(item, "Description"), "group_type": stringFromMap(item, "GroupType"), "project_name": stringFromMap(item, "ProjectName"), "create_time": stringFromMap(item, "CreateTime"), "update_time": stringFromMap(item, "UpdateTime")}
}

func normalizeAsset(item map[string]any) map[string]any {
	id := firstStringFromMap(item, "Id", "AssetId")
	name := firstNonEmptyVolc(firstStringFromMap(item, "Name", "Title"), id, "未命名素材")
	mediaURL := normalizePreviewURL(firstStringFromMap(item, "URL", "Url", "url", "PreviewURL", "CoverURL"))
	previewURL := mediaURL
	if previewURL == "" && id != "" {
		previewURL = "/api/volcengine/assets/preview/" + url.PathEscape(id)
	}
	proxyURL := ""
	if id != "" {
		proxyURL = "/api/volcengine/assets/preview/" + url.PathEscape(id)
	}
	return map[string]any{"id": id, "asset_id": id, "asset_uri": assetURI(id), "name": name, "title": stringFromMap(item, "Title"), "description": stringFromMap(item, "Description"), "asset_type": stringFromMap(item, "AssetType"), "group_id": stringFromMap(item, "GroupId"), "group_type": stringFromMap(item, "GroupType"), "status": stringFromMap(item, "Status"), "project_name": stringFromMap(item, "ProjectName"), "create_time": stringFromMap(item, "CreateTime"), "update_time": stringFromMap(item, "UpdateTime"), "url": mediaURL, "preview_url": previewURL, "preview_proxy_url": firstNonEmptyVolc(proxyURL, previewURL)}
}

func normalizePresetVirtualAsset(item map[string]any) map[string]any {
	group, _ := item["AssetGroup"].(map[string]any)
	if group == nil {
		group = item
	}
	content, _ := group["Content"].(map[string]any)
	assetType := "Image"
	media := map[string]any{}
	if images := firstAnyListFromMap(content, "Image", "Images", "image", "images"); len(images) > 0 {
		media, _ = images[0].(map[string]any)
	} else if videos := firstAnyListFromMap(content, "Video", "Videos", "video", "videos"); len(videos) > 0 {
		assetType = "Video"
		media, _ = videos[0].(map[string]any)
	}
	id := firstAssetID(
		firstStringFromMap(media, "AssetID", "AssetId", "asset_id", "Id", "id"),
		firstStringFromMap(media, "AssetURI", "AssetUri", "asset_uri"),
		firstStringFromMap(group, "AssetID", "AssetId", "asset_id", "Id", "id"),
		firstStringFromMap(group, "AssetURI", "AssetUri", "asset_uri"),
		firstStringFromMap(item, "asset_id", "AssetID", "AssetId", "id", "Id", "SID"),
		firstStringFromMap(item, "AssetURI", "AssetUri", "asset_uri"),
		firstRecursiveStringByKeys(item, "AssetID", "AssetId", "asset_id", "AssetURI", "AssetUri", "asset_uri"),
	)
	name := firstNonEmptyVolc(firstStringFromMap(group, "Name", "Title"), firstStringFromMap(item, "Name", "Title"), id, "Preset virtual portrait")
	preview := firstUsablePreviewURL(
		firstStringFromMap(item, "preview_url", "PreviewURL", "CoverURL", "ThumbnailURL", "PosterURL", "url", "URL"),
		firstStringFromMap(group, "preview_url", "PreviewURL", "CoverURL", "ThumbnailURL", "PosterURL", "url", "URL"),
		firstStringFromMap(media, "preview_url", "PreviewURL", "CoverURL", "ThumbnailURL", "PosterURL", "ImageURL", "url", "URL"),
		firstRecursiveStringByKeys(item, "preview_url", "PreviewURL", "CoverURL", "ThumbnailURL", "PosterURL", "ImageURL", "url", "URL"),
	)
	proxyURL := ""
	if id != "" {
		proxyURL = "/api/volcengine/assets/preview/" + url.PathEscape(id)
	}
	return map[string]any{"id": id, "asset_id": id, "asset_uri": firstNonEmptyVolc(firstStringFromMap(media, "AssetURI"), firstStringFromMap(group, "AssetURI"), firstStringFromMap(item, "AssetURI"), assetURI(id)), "name": name, "title": firstStringFromMap(group, "Title"), "description": firstStringFromMap(group, "Description"), "tags": normalizeTags(firstValue(group["Tags"], item["Tags"], item["tags"])), "metadata": firstValue(group["Metadata"], map[string]any{}), "source": "volcengine_preset_virtual", "asset_type": assetType, "score": item["Score"], "preview_url": firstNonEmptyVolc(preview, proxyURL), "preview_proxy_url": firstNonEmptyVolc(proxyURL, preview)}
}

func (c *VolcengineAssetClient) projectName(value string) string {
	return firstNonEmptyVolc(strings.TrimSpace(value), c.cfg.VolcengineProjectName, "default")
}
func (c *VolcengineAssetClient) region(value string) string {
	return firstNonEmptyVolc(strings.TrimSpace(value), c.cfg.VolcengineRegion, "cn-beijing")
}

func assetURI(id string) string {
	if id == "" || strings.HasPrefix(id, "asset://") {
		return id
	}
	return "asset://" + id
}

func volcHMAC(key []byte, msg string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func findCachedAssetPreview(assetsDir, cacheDir, assetID string) string {
	prefix := safeAssetCacheName.ReplaceAllString(strings.TrimPrefix(assetID, "asset://"), "_") + "."
	entries, _ := os.ReadDir(cacheDir)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			return cachedAssetURL(assetsDir, filepath.Join(cacheDir, entry.Name()))
		}
	}
	return ""
}

func cacheFileForAsset(cacheDir, assetID, contentType, sourceURL string) string {
	ext := ""
	if contentType != "" {
		exts, _ := mime.ExtensionsByType(strings.Split(contentType, ";")[0])
		if len(exts) > 0 {
			ext = exts[0]
		}
	}
	if ext == "" {
		ext = filepath.Ext(sourceURL)
	}
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
	default:
		ext = ".jpg"
	}
	return filepath.Join(cacheDir, safeAssetCacheName.ReplaceAllString(strings.TrimPrefix(assetID, "asset://"), "_")+ext)
}

func cachedAssetURL(assetsDir, path string) string {
	rel, err := filepath.Rel(assetsDir, path)
	if err != nil {
		return ""
	}
	return "/assets/" + filepath.ToSlash(rel)
}

func pageClamp(value, fallback, maximum int) int {
	if value <= 0 {
		value = fallback
	}
	if value > maximum {
		value = maximum
	}
	return value
}

func truncateString(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func anyListFromMap(obj map[string]any, key string) []any {
	if obj == nil {
		return nil
	}
	if list, ok := obj[key].([]any); ok {
		return list
	}
	return nil
}

func firstAnyListFromMap(obj map[string]any, keys ...string) []any {
	for _, key := range keys {
		if list := anyListFromMap(obj, key); len(list) > 0 {
			return list
		}
	}
	return nil
}

func mapListValue(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if obj, ok := item.(map[string]any); ok {
				out = append(out, obj)
			}
		}
		return out
	default:
		return nil
	}
}

func hasPresetPortraitFilters(filters PresetPortraitFilters) bool {
	return strings.TrimSpace(filters.Gender) != "" ||
		strings.TrimSpace(filters.Country) != "" ||
		strings.TrimSpace(filters.Occupation) != "" ||
		strings.TrimSpace(filters.Temperament) != ""
}

func matchesPresetPortraitFilters(item map[string]any, filters PresetPortraitFilters) bool {
	metadata, _ := item["metadata"].(map[string]any)
	return matchesFilterValue(metadataText(metadata, "Gender"), filters.Gender) &&
		matchesFilterValue(metadataText(metadata, "Country"), filters.Country) &&
		matchesFilterValue(metadataText(metadata, "Occupation"), filters.Occupation) &&
		matchesFilterValue(metadataText(metadata, "Temperament"), filters.Temperament)
}

func matchesFilterValue(value, filter string) bool {
	text := strings.TrimSpace(filter)
	if text == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(value), text)
}

func firstStringFromMap(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := stringFromMap(obj, key); text != "" {
			return text
		}
	}
	return ""
}

func firstRecursiveStringByKeys(value any, keys ...string) string {
	for _, key := range keys {
		if text := recursiveStringByKey(value, key); text != "" {
			return text
		}
	}
	return ""
}

func recursiveStringByKey(value any, key string) string {
	switch typed := value.(type) {
	case map[string]any:
		if text := stringFromMap(typed, key); text != "" {
			return text
		}
		for _, child := range typed {
			if text := recursiveStringByKey(child, key); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range typed {
			if text := recursiveStringByKey(child, key); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstAssetID(values ...string) string {
	fallback := ""
	for _, value := range values {
		text := strings.TrimSpace(strings.TrimPrefix(value, "asset://"))
		if text == "" {
			continue
		}
		if fallback == "" {
			fallback = text
		}
		if assetIDPattern.MatchString(text) {
			return text
		}
	}
	return fallback
}

func firstUsablePreviewURL(values ...string) string {
	for _, value := range values {
		if text := normalizePreviewURL(value); text != "" {
			return text
		}
	}
	return ""
}

func normalizePreviewURL(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "//") {
		return "https:" + text
	}
	if isHTTPURL(text) || strings.HasPrefix(text, "/") || strings.HasPrefix(text, "data:image/") {
		return text
	}
	return ""
}

func isHTTPURL(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")
}

func stringFromMap(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	if text, ok := obj[key].(string); ok {
		return text
	}
	return ""
}

func intValue(value any, fallback any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return intValue(fallback, 0)
	}
}

func firstValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func normalizeTags(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return typed
	case string:
		parts := strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return []string{}
	}
}

func metadataText(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	switch value := metadata[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", value))
	case int:
		return strings.TrimSpace(fmt.Sprintf("%d", value))
	default:
		return ""
	}
}

func addFilterValue(values map[string]int, value string) {
	text := strings.TrimSpace(value)
	if text == "" {
		return
	}
	values[text]++
}

func sortedFilterOptions(values map[string]int) []map[string]any {
	options := make([]map[string]any, 0, len(values))
	for value, count := range values {
		options = append(options, map[string]any{"value": value, "count": count})
	}
	sort.Slice(options, func(i, j int) bool {
		ci, _ := options[i]["count"].(int)
		cj, _ := options[j]["count"].(int)
		if ci != cj {
			return ci > cj
		}
		vi, _ := options[i]["value"].(string)
		vj, _ := options[j]["value"].(string)
		return vi < vj
	})
	return options
}
