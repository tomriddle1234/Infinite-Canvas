"""API 平台配置：规范化、加载、保存，以及构造请求头/解析协议。"""

import json
import os
import re

from fastapi import HTTPException

from . import config


# --- env / 显示 ---

def provider_key_env(provider_id: str) -> str:
    if provider_id == "comfly":
        return "COMFLY_API_KEY"
    if provider_id == "modelscope":
        return "MODELSCOPE_API_KEY"
    return f"API_PROVIDER_{re.sub(r'[^A-Za-z0-9]', '_', provider_id).upper()}_KEY"


def mask_secret(value: str) -> str:
    if not value:
        return ""
    tail = value[-4:] if len(value) > 4 else value
    return f"••••••••{tail}"


def env_quote(value) -> str:
    text = str(value or "")
    if not text or re.search(r"\s|#|['\"]", text):
        return '"' + text.replace("\\", "\\\\").replace('"', '\\"') + '"'
    return text


def update_env_values(updates: dict) -> None:
    """把 updates 写回 API/.env，同时同步到 os.environ。保留未被覆盖的旧行。"""
    os.makedirs(os.path.dirname(config.API_ENV_FILE), exist_ok=True)
    lines = []
    if os.path.exists(config.API_ENV_FILE):
        with open(config.API_ENV_FILE, "r", encoding="utf-8-sig") as f:
            lines = f.read().splitlines()
    seen = set()
    next_lines = []
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in line:
            next_lines.append(line)
            continue
        key = line.split("=", 1)[0].strip()
        if key in updates:
            next_lines.append(f"{key}={env_quote(updates[key])}")
            os.environ[key] = str(updates[key] or "")
            seen.add(key)
        else:
            next_lines.append(line)
    for key, value in updates.items():
        if key not in seen:
            next_lines.append(f"{key}={env_quote(value)}")
            os.environ[key] = str(value or "")
    with open(config.API_ENV_FILE, "w", encoding="utf-8") as f:
        f.write("\n".join(next_lines).rstrip() + "\n")


# --- 规范化 ---

def model_list_from_values(values) -> list:
    deduped = []
    for value in values or []:
        item = str(value or "").strip()
        if item and item not in deduped:
            config.selected_model(item, item)
            deduped.append(item)
    return deduped


def normalize_model_list(values):
    return model_list_from_values(values)


def normalize_ms_loras(values):
    normalized = []
    seen = set()
    for raw in values or []:
        if not isinstance(raw, dict):
            continue
        lora_id = str(raw.get("id") or "").strip()
        if not lora_id:
            continue
        target_model = str(raw.get("target_model") or raw.get("model") or "").strip()
        if not target_model:
            continue
        key = (target_model, lora_id)
        if key in seen:
            continue
        seen.add(key)
        try:
            strength = float(raw.get("strength", raw.get("default_strength", 0.8)))
        except Exception:
            strength = 0.8
        strength = max(0.0, min(2.0, strength))
        name = re.sub(r"\s+", " ", str(raw.get("name") or "").strip())[:80]
        normalized.append({
            "id": lora_id[:180],
            "name": name or lora_id,
            "target_model": target_model[:180],
            "strength": strength,
            "enabled": bool(raw.get("enabled", True)),
            "note": str(raw.get("note") or "").strip()[:300],
        })
    return normalized


def normalize_provider(item: dict) -> dict:
    provider_id = str(item.get("id") or "").strip().lower()
    if not config.PROVIDER_ID_RE.fullmatch(provider_id):
        raise HTTPException(status_code=400, detail=f"API 平台 ID 不合法：{provider_id or '(empty)'}")
    name = re.sub(r"\s+", " ", str(item.get("name") or provider_id).strip())[:60] or provider_id
    base_url = str(item.get("base_url") or "").strip().rstrip("/")
    if base_url and not re.match(r"^https?://", base_url):
        raise HTTPException(status_code=400, detail=f"{name} 的 Base URL 需要以 http:// 或 https:// 开头")
    protocol = str(item.get("protocol") or "openai").strip().lower()
    if protocol not in {"openai", "apimart"}:
        protocol = "openai"
    return {
        "id": provider_id,
        "name": name,
        "base_url": base_url,
        "protocol": protocol,
        "enabled": bool(item.get("enabled", True)),
        "primary": bool(item.get("primary", False)),
        "image_models": model_list_from_values(item.get("image_models") or []),
        "chat_models": model_list_from_values(item.get("chat_models") or []),
        "video_models": model_list_from_values(item.get("video_models") or []),
        "ms_loras": normalize_ms_loras(item.get("ms_loras") or []),
        "ms_defaults_version": int(item.get("ms_defaults_version") or 0),
    }


