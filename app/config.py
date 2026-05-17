"""路径、环境变量加载、全局配置项与基础校验工具。

所有可在运行时被改写的全局变量都集中在本模块；其它模块通过
``from app import config; config.XXX`` 引用，避免 ``from app.config import XXX``
固定为模块加载瞬间的旧值。
"""

import os
import re
import sys
import uuid
from threading import Lock
from fastapi import HTTPException

# --- 路径常量 ---

def _resolve_base_dir():
    env_base_dir = os.environ.get("INFINITE_CANVAS_BASE_DIR")
    if env_base_dir:
        return os.path.abspath(env_base_dir)
    if getattr(sys, "frozen", False) or "__compiled__" in globals():
        exe_dir = os.path.dirname(os.path.abspath(sys.argv[0]))
        if os.path.basename(exe_dir).lower() == "runtime":
            return os.path.dirname(exe_dir)
        return exe_dir
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


BASE_DIR = _resolve_base_dir()
WORKFLOW_DIR = os.path.join(BASE_DIR, "workflows")
WORKFLOW_PATH = os.path.join(WORKFLOW_DIR, "Z-Image.json")
STATIC_DIR = os.path.join(BASE_DIR, "static")
OUTPUT_DIR = os.path.join(BASE_DIR, "output")
ASSETS_DIR = os.path.join(BASE_DIR, "assets")
OUTPUT_INPUT_DIR = os.path.join(ASSETS_DIR, "input")
OUTPUT_OUTPUT_DIR = os.path.join(ASSETS_DIR, "output")
HISTORY_FILE = os.path.join(BASE_DIR, "history.json")
API_ENV_FILE = os.path.join(BASE_DIR, "API", ".env")
DATA_DIR = os.path.join(BASE_DIR, "data")
CONVERSATION_DIR = os.path.join(DATA_DIR, "conversations")
CANVAS_DIR = os.path.join(DATA_DIR, "canvases")
API_PROVIDERS_FILE = os.path.join(DATA_DIR, "api_providers.json")
GLOBAL_CONFIG_FILE = os.path.join(BASE_DIR, "global_config.json")
CANVAS_TRASH_RETENTION_MS = 30 * 24 * 60 * 60 * 1000

# 进程级唯一 ID（提交 ComfyUI 任务时用作 client_id）
CLIENT_ID = str(uuid.uuid4())

# --- 锁 ---

QUEUE_LOCK = Lock()
HISTORY_LOCK = Lock()
GLOBAL_CONFIG_LOCK = Lock()
CONVERSATION_LOCK = Lock()
CANVAS_LOCK = Lock()
LOAD_LOCK = Lock()

# --- 校验 ---

PROVIDER_ID_RE = re.compile(r"^[a-zA-Z0-9_-]{2,40}$")

FIELD_LABELS = {
    "prompt": "提示词",
    "message": "文本",
    "system_prompt": "系统提示词",
}


def friendly_validation_error(errors):
    parts = []
    for err in errors or []:
        loc = [str(item) for item in err.get("loc", []) if item != "body"]
        field = loc[-1] if loc else ""
        label = FIELD_LABELS.get(field, field or "请求参数")
        ctx = err.get("ctx") or {}
        limit = ctx.get("limit_value") or ctx.get("max_length") or ctx.get("min_length")
        err_type = str(err.get("type") or "")
        msg = str(err.get("msg") or "")
        if "max_length" in err_type or "at most" in msg:
            parts.append(
                f"{label}过长：当前内容超过后端上限 {limit} 个字符。请拆分为多个提示词节点，或先用 LLM 节点压缩后再生成。"
            )
        elif "min_length" in err_type:
            parts.append(f"{label}不能为空。")
        else:
            parts.append(f"{label}格式不正确：{msg}")
    return "\n".join(parts) or "请求参数不正确。"


def selected_model(requested, fallback):
    """校验并返回模型名；用于阻止注入与防止字符越界。"""
    model = (requested or fallback).strip()
    if not model:
        raise HTTPException(status_code=400, detail="模型名称不能为空")
    if len(model) > 120 or not re.fullmatch(r"[a-zA-Z0-9_.:/+-]+", model):
        raise HTTPException(status_code=400, detail=f"模型名称不合法：{model}")
    return model


def modelscope_size(value, fallback="1024x1024"):
    size = str(value or fallback).strip().lower().replace("*", "x")
    if re.fullmatch(r"\d{2,5}x\d{2,5}", size):
        return size
    raise HTTPException(
        status_code=400,
        detail=f"ModelScope size 格式不正确：{value or fallback}，应为 WxH，例如 1024x1024",
    )


# --- .env 加载 ---

