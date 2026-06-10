package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/store"
)

type ComfyInstancesPayload struct {
	Instances []string `json:"instances"`
}

type WorkflowUploadRequest struct {
	Name     string         `json:"name"`
	Workflow map[string]any `json:"workflow"`
}

type WorkflowConfigRequest struct {
	Title     string          `json:"title"`
	Fields    []WorkflowField `json:"fields"`
	MiniCards map[string]any  `json:"mini_cards"`
}

type WorkflowField struct {
	ID            string   `json:"id"`
	Node          string   `json:"node"`
	Input         string   `json:"input"`
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	Default       any      `json:"default"`
	Min           *float64 `json:"min"`
	Max           *float64 `json:"max"`
	Step          *float64 `json:"step"`
	Options       []string `json:"options"`
	RandomEnabled bool     `json:"random_enabled"`
}

type WorkflowRunRequest struct {
	Fields   map[string]any        `json:"fields"`
	Config   WorkflowConfigRequest `json:"config"`
	ClientID string                `json:"client_id"`
}

type GenerateRequest struct {
	Prompt       string         `json:"prompt"`
	Width        int            `json:"width"`
	Height       int            `json:"height"`
	WorkflowJSON string         `json:"workflow_json"`
	Params       map[string]any `json:"params"`
	Type         string         `json:"type"`
	ClientID     string         `json:"client_id"`
	ConvertToJPG bool           `json:"convert_to_jpg"`
}

var comfyInstancePattern = regexp.MustCompile(`^https?://`)

func (h *Handler) ListWorkflows(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"workflows": h.store.ListWorkflows()})
}

func (h *Handler) GetWorkflow(c *gin.Context) {
	name := pathParamWithoutSlash(c, "name")
	workflow, err := h.store.GetWorkflow(name)
	if err != nil {
		writeStoreError(c, err, "Workflow not found")
		return
	}
	c.JSON(http.StatusOK, workflow)
}

func (h *Handler) SaveComfyUIInstances(c *gin.Context) {
	var payload ComfyInstancesPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	cleaned := make([]string, 0, len(payload.Instances))
	seen := map[string]bool{}
	for _, item := range payload.Instances {
		value := strings.TrimSpace(item)
		value = comfyInstancePattern.ReplaceAllString(value, "")
		value = strings.TrimRight(value, "/")
		if value == "" {
			continue
		}
		host, port, ok := strings.Cut(value, ":")
		if !ok || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" || !allDigits(port) {
			c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("地址不合法：%s（应为 host:port，例如 127.0.0.1:8188）", item)})
			return
		}
		if !seen[value] {
			cleaned = append(cleaned, value)
			seen[value] = true
		}
	}
	if len(cleaned) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "至少保留一个 ComfyUI 后端地址"})
		return
	}
	if err := h.store.UpdateEnvValues(map[string]string{"COMFYUI_INSTANCES": strings.Join(cleaned, ",")}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "写入 env 失败：" + err.Error()})
		return
	}
	h.cfg.RefreshFromEnv()
	h.resetComfyBackendLoad(cleaned)
	c.JSON(http.StatusOK, gin.H{"instances": cleaned})
}

func (h *Handler) UploadWorkflow(c *gin.Context) {
	var payload WorkflowUploadRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	name, err := h.store.SaveWorkflow(payload.Name, payload.Workflow)
	if err != nil {
		if errors.Is(err, store.ErrBadID) {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "工作流名称不合法，请使用中文/英文/数字/_-."})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"name": name})
}

