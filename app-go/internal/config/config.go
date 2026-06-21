package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const CanvasTrashRetentionMS int64 = 30 * 24 * 60 * 60 * 1000

type Config struct {
	Host string
	Port string

	RepoRoot string
	BaseDir  string

	StaticDir        string
	WorkflowDir      string
	OutputDir        string
	AssetsDir        string
	OutputInputDir   string
	OutputOutputDir  string
	MediaPreviewDir  string
	DataDir          string
	ConversationDir  string
	CanvasDir        string
	APIEnvFile       string
	APIProvidersFile string
	GlobalConfigFile string
	HistoryFile      string

	AIBaseURL                 string
	AIAPIKey                  string
	ModelScopeAPIKey          string
	ModelScopeChatBaseURL     string
	OpenAIAPIKey              string
	OpenAIAPIBaseURL          string
	VolcengineArkAPIKey       string
	VolcengineArkBaseURL      string
	VolcengineAccessKeyID     string
	VolcengineSecretAccessKey string
	VolcengineProjectName     string
	VolcengineRegion          string

	ChatModel   string
	ImageModel  string
	ChatModels  []string
	ImageModels []string
	VideoModels []string

	ModelScopeChatModels []string
	ComfyUIInstances     []string
	MaxHistoryMessages   int
	AIRequestTimeoutSec  int
	ImageTaskTimeoutSec  int
}

