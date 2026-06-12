package handler

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"infinite-canvas/app-go/internal/store"
)

type DeleteHistoryRequest struct {
	Timestamp float64 `json:"timestamp"`
}

type CanvasAssetCheckRequest struct {
	URLs []string `json:"urls"`
}

type CanvasAssetDownloadRequest struct {
	URLs     []string `json:"urls"`
	Filename string   `json:"filename"`
}

type NodeMediaDeleteRequest struct {
	URL string `json:"url"`
}

func (h *Handler) GetHistory(c *gin.Context) {
	c.JSON(http.StatusOK, h.store.LoadHistory(c.Query("type")))
}

func (h *Handler) DeleteHistory(c *gin.Context) {
	var payload DeleteHistoryRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	record, err := h.store.DeleteHistory(payload.Timestamp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	if record == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Record not found"})
		return
	}
	if images, ok := record["images"].([]any); ok {
		for _, imageURL := range images {
			if path := h.store.OutputFileFromURL(imageURL); path != "" {
				_ = os.Remove(path)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *Handler) GetQueueStatus(c *gin.Context) {
	clientID := c.Query("client_id")
	h.comfyMu.Lock()
	total := len(h.comfyQueue)
	position := 0
	for index, task := range h.comfyQueue {
		if task["client_id"] == clientID {
			position = index + 1
			break
		}
	}
	h.comfyMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"total": total, "position": position})
}

func (h *Handler) DownloadOutput(c *gin.Context) {
	path := h.store.OutputFileFromURL(c.Query("url"))
	if path == "" {
		c.JSON(http.StatusNotFound, gin.H{"detail": "文件不存在"})
		return
	}
	filename := filepath.Base(c.Query("name"))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		filename = filepath.Base(path)
	}
	c.Header("Content-Disposition", contentDisposition(filename))
	c.File(path)
}

func (h *Handler) MediaPreview(c *gin.Context) {
	path := h.store.OutputFileFromURL(c.Query("url"))
	if path == "" {
		c.JSON(http.StatusNotFound, gin.H{"detail": "媒体文件不存在"})
		return
	}
	if !isPreviewImagePath(path) {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"detail": "当前预览接口仅支持图片缩略图"})
		return
	}
	width, err := strconv.Atoi(c.DefaultQuery("w", "512"))
	if err != nil {
		width = 512
	}
	width = clampInt(width, 64, 2048)

	cachePath, mediaType, ok := h.mediaPreviewCache(path, width)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"detail": "媒体文件不存在"})
		return
	}
	if _, err := os.Stat(cachePath); err == nil {
		c.Header("Content-Type", mediaType)
		c.File(cachePath)
		return
	}

	if err := os.MkdirAll(h.cfg.MediaPreviewDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "创建预览缓存目录失败：" + err.Error()})
		return
	}
	if err := generateImagePreview(path, cachePath, width, mediaType); err != nil {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"detail": "无法生成预览图：" + err.Error()})
		return
	}
	c.Header("Content-Type", mediaType)
	c.File(cachePath)
}

func (h *Handler) mediaPreviewCache(path string, width int) (string, string, bool) {
	stat, err := os.Stat(path)
	if err != nil || stat.IsDir() {
		return "", "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	sum := sha1.Sum([]byte(abs + "|" + strconv.FormatInt(stat.ModTime().UnixNano(), 10) + "|" + strconv.FormatInt(stat.Size(), 10) + "|" + strconv.Itoa(width)))
	ext := ".jpg"
	mediaType := "image/jpeg"
	if previewShouldUsePNG(path) {
		ext = ".png"
		mediaType = "image/png"
	}
	return filepath.Join(h.cfg.MediaPreviewDir, hex.EncodeToString(sum[:])+ext), mediaType, true
}

func (h *Handler) ProxyComfyView(c *gin.Context) {
	filename := strings.TrimSpace(c.Query("filename"))
	if filename == "" || strings.Contains(filename, "\\") || strings.Contains(filename, "/") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid filename"})
		return
	}
	fileType := firstNonEmptyString(c.Query("type"), "input")
	subfolder := strings.TrimSpace(c.Query("subfolder"))
	client := &http.Client{Timeout: time.Second}
	for _, backend := range h.cfg.ComfyUIInstances {
		endpoint := "http://" + backend + "/view?filename=" + url.QueryEscape(filename) + "&type=" + url.QueryEscape(fileType) + "&subfolder=" + url.QueryEscape(subfolder)
		resp, err := client.Get(endpoint)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
		ct := resp.Header.Get("Content-Type")
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK && len(data) > 0 {
			c.Data(http.StatusOK, firstNonEmptyString(ct, "application/octet-stream"), data)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"detail": "Image not found on any available backend"})
}