def load_env_file():
    if not os.path.exists(API_ENV_FILE):
        return
    try:
        with open(API_ENV_FILE, "r", encoding="utf-8-sig") as f:
            for raw_line in f.read().splitlines():
                line = raw_line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, value = line.split("=", 1)
                key = key.strip()
                value = value.strip().strip('"').strip("'")
                os.environ.setdefault(key, value)
    except Exception as e:
        print(f"加载 API/.env 失败: {e}")


load_env_file()

# --- 运行时配置（来自 env，可被 reload_env_globals 刷新） ---

COMFYUI_INSTANCES = [s.strip() for s in os.getenv("COMFYUI_INSTANCES", "127.0.0.1:8188").split(",") if s.strip()]
COMFYUI_ADDRESS = COMFYUI_INSTANCES[0]

AI_BASE_URL = os.getenv("COMFLY_BASE_URL", "https://ai.comfly.chat").rstrip("/")
AI_API_KEY = os.getenv("COMFLY_API_KEY", "")
MODELSCOPE_API_KEY = os.getenv("MODELSCOPE_API_KEY", "")
MODELSCOPE_CHAT_BASE_URL = "https://api-inference.modelscope.cn/v1"
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "")
OPENAI_API_BASE_URL = os.getenv("OPENAI_API_BASE_URL", "https://api.openai.com/v1").rstrip("/")
VOLCENGINE_ARK_API_KEY = os.getenv("VOLCENGINE_ARK_API_KEY", "")
VOLCENGINE_ARK_BASE_URL = os.getenv("VOLCENGINE_ARK_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3").rstrip("/")

SEEDREAM_MODELS = {
    "seedream-4.0": "doubao-seedream-4-0-250828",
    "seedream-4.5": "doubao-seedream-4-5-251128",
    "seedream-5.0": "doubao-seedream-5-0-260128",
}
SEEDANCE_MODELS = {
    "seedance-1.5-pro": "doubao-seedance-1-5-pro-251215",
    "seedance-2.0": "doubao-seedance-2-0-260128",
    "seedance-2.0-fast": "doubao-seedance-2-0-fast-260128",
}

MODELSCOPE_DEFAULT_IMAGE_MODELS = [
    "Tongyi-MAI/Z-Image-Turbo",
    "Qwen/Qwen-Image-2512",
    "Qwen/Qwen-Image-Edit-2511",
    "black-forest-labs/FLUX.2-klein-9B",
]
MODELSCOPE_DEFAULT_CHAT_MODELS = [
    "Qwen/Qwen3-235B-A22B",
    "Qwen/Qwen3-VL-235B-A22B-Instruct",
    "MiniMax/MiniMax-M2.7:MiniMax",
]
_MODELSCOPE_CONFIGURED_CHAT_MODELS = [m.strip() for m in os.getenv("MODELSCOPE_CHAT_MODELS", "").split(",") if m.strip()]
MODELSCOPE_CHAT_MODELS = list(dict.fromkeys([m for m in [*MODELSCOPE_DEFAULT_CHAT_MODELS, *_MODELSCOPE_CONFIGURED_CHAT_MODELS] if m]))
MODELSCOPE_DEFAULT_IMAGE_MODEL = MODELSCOPE_DEFAULT_IMAGE_MODELS[0]
MODELSCOPE_DEFAULT_CHAT_MODEL = "Qwen/Qwen3-235B-A22B"

MODELSCOPE_DEFAULT_LORAS = [
    {
        "id": "Daniel8152/film",
        "name": "Z-Image Film",
        "target_model": "Tongyi-MAI/Z-Image-Turbo",
        "strength": 0.8,
        "enabled": True,
        "note": "",
    },
    {
        "id": "Daniel8152/Qwen-Image-2512-Film",
        "name": "Qwen Image 2512 Film",
        "target_model": "Qwen/Qwen-Image-2512",
        "strength": 0.8,
        "enabled": True,
        "note": "",
    },
    {
        "id": "Daniel8152/Klein-enhance",
        "name": "Klein enhance",
        "target_model": "black-forest-labs/FLUX.2-klein-9B",
        "strength": 0.8,
        "enabled": True,
        "note": "",
    },
]
MODELSCOPE_DEFAULTS_VERSION = 3