func (h *Handler) SaveWorkflowConfig(c *gin.Context) {
	name := pathParamWithoutSlash(c, "name")
	name = strings.TrimSuffix(name, "/config")
	if name == pathParamWithoutSlash(c, "name") {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Workflow not found"})
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	if err := h.store.SaveWorkflowConfig(name, payload); err != nil {
		writeStoreError(c, err, "Workflow not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"config": payload})
}

func (h *Handler) DeleteWorkflow(c *gin.Context) {
	name := pathParamWithoutSlash(c, "name")
	if err := h.store.DeleteWorkflow(name); err != nil {
		if errors.Is(err, store.ErrBadID) {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid workflow name"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) RunWorkflow(c *gin.Context) {
	name := pathParamWithoutSlash(c, "name")
	name = strings.TrimSuffix(name, "/run")
	if name == pathParamWithoutSlash(c, "name") {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Workflow not found"})
		return
	}
	var payload WorkflowRunRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	params := map[string]any{}
	for _, field := range payload.Config.Fields {
		if field.Node == "" || field.Input == "" {
			continue
		}
		value, ok := payload.Fields[field.ID]
		if !ok {
			continue
		}
		value = coerceWorkflowFieldValue(value, field)
		nodeInputs, _ := params[field.Node].(map[string]any)
		if nodeInputs == nil {
			nodeInputs = map[string]any{}
			params[field.Node] = nodeInputs
		}
		nodeInputs[field.Input] = value
	}
	h.generateComfy(c, GenerateRequest{WorkflowJSON: name, Params: params, Type: "workflow-test", ClientID: firstNonEmptyString(payload.ClientID, store.NewHexID())})
}

func (h *Handler) GenerateComfy(c *gin.Context) {
	var payload GenerateRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	h.generateComfy(c, payload)
}

func (h *Handler) generateComfy(c *gin.Context, req GenerateRequest) {
	if req.Width <= 0 {
		req.Width = 1024
	}
	if req.Height <= 0 {
		req.Height = 1024
	}
	if req.WorkflowJSON == "" {
		req.WorkflowJSON = "Z-Image.json"
	}
	if req.Type == "" {
		req.Type = "zimage"
	}
	taskID, currentTask := h.enqueueComfyTask(req.ClientID)
	targetBackend := ""
	defer func() {
		if targetBackend != "" {
			h.decrementComfyLoad(targetBackend)
		}
		h.dequeueComfyTask(currentTask)
	}()

	requiredImages := requiredComfyImages(req.Params)
	targetBackend = h.bestComfyBackend(requiredImages)
	h.incrementComfyLoad(targetBackend)
	h.syncRequiredImages(targetBackend, requiredImages)

	workflowPayload, err := h.loadWorkflowPayload(req.WorkflowJSON)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"images": []string{}, "error": err.Error()})
		return
	}
	seed := rand.New(rand.NewSource(time.Now().UnixNano())).Int63n(1_000_000_000_000_000-1) + 1
	injectComfyDefaults(workflowPayload, req, seed)

	promptID, err := postComfyPrompt(targetBackend, workflowPayload, req.ClientID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"images": []string{}, "error": err.Error()})
		return
	}
	historyData, err := pollComfyHistory(targetBackend, promptID, 600*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"images": []string{}, "error": err.Error()})
		return
	}
	images, videos, outputs := h.downloadComfyOutputs(targetBackend, historyData, req.Type, req.ConvertToJPG)
	result := map[string]any{
		"prompt":        firstNonEmptyString(req.Prompt, "Detail Enhance"),
		"images":        images,
		"videos":        videos,
		"outputs":       outputs,
		"seed":          seed,
		"timestamp":     nowSeconds(),
		"type":          req.Type,
		"workflow_json": req.WorkflowJSON,
		"task_id":       taskID,
		"prompt_id":     promptID,
		"backend":       targetBackend,
		"params":        req.Params,
	}
	_ = h.store.SaveHistory(result)
	h.rt.BroadcastNewImage(result)
	c.JSON(http.StatusOK, result)
}

func (h *Handler) enqueueComfyTask(clientID string) (int, map[string]string) {
	h.comfyMu.Lock()
	defer h.comfyMu.Unlock()
	taskID := h.comfyNextTaskID
	h.comfyNextTaskID++
	task := map[string]string{"task_id": fmt.Sprint(taskID), "client_id": clientID}
	h.comfyQueue = append(h.comfyQueue, task)
	return taskID, task
}

func (h *Handler) dequeueComfyTask(task map[string]string) {
	h.comfyMu.Lock()
	defer h.comfyMu.Unlock()
	next := h.comfyQueue[:0]
	for _, item := range h.comfyQueue {
		if item["task_id"] != task["task_id"] {
			next = append(next, item)
		}
	}
	h.comfyQueue = next
}

func (h *Handler) resetComfyBackendLoad(instances []string) {
	h.comfyMu.Lock()
	defer h.comfyMu.Unlock()
	next := map[string]int{}
	for _, instance := range instances {
		next[instance] = h.comfyBackendLoad[instance]
	}
	h.comfyBackendLoad = next
}

func (h *Handler) incrementComfyLoad(backend string) {
	h.comfyMu.Lock()
	h.comfyBackendLoad[backend]++
	h.comfyMu.Unlock()
}

func (h *Handler) decrementComfyLoad(backend string) {
	h.comfyMu.Lock()
	if h.comfyBackendLoad[backend] > 0 {
		h.comfyBackendLoad[backend]--
	}
	h.comfyMu.Unlock()
}

func (h *Handler) bestComfyBackend(requiredImages []string) string {
	if len(h.cfg.ComfyUIInstances) == 0 {
		return "127.0.0.1:8188"
	}
	best := h.cfg.ComfyUIInstances[0]
	bestLoad := int(^uint(0) >> 1)
	for _, backend := range h.cfg.ComfyUIInstances {
		load := h.remoteComfyQueueSize(backend)
		h.comfyMu.Lock()
		local := h.comfyBackendLoad[backend]
		h.comfyMu.Unlock()
		if local > load {
			load = local
		}
		if len(requiredImages) > 0 && !h.comfyImagesExist(backend, requiredImages) {
			load += 100000
		}
		if load < bestLoad {
			best = backend
			bestLoad = load
		}
	}
	return best
}