func (h *Handler) UploadComfyImage(c *gin.Context) {
	files, ok := multipartFiles(c, "files")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "缺少上传文件"})
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	uploaded := make([]gin.H, 0, len(files))
	for _, file := range files {
		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "读取上传文件失败"})
			return
		}
		content, err := io.ReadAll(io.LimitReader(src, 64*1024*1024))
		_ = src.Close()
		if err != nil || len(content) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "上传文件为空或过大"})
			return
		}
		success := 0
		comfyName := file.Filename
		for _, backend := range h.cfg.ComfyUIInstances {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			part, err := writer.CreateFormFile("image", file.Filename)
			if err != nil {
				continue
			}
			_, _ = part.Write(content)
			_ = writer.Close()
			req, err := http.NewRequest(http.MethodPost, "http://"+backend+"/upload/image", body)
			if err != nil {
				continue
			}
			req.Header.Set("Content-Type", writer.FormDataContentType())
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			var raw map[string]any
			if resp.StatusCode == http.StatusOK {
				_ = json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&raw)
				if name := stringValue(raw["name"], ""); name != "" {
					comfyName = name
				}
				success++
			}
			_ = resp.Body.Close()
		}
		if success == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to upload to any backend"})
			return
		}
		uploaded = append(uploaded, gin.H{"comfy_name": comfyName})
	}
	c.JSON(http.StatusOK, gin.H{"files": uploaded})
}

func (h *Handler) CheckCanvasAssets(c *gin.Context) {
	var payload CanvasAssetCheckRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	exists := map[string]bool{}
	limit := len(payload.URLs)
	if limit > 3000 {
		limit = 3000
	}
	for _, item := range payload.URLs[:limit] {
		if item == "" {
			continue
		}
		if len(item) >= 8 && (item[:8] == "/output/" || item[:8] == "/assets/") {
			exists[item] = h.store.OutputFileFromURL(item) != ""
		} else {
			exists[item] = true
		}
	}
	c.JSON(http.StatusOK, gin.H{"exists": exists})
}

func (h *Handler) DownloadCanvasAssets(c *gin.Context) {
	var payload CanvasAssetDownloadRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}

	type fileItem struct {
		path string
		name string
	}
	files := make([]fileItem, 0)
	used := map[string]bool{}
	limit := len(payload.URLs)
	if limit > 1000 {
		limit = 1000
	}
	for _, item := range payload.URLs[:limit] {
		text := strings.TrimSpace(item)
		if text == "" || (!strings.HasPrefix(text, "/output/") && !strings.HasPrefix(text, "/assets/")) {
			continue
		}
		path := h.store.OutputFileFromURL(text)
		if path == "" {
			continue
		}
		base := filepath.Base(path)
		if base == "." || base == "" {
			base = "image"
		}
		archiveName := uniqueArchiveName(base, used)
		files = append(files, fileItem{path: path, name: archiveName})
	}
	if len(files) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "没有可下载的本地图片"})
		return
	}

	filename := store.SafeArchiveName(payload.Filename, "canvas-output-images.zip")
	if !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		filename += ".zip"
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", contentDisposition(filename))
	zipWriter := zip.NewWriter(c.Writer)
	defer zipWriter.Close()
	for _, item := range files {
		if err := addFileToZip(zipWriter, item.path, item.name); err != nil {
			return
		}
	}
}