CHAT_MODEL = os.getenv("CHAT_MODEL", "gpt-4o-mini")
IMAGE_MODEL = os.getenv("IMAGE_MODEL", "gpt-image-2")
SYSTEM_PROMPT = os.getenv("SYSTEM_PROMPT", "You are a helpful assistant.")
MAX_HISTORY_MESSAGES = int(os.getenv("MAX_HISTORY_MESSAGES", "30"))
AI_REQUEST_TIMEOUT = float(os.getenv("REQUEST_TIMEOUT", "120"))
IMAGE_POLL_INTERVAL = float(os.getenv("IMAGE_POLL_INTERVAL", "2"))
IMAGE_TASK_TIMEOUT = float(os.getenv("IMAGE_TASK_TIMEOUT", str(AI_REQUEST_TIMEOUT)))
COMFYUI_HISTORY_TIMEOUT = int(float(os.getenv("COMFYUI_HISTORY_TIMEOUT", "1800")))
APIMART_IMAGE_TASK_TIMEOUT = float(os.getenv("APIMART_IMAGE_TASK_TIMEOUT", "1800"))
APIMART_IMAGE_POLL_INTERVAL = float(os.getenv("APIMART_IMAGE_POLL_INTERVAL", "5"))
APIMART_IMAGE_INITIAL_POLL_DELAY = float(os.getenv("APIMART_IMAGE_INITIAL_POLL_DELAY", "10"))
VIDEO_POLL_TIMEOUT = float(os.getenv("VIDEO_POLL_TIMEOUT", "1800"))
ONLINE_IMAGE_PROMPT_MAX_LENGTH = int(os.getenv("ONLINE_IMAGE_PROMPT_MAX_LENGTH", "20000"))
VIDEO_PROMPT_MAX_LENGTH = int(os.getenv("VIDEO_PROMPT_MAX_LENGTH", "4000"))
LLM_MESSAGE_MAX_LENGTH = int(os.getenv("LLM_MESSAGE_MAX_LENGTH", "20000"))


def _model_list(env_name, primary, defaults):
    configured = os.getenv(env_name, "")
    configured_values = [item.strip() for item in configured.split(",") if item.strip()]
    values = configured_values or [primary, *defaults]
    deduped = []
    for value in values:
        if value and value not in deduped:
            deduped.append(value)
    return deduped


CHAT_MODELS = _model_list("CHAT_MODELS", CHAT_MODEL, ["gpt-4o-mini", "gemini-3.1-flash-image-preview-2k"])
IMAGE_MODELS = _model_list("IMAGE_MODELS", IMAGE_MODEL, ["nano-banana-pro"])
VIDEO_MODELS = _model_list("VIDEO_MODELS", "veo3-fast", [
    "veo2", "veo2-fast", "veo2-pro",
    "veo3", "veo3-fast", "veo3-pro",
    "veo3.1", "veo3.1-fast", "veo3.1-pro",
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
])


def reload_env_globals():
    """保存 API 设置后，把 os.environ 里的最新值同步回模块全局变量，
    避免保存后需要重启才能生效。"""
    global MODELSCOPE_API_KEY, AI_API_KEY, AI_BASE_URL, OPENAI_API_KEY, OPENAI_API_BASE_URL, VOLCENGINE_ARK_API_KEY, VOLCENGINE_ARK_BASE_URL
    global IMAGE_MODELS, CHAT_MODELS, VIDEO_MODELS, MODELSCOPE_CHAT_MODELS
    MODELSCOPE_API_KEY = os.getenv("MODELSCOPE_API_KEY", "")
    AI_API_KEY = os.getenv("COMFLY_API_KEY", "")
    AI_BASE_URL = os.getenv("COMFLY_BASE_URL", "https://ai.comfly.chat").rstrip("/")
    OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "")
    OPENAI_API_BASE_URL = os.getenv("OPENAI_API_BASE_URL", "https://api.openai.com/v1").rstrip("/")
    VOLCENGINE_ARK_API_KEY = os.getenv("VOLCENGINE_ARK_API_KEY", "")
    VOLCENGINE_ARK_BASE_URL = os.getenv("VOLCENGINE_ARK_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3").rstrip("/")
    IMAGE_MODELS = _model_list("IMAGE_MODELS", os.getenv("IMAGE_MODEL", IMAGE_MODEL), ["nano-banana-pro"])
    CHAT_MODELS = _model_list("CHAT_MODELS", os.getenv("CHAT_MODEL", CHAT_MODEL), ["gpt-4o-mini", "gemini-3.1-flash-image-preview-2k"])
    VIDEO_MODELS = _model_list("VIDEO_MODELS", "veo3-fast", [
        "veo2", "veo2-fast", "veo2-pro",
        "veo3", "veo3-fast", "veo3-pro",
        "veo3.1", "veo3.1-fast", "veo3.1-pro",
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
    ])
    _configured = [m.strip() for m in os.getenv("MODELSCOPE_CHAT_MODELS", "").split(",") if m.strip()]
    MODELSCOPE_CHAT_MODELS = list(dict.fromkeys([m for m in [*MODELSCOPE_DEFAULT_CHAT_MODELS, *_configured] if m]))


# --- 启动期目录创建 ---

for _d in (OUTPUT_DIR, ASSETS_DIR, OUTPUT_INPUT_DIR, OUTPUT_OUTPUT_DIR, STATIC_DIR,
           WORKFLOW_DIR, CONVERSATION_DIR, CANVAS_DIR):
    os.makedirs(_d, exist_ok=True)