func Load() (*Config, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		repoRoot, err = executableDir()
		if err != nil {
			return nil, err
		}
	}
	baseDir := getenv("INFINITE_CANVAS_BASE_DIR", repoRoot)
	workflowDir := filepath.Join(baseDir, "workflows")
	if baseDir == repoRoot && !pathExists(workflowDir) {
		workflowDir = filepath.Join(repoRoot, "app-go", "web", "workflows")
	}

	cfg := &Config{
		Host: getenv("INFINITE_CANVAS_GO_HOST", "0.0.0.0"),
		Port: getenv("INFINITE_CANVAS_GO_PORT", "8080"),

		RepoRoot: repoRoot,
		BaseDir:  baseDir,

		StaticDir:        filepath.Join(baseDir, "static"),
		WorkflowDir:      workflowDir,
		OutputDir:        filepath.Join(baseDir, "output"),
		AssetsDir:        filepath.Join(baseDir, "assets"),
		OutputInputDir:   filepath.Join(baseDir, "assets", "input"),
		OutputOutputDir:  filepath.Join(baseDir, "assets", "output"),
		DataDir:          filepath.Join(baseDir, "data"),
		MediaPreviewDir:  filepath.Join(baseDir, "data", "media_previews"),
		ConversationDir:  filepath.Join(baseDir, "data", "conversations"),
		CanvasDir:        filepath.Join(baseDir, "data", "canvases"),
		APIEnvFile:       filepath.Join(baseDir, "API", ".env"),
		APIProvidersFile: filepath.Join(baseDir, "data", "api_providers.json"),
		GlobalConfigFile: filepath.Join(baseDir, "global_config.json"),
		HistoryFile:      filepath.Join(baseDir, "history.json"),

		ModelScopeChatBaseURL: "https://api-inference.modelscope.cn/v1",
	}

	if err := loadEnvFile(cfg.APIEnvFile); err != nil {
		return nil, err
	}

	cfg.AIBaseURL = strings.TrimRight(getenv("COMFLY_BASE_URL", "https://ai.comfly.chat"), "/")
	cfg.RefreshFromEnv()

	for _, path := range []string{
		cfg.OutputDir,
		cfg.AssetsDir,
		filepath.Join(cfg.AssetsDir, "cache", "volcengine_assets"),
		filepath.Join(cfg.AssetsDir, "cache", "volc_presets"),
		cfg.OutputInputDir,
		cfg.OutputOutputDir,
		cfg.DataDir,
		cfg.MediaPreviewDir,
		cfg.ConversationDir,
		cfg.CanvasDir,
		filepath.Dir(cfg.APIEnvFile),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func (c *Config) RefreshFromEnv() {
	c.AIBaseURL = strings.TrimRight(getenv("COMFLY_BASE_URL", "https://ai.comfly.chat"), "/")
	c.AIAPIKey = os.Getenv("COMFLY_API_KEY")
	c.ModelScopeAPIKey = os.Getenv("MODELSCOPE_API_KEY")
	c.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	c.OpenAIAPIBaseURL = strings.TrimRight(getenv("OPENAI_API_BASE_URL", "https://api.openai.com/v1"), "/")
	c.VolcengineArkAPIKey = os.Getenv("VOLCENGINE_ARK_API_KEY")
	c.VolcengineArkBaseURL = strings.TrimRight(os.Getenv("VOLCENGINE_ARK_BASE_URL"), "/")
	c.VolcengineAccessKeyID = os.Getenv("VOLCENGINE_ACCESS_KEY_ID")
	c.VolcengineSecretAccessKey = os.Getenv("VOLCENGINE_SECRET_ACCESS_KEY")
	c.VolcengineProjectName = nonEmpty(strings.TrimSpace(os.Getenv("VOLCENGINE_PROJECT_NAME")), "default")
	c.VolcengineRegion = nonEmpty(strings.TrimSpace(os.Getenv("VOLCENGINE_REGION")), "cn-beijing")

	c.ChatModel = getenv("CHAT_MODEL", "gpt-4o-mini")
	c.ImageModel = getenv("IMAGE_MODEL", "gpt-image-2")
	c.ChatModels = modelList("CHAT_MODELS", c.ChatModel, []string{"gpt-4o-mini", "gemini-3.1-flash-image-preview-2k"})
	c.ImageModels = modelList("IMAGE_MODELS", c.ImageModel, []string{"nano-banana-pro"})
	c.VideoModels = modelList("VIDEO_MODELS", "veo3-fast", defaultVideoModels())
	c.ModelScopeChatModels = modelScopeChatModels()
	c.ComfyUIInstances = splitCSV(getenv("COMFYUI_INSTANCES", "127.0.0.1:8188"))
	c.MaxHistoryMessages = envInt("MAX_HISTORY_MESSAGES", 30)
	c.AIRequestTimeoutSec = envInt("REQUEST_TIMEOUT", 120)
	c.ImageTaskTimeoutSec = envInt("IMAGE_TASK_TIMEOUT", c.AIRequestTimeoutSec)
}

func (c *Config) Addr() string {
	return c.Host + ":" + c.Port
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func nonEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func modelList(envName, primary string, defaults []string) []string {
	if configured := splitCSV(os.Getenv(envName)); len(configured) > 0 {
		return dedupe(configured)
	}
	values := []string{primary}
	values = append(values, defaults...)
	return dedupe(values)
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func modelScopeChatModels() []string {
	defaults := []string{
		"Qwen/Qwen3-235B-A22B",
		"Qwen/Qwen3-VL-235B-A22B-Instruct",
		"MiniMax/MiniMax-M2.7:MiniMax",
	}
	return dedupe(append(defaults, splitCSV(os.Getenv("MODELSCOPE_CHAT_MODELS"))...))
}

func defaultVideoModels() []string {
	return []string{
		"veo2", "veo2-fast", "veo2-pro",
		"veo3", "veo3-fast", "veo3-pro",
		"veo3.1", "veo3.1-fast", "veo3.1-quality", "veo3.1-lite",
		"sora-2", "sora-2-pro",
		"wan2.6-t2v", "wan2.6-i2v",
		"wan2.5-t2v-preview", "wan2.5-i2v-preview",
		"wan2.2-t2v-plus", "wan2.2-i2v-plus", "wan2.2-i2v-flash",
		"doubao-seedance-2-0-260128",
		"doubao-seedance-2-0-fast-260128",
		"doubao-seedance-1-5-pro-251215",
		"doubao-seedance-1-0-pro-250528",
		"doubao-seedance-1-0-lite-t2v-250428",
		"doubao-seedance-1-0-lite-i2v-250428",
	}
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