# --- 默认值 ---

def default_api_providers() -> list:
    # 只保留 ModelScope 为强制默认平台，其它平台均可自定义增删
    return [
        {
            "id": "modelscope",
            "name": "ModelScope",
            "base_url": config.MODELSCOPE_CHAT_BASE_URL,
            "enabled": True,
            "primary": False,
            "image_models": config.MODELSCOPE_DEFAULT_IMAGE_MODELS,
            "chat_models": config.MODELSCOPE_CHAT_MODELS,
            "video_models": [],
            "ms_loras": config.MODELSCOPE_DEFAULT_LORAS,
            "ms_defaults_version": config.MODELSCOPE_DEFAULTS_VERSION,
        },
    ]


def merge_default_api_providers(providers: list) -> list:
    merged = [dict(item) for item in providers]
    ms_default = next((d for d in default_api_providers() if d["id"] == "modelscope"), None)
    if ms_default:
        current = next((item for item in merged if item.get("id") == "modelscope"), None)
        if not current:
            merged.append(ms_default)
        else:
            if not current.get("base_url"):
                current["base_url"] = ms_default["base_url"]
            seeded_version = int(current.get("ms_defaults_version") or 0)
            if seeded_version < config.MODELSCOPE_DEFAULTS_VERSION:
                image_models = model_list_from_values([*config.MODELSCOPE_DEFAULT_IMAGE_MODELS, *(current.get("image_models") or [])])
                chat_models = model_list_from_values([*config.MODELSCOPE_DEFAULT_CHAT_MODELS, *(current.get("chat_models") or [])])
                loras = normalize_ms_loras([*config.MODELSCOPE_DEFAULT_LORAS, *(current.get("ms_loras") or [])])
                current["image_models"] = image_models
                current["chat_models"] = chat_models
                current["ms_loras"] = loras
                current["ms_defaults_version"] = config.MODELSCOPE_DEFAULTS_VERSION
    return merged


# --- 持久化 ---

def load_api_providers() -> list:
    defaults = default_api_providers()
    if not os.path.exists(config.API_PROVIDERS_FILE):
        return defaults
    try:
        with open(config.API_PROVIDERS_FILE, "r", encoding="utf-8") as f:
            raw = json.load(f)
        providers = [normalize_provider(item) for item in raw if isinstance(item, dict)]
        return merge_default_api_providers(providers or defaults)
    except Exception as e:
        print(f"加载 API 平台配置失败: {e}")
        return defaults


def save_api_providers(providers: list) -> None:
    os.makedirs(config.DATA_DIR, exist_ok=True)
    with config.GLOBAL_CONFIG_LOCK:
        with open(config.API_PROVIDERS_FILE, "w", encoding="utf-8") as f:
            json.dump(providers, f, ensure_ascii=False, indent=2)


def public_provider(provider: dict) -> dict:
    key = os.getenv(provider_key_env(provider["id"]), "")
    return {
        **provider,
        "has_key": bool(key),
        "key_preview": mask_secret(key),
        "key_env": provider_key_env(provider["id"]),
    }


def get_primary_provider_id(providers=None) -> str:
    """返回当前首选 provider 的 id；优先 primary=True，否则第一个非 modelscope，再次第一个。"""
    providers = providers if providers is not None else load_api_providers()
    primary = next((p for p in providers if p.get("primary") and p.get("enabled", True)), None)
    if primary:
        return primary["id"]
    non_ms = next((p for p in providers if p["id"] != "modelscope" and p.get("enabled", True)), None)
    if non_ms:
        return non_ms["id"]
    return providers[0]["id"] if providers else "modelscope"