func (h *Handler) remoteComfyQueueSize(backend string) int {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + backend + "/queue")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var data map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&data); err != nil {
		return 0
	}
	return lenAny(data["queue_running"]) + lenAny(data["queue_pending"])
}

func (h *Handler) comfyImagesExist(backend string, images []string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for _, image := range images {
		resp, err := client.Get("http://" + backend + "/view?filename=" + url.QueryEscape(image) + "&type=input")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
	}
	return true
}

func (h *Handler) syncRequiredImages(target string, images []string) {
	for _, image := range images {
		if h.comfyImagesExist(target, []string{image}) {
			continue
		}
		var content []byte
		contentType := "image/png"
		for _, backend := range h.cfg.ComfyUIInstances {
			if backend == target {
				continue
			}
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Get("http://" + backend + "/view?filename=" + url.QueryEscape(image) + "&type=input")
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil {
					_ = resp.Body.Close()
				}
				continue
			}
			contentType = firstNonEmptyString(resp.Header.Get("Content-Type"), contentType)
			content, _ = io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
			_ = resp.Body.Close()
			break
		}
		if len(content) == 0 {
			continue
		}
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		_ = writer.WriteField("type", "input")
		part, err := writer.CreateFormFile("image", filepath.Base(image))
		if err == nil {
			_, _ = part.Write(content)
		}
		_ = writer.Close()
		req, err := http.NewRequest(http.MethodPost, "http://"+target+"/upload/image", body)
		if err != nil {
			continue
		}
		_ = contentType
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}
}

func (h *Handler) loadWorkflowPayload(name string) (map[string]any, error) {
	record, err := h.store.GetWorkflow(name)
	if err != nil {
		return nil, fmt.Errorf("Workflow file not found: %s", name)
	}
	workflow, ok := record["workflow"].(map[string]any)
	if !ok || len(workflow) == 0 {
		return nil, fmt.Errorf("Workflow file not found: %s", name)
	}
	return workflow, nil
}

func postComfyPrompt(backend string, workflow map[string]any, clientID string) (string, error) {
	if clientID == "" {
		clientID = store.NewHexID()
	}
	payload, _ := json.Marshal(map[string]any{"prompt": workflow, "client_id": clientID})
	resp, err := http.Post("http://"+backend+"/prompt", "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP Error %d: %s", resp.StatusCode, string(data))
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	id := stringValue(raw["prompt_id"], "")
	if id == "" {
		return "", errors.New("ComfyUI 没有返回 prompt_id")
	}
	return id, nil
}

func pollComfyHistory(backend, promptID string, timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 10 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + backend + "/history/" + url.PathEscape(promptID))
		if err == nil {
			var raw map[string]any
			decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 16*1024*1024)).Decode(&raw)
			_ = resp.Body.Close()
			if decodeErr == nil {
				if item, ok := raw[promptID].(map[string]any); ok {
					return item, nil
				}
			}
		}
		time.Sleep(time.Second)
	}
	return nil, errors.New("ComfyUI 渲染超时")
}

func (h *Handler) downloadComfyOutputs(backend string, history map[string]any, outputType string, convertToJPG bool) ([]string, []string, []string) {
	images := []string{}
	videos := []string{}
	outputs := []string{}
	outputMap, _ := history["outputs"].(map[string]any)
	for _, rawNode := range outputMap {
		node, _ := rawNode.(map[string]any)
		for _, rawImage := range anyList(node["images"]) {
			item, _ := rawImage.(map[string]any)
			if stringValue(item["filename"], "") == "" {
				continue
			}
			local := h.downloadComfyOutput(backend, item, fmt.Sprintf("%s_%d_", outputType, time.Now().Unix()), convertToJPG)
			images = append(images, local)
			outputs = append(outputs, local)
		}
		for _, key := range []string{"videos", "gifs", "animated"} {
			for _, rawVideo := range anyList(node[key]) {
				item, _ := rawVideo.(map[string]any)
				if stringValue(item["filename"], "") == "" {
					continue
				}
				local := h.downloadComfyOutput(backend, item, fmt.Sprintf("%s_%d_", outputType, time.Now().Unix()), false)
				videos = append(videos, local)
				outputs = append(outputs, local)
			}
		}
	}
	return images, videos, outputs
}