func (h *Handler) UploadAIReference(c *gin.Context) {
	files, ok := multipartFiles(c, "files")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "缺少上传文件"})
		return
	}
	uploaded := make([]gin.H, 0, len(files))
	for _, file := range files {
		ext := normalizedUploadExt(file.Filename, file.Header.Get("Content-Type"), map[string]bool{
			".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
		}, ".png")
		filename := "ai_ref_" + store.NewHexID()[:12] + ext
		path := h.store.OutputPathFor(filename, "input")
		if err := saveMultipartFile(file, path); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存上传文件失败：" + err.Error()})
			return
		}
		uploaded = append(uploaded, gin.H{"url": h.store.OutputURLFor(filename, "input"), "name": firstNonEmptyString(file.Filename, filename)})
	}
	c.JSON(http.StatusOK, gin.H{"files": uploaded})
}

func (h *Handler) ImportWebGeneratorImage(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Empty image file"})
		return
	}
	if file.Size <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Empty image file"})
		return
	}
	if file.Size > 64*1024*1024 {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"detail": "Image file is too large"})
		return
	}
	content, err := readMultipartFile(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "读取上传文件失败：" + err.Error()})
		return
	}
	format, width, height, ext, contentType, err := detectUploadedImage(content, file.Filename, file.Header.Get("Content-Type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	filename := "webgen_" + store.NewHexID()[:12] + ext
	path := h.store.OutputPathFor(filename, "output")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存上传文件失败：" + err.Error()})
		return
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存上传文件失败：" + err.Error()})
		return
	}

	widthValue := firstNonEmptyString(c.PostForm("width"), strconv.Itoa(width))
	heightValue := firstNonEmptyString(c.PostForm("height"), strconv.Itoa(height))
	byteSizeValue := firstNonEmptyString(c.PostForm("byte_size"), strconv.Itoa(len(content)))
	c.JSON(http.StatusOK, gin.H{
		"url":          h.store.OutputURLFor(filename, "output"),
		"filename":     filename,
		"name":         firstNonEmptyString(file.Filename, filename),
		"content_type": contentType,
		"width":        widthValue,
		"height":       heightValue,
		"byte_size":    byteSizeValue,
		"metadata": gin.H{
			"prompt":           c.PostForm("prompt"),
			"prompt_id":        c.PostForm("prompt_id"),
			"source":           "chat-image-web-extension",
			"source_site":      c.PostForm("source_site"),
			"source_url":       c.PostForm("source_url"),
			"source_tab_title": c.PostForm("source_tab_title"),
			"captured_at":      c.PostForm("captured_at"),
			"detected_format":  format,
		},
	})
}

func (h *Handler) UploadNodeMedia(c *gin.Context) {
	files, ok := multipartFiles(c, "files")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "缺少上传文件"})
		return
	}
	allowed := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
		".mp4": true, ".mov": true, ".webm": true,
		".wav": true, ".mp3": true, ".m4a": true, ".aac": true, ".flac": true, ".ogg": true,
	}
	uploaded := make([]gin.H, 0, len(files))
	for _, file := range files {
		ext := normalizedUploadExt(file.Filename, file.Header.Get("Content-Type"), allowed, "")
		if ext == "" {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "不支持的媒体格式：" + firstNonEmptyString(file.Filename, file.Header.Get("Content-Type"))})
			return
		}
		filename := "node_media_" + store.NewHexID()[:12] + ext
		path := h.store.OutputPathFor(filename, "input")
		if err := saveMultipartFile(file, path); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存上传文件失败：" + err.Error()})
			return
		}
		uploaded = append(uploaded, gin.H{
			"url":          h.store.OutputURLFor(filename, "input"),
			"name":         firstNonEmptyString(file.Filename, filename),
			"content_type": store.ContentTypeForPath(path),
		})
	}
	c.JSON(http.StatusOK, gin.H{"files": uploaded})
}

func (h *Handler) DeleteNodeMedia(c *gin.Context) {
	var payload NodeMediaDeleteRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	path := h.store.OutputFileFromURL(payload.URL)
	if path == "" {
		c.JSON(http.StatusOK, gin.H{"deleted": false})
		return
	}
	absPath, _ := filepath.Abs(path)
	inputRoot, _ := filepath.Abs(h.cfg.OutputInputDir)
	rel, err := filepath.Rel(inputRoot, absPath)
	filename := filepath.Base(absPath)
	ext := strings.ToLower(filepath.Ext(filename))
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) || !strings.HasPrefix(filename, "node_media_") || ext != ".wav" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Only generated project WAV media can be deleted"})
		return
	}
	if err := os.Remove(absPath); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{"deleted": false})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to delete media: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func contentDisposition(filename string) string {
	escaped := url.QueryEscape(filename)
	return "attachment; filename*=UTF-8''" + escaped
}

func isPreviewImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.SplitN(path, "?", 2)[0])) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func previewShouldUsePNG(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.SplitN(path, "?", 2)[0])) {
	case ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func generateImagePreview(sourcePath, cachePath string, maxWidth int, mediaType string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return err
	}
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return errors.New("图片尺寸无效")
	}
	scale := math.Min(1, float64(maxWidth)/float64(srcW))
	dstW := max(1, int(math.Round(float64(srcW)*scale)))
	dstH := max(1, int(math.Round(float64(srcH)*scale)))
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, srcBounds, xdraw.Over, nil)

	tmpPath := cachePath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if mediaType == "image/png" {
		err = png.Encode(out, dst)
	} else {
		err = jpeg.Encode(out, dst, &jpeg.Options{Quality: 82})
	}
	closeErr := out.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func uniqueArchiveName(base string, used map[string]bool) string {
	if !used[base] {
		used[base] = true
		return base
	}
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for suffix := 2; ; suffix++ {
		candidate := name + "-" + strconv.Itoa(suffix) + ext
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

func addFileToZip(zipWriter *zip.Writer, filePath, archiveName string) error {
	source, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := zipWriter.Create(archiveName)
	if err != nil {
		return err
	}
	_, err = io.Copy(target, source)
	return err
}

func multipartFiles(c *gin.Context, field string) ([]*multipart.FileHeader, bool) {
	form, err := c.MultipartForm()
	if err != nil || form == nil {
		return nil, false
	}
	files := form.File[field]
	if len(files) == 0 {
		return nil, false
	}
	return files, true
}

func saveMultipartFile(file *multipart.FileHeader, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	source, err := file.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer target.Close()
	_, err = io.Copy(target, source)
	return err
}

func readMultipartFile(file *multipart.FileHeader) ([]byte, error) {
	source, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer source.Close()
	return io.ReadAll(source)
}

func detectUploadedImage(content []byte, filename, declaredContentType string) (string, int, int, string, string, error) {
	declaredContentType = strings.ToLower(strings.TrimSpace(declaredContentType))
	if declaredContentType != "" && !strings.HasPrefix(declaredContentType, "image/") {
		return "", 0, 0, "", "", errors.New("Unsupported image content type: " + declaredContentType)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return "", 0, 0, "", "", errors.New("Uploaded file is not a valid image")
	}
	format = strings.ToLower(format)
	ext := strings.ToLower(filepath.Ext(filename))
	contentType := ""
	switch format {
	case "png":
		ext = ".png"
		contentType = "image/png"
	case "jpeg":
		ext = ".jpg"
		contentType = "image/jpeg"
	case "gif":
		ext = ".gif"
		contentType = "image/gif"
	case "webp":
		ext = ".webp"
		contentType = "image/webp"
	default:
		return "", 0, 0, "", "", errors.New("Unsupported image format: " + format)
	}
	return strings.ToUpper(format), config.Width, config.Height, ext, contentType, nil
}

func normalizedUploadExt(filename, contentType string, allowed map[string]bool, fallback string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if allowed[ext] {
		return ext
	}
	contentType = strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(contentType, "image/"):
		if strings.Contains(contentType, "jpeg") {
			ext = ".jpg"
		} else if strings.Contains(contentType, "webp") {
			ext = ".webp"
		} else {
			ext = ".png"
		}
	case strings.HasPrefix(contentType, "video/"):
		if strings.Contains(contentType, "quicktime") {
			ext = ".mov"
		} else if strings.Contains(contentType, "webm") {
			ext = ".webm"
		} else {
			ext = ".mp4"
		}
	case strings.HasPrefix(contentType, "audio/"):
		if strings.Contains(contentType, "mpeg") {
			ext = ".mp3"
		} else if strings.Contains(contentType, "mp4") {
			ext = ".m4a"
		} else if strings.Contains(contentType, "ogg") {
			ext = ".ogg"
		} else {
			ext = ".wav"
		}
	default:
		ext = fallback
	}
	if allowed[ext] {
		return ext
	}
	return fallback
}