def get_api_provider(provider_id: str = "comfly") -> dict:
    providers = load_api_providers()
    target = (provider_id or "").strip().lower()
    # 兼容旧的 "comfly" 硬编码：未指定或不存在时回退到首选 provider
    if not target or not any(p["id"] == target for p in providers):
        target = get_primary_provider_id(providers)
    provider = next((p for p in providers if p["id"] == target), None)
    if not provider:
        raise HTTPException(status_code=400, detail=f"未找到 API 平台：{target}")
    if not provider.get("enabled", True):
        raise HTTPException(status_code=400, detail=f"API 平台已禁用：{provider.get('name') or target}")
    return provider


def get_api_provider_exact(provider_id: str) -> dict:
    """严格按 id 查找 provider，找不到直接报错；不像 get_api_provider 会兜底到首选。

    用于 API 设置页"已保存平台"路径，避免静默拉错平台的模型清单。
    """
    providers = load_api_providers()
    target = (provider_id or "").strip().lower()
    provider = next((p for p in providers if p["id"] == target), None)
    if not provider:
        raise HTTPException(status_code=400, detail=f"未找到 API 平台：{target or '(empty)'}。新增平台未保存时请使用当前表单拉取模型。")
    if not provider.get("enabled", True):
        raise HTTPException(status_code=400, detail=f"API 平台已禁用：{provider.get('name') or target}")
    return provider


# --- 协议判断 / 请求头 ---

def provider_protocol(provider) -> str:
    return str((provider or {}).get("protocol") or "openai").strip().lower()


def is_apimart_provider(provider) -> bool:
    base_url = str((provider or {}).get("base_url") or "").lower()
    return provider_protocol(provider) == "apimart" or "apimart.ai" in base_url


def api_headers(json_body: bool = True, provider: dict = None) -> dict:
    if provider:
        key_env = provider_key_env(provider["id"])
        api_key = os.getenv(key_env, "")
        provider_name = provider.get("name") or provider["id"]
        if not api_key:
            raise HTTPException(status_code=400, detail=f"未配置 {provider_name} 的 API Key，请在 API 平台管理中填写。")
    else:
        api_key = config.AI_API_KEY
        if not api_key:
            raise HTTPException(status_code=400, detail="未配置 COMFLY_API_KEY，请在 API/.env 中填写。")
    headers = {"Accept": "application/json", "Authorization": f"Bearer {api_key}"}
    if json_body:
        headers["Content-Type"] = "application/json"
    return headers


def resolve_chat_provider(provider: str, model: str, ms_model: str):
    """返回 (chat_base_url, headers, model_name)。"""
    if provider == "modelscope":
        if not config.MODELSCOPE_API_KEY:
            raise HTTPException(status_code=400, detail="未配置 MODELSCOPE_API_KEY，请在 API/.env 中填写。")
        base = config.MODELSCOPE_CHAT_BASE_URL
        hdrs = {"Authorization": f"Bearer {config.MODELSCOPE_API_KEY}", "Content-Type": "application/json"}
        fallback = config.MODELSCOPE_CHAT_MODELS[0] if config.MODELSCOPE_CHAT_MODELS else "MiniMax/MiniMax-M2.7"
        mdl = config.selected_model(ms_model or model, fallback)
        return base, hdrs, mdl
    api_provider = get_api_provider(provider or "")
    base_root = (api_provider.get("base_url") or config.AI_BASE_URL).rstrip("/")
    if not base_root:
        raise HTTPException(status_code=400, detail=f"{api_provider.get('name') or api_provider['id']} 未配置 Base URL")
    base = base_root if base_root.endswith("/v1") else base_root + "/v1"
    hdrs = api_headers(provider=api_provider)
    default_model = (api_provider.get("chat_models") or [config.CHAT_MODEL])[0]
    mdl = config.selected_model(model, default_model)
    return base, hdrs, mdl