func (h *Handler) downloadComfyOutput(backend string, item map[string]any, prefix string, convertToJPG bool) string {
	ext := comfyOutputExtension(item)
	filename := prefix + store.NewHexID()[:10] + ext
	subfolder := url.QueryEscape(stringValue(item["subfolder"], ""))
	fileType := url.QueryEscape(firstNonEmptyString(stringValue(item["type"], ""), "output"))
	comfyPath := "/view?filename=" + url.QueryEscape(stringValue(item["filename"], "")) + "&subfolder=" + subfolder + "&type=" + fileType
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get("http://" + backend + comfyPath)
	if err != nil {
		return strings.Replace(comfyPath, "/view", "/api/view", 1)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return strings.Replace(comfyPath, "/view", "/api/view", 1)
	}
	path := h.store.OutputPathFor(filename, "output")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "http://" + backend + comfyPath
	}
	out, err := os.Create(path)
	if err != nil {
		return "http://" + backend + comfyPath
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 1024*1024*1024)); err != nil {
		return "http://" + backend + comfyPath
	}
	_ = convertToJPG
	return h.store.OutputURLFor(filename, "output")
}

func injectComfyDefaults(workflow map[string]any, req GenerateRequest, seed int64) {
	setComfyInput(workflow, "23", "text", req.Prompt, req.Prompt != "")
	setComfyInput(workflow, "144", "width", req.Width, true)
	setComfyInput(workflow, "144", "height", req.Height, true)
	setComfyInput(workflow, "22", "seed", seed, true)
	setComfyInput(workflow, "158", "noise_seed", seed, true)
	for _, nodeID := range []string{"146", "181", "184", "14"} {
		setComfyInputIfExists(workflow, nodeID, "seed", seed)
	}
	setComfyInputIfExists(workflow, "172", "seed", seed%4294967295)
	for nodeID, rawInputs := range req.Params {
		node, _ := workflow[nodeID].(map[string]any)
		inputs, _ := rawInputs.(map[string]any)
		if node == nil || inputs == nil {
			continue
		}
		target, _ := node["inputs"].(map[string]any)
		if target == nil {
			target = map[string]any{}
			node["inputs"] = target
		}
		for key, value := range inputs {
			target[key] = value
		}
	}
}

func setComfyInput(workflow map[string]any, nodeID, input string, value any, enabled bool) {
	if !enabled {
		return
	}
	node, _ := workflow[nodeID].(map[string]any)
	if node == nil {
		return
	}
	inputs, _ := node["inputs"].(map[string]any)
	if inputs == nil {
		inputs = map[string]any{}
		node["inputs"] = inputs
	}
	inputs[input] = value
}

func setComfyInputIfExists(workflow map[string]any, nodeID, input string, value any) {
	node, _ := workflow[nodeID].(map[string]any)
	inputs, _ := node["inputs"].(map[string]any)
	if inputs == nil {
		return
	}
	if _, ok := inputs[input]; ok {
		inputs[input] = value
	}
}

func requiredComfyImages(params map[string]any) []string {
	out := []string{}
	for _, rawInputs := range params {
		inputs, _ := rawInputs.(map[string]any)
		if image := strings.TrimSpace(stringValue(inputs["image"], "")); image != "" {
			out = append(out, image)
		}
	}
	return out
}

func comfyOutputExtension(item map[string]any) string {
	ext := strings.ToLower(filepath.Ext(stringValue(item["filename"], "")))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".mp4", ".webm", ".mov", ".m4v", ".gif":
		return ext
	}
	format := strings.ToLower(stringValue(item["format"], ""))
	switch {
	case strings.Contains(format, "webm"):
		return ".webm"
	case strings.Contains(format, "quicktime"), strings.Contains(format, "mov"):
		return ".mov"
	case strings.Contains(format, "mp4"), strings.Contains(format, "h264"), strings.Contains(format, "video"):
		return ".mp4"
	default:
		return ".png"
	}
}

func coerceWorkflowFieldValue(value any, field WorkflowField) any {
	switch field.Type {
	case "number", "slider":
		n, ok := numericWorkflowValue(value)
		if ok {
			if field.Step != nil && *field.Step < 1 {
				return n
			}
			return int(n)
		}
	case "boolean":
		if b, ok := value.(bool); ok {
			return b
		}
		return stringValue(value, "") != ""
	case "dropdown":
		text, ok := value.(string)
		if !ok {
			return value
		}
		if strings.ContainsAny(text, ".eE") {
			var f float64
			if _, err := fmt.Sscanf(text, "%f", &f); err == nil {
				return f
			}
		}
		var i int
		if _, err := fmt.Sscanf(text, "%d", &i); err == nil {
			return i
		}
	}
	return value
}

func numericWorkflowValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func lenAny(value any) int { return len(anyList(value)) }

func anyList(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return nil
}

func allDigits(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return value != ""
}
