"""API 平台与全局配置：/api/providers/*, /api/config*, /api/models。"""

import json
import os
import re
from typing import List

import httpx
from fastapi import APIRouter, HTTPException

from .. import config, providers
from ..models import ApiProviderPayload, TestConnectionPayload

router = APIRouter()


@router.get("/api/config")
async def ai_config():
    preferred_chat_model = next(
        (m for m in config.CHAT_MODELS if m == "gpt-5.5"),
        config.CHAT_MODELS[0] if config.CHAT_MODELS else config.CHAT_MODEL,
    )
    public = [providers.public_provider(p) for p in providers.load_api_providers()]
    return {
        "base_url": config.AI_BASE_URL,
        "chat_model": preferred_chat_model,
        "image_model": config.IMAGE_MODEL,
        "chat_models": config.CHAT_MODELS,
        "image_models": config.IMAGE_MODELS,
        "video_models": config.VIDEO_MODELS,
        "api_providers": public,
        "has_api_key": bool(config.AI_API_KEY),
        "ms_chat_models": config.MODELSCOPE_CHAT_MODELS,
        "has_ms_key": bool(config.MODELSCOPE_API_KEY),
    }


@router.get("/api/models")
async def ai_models():
    return {
        "chat_models": config.CHAT_MODELS,
        "image_models": config.IMAGE_MODELS,
        "video_models": config.VIDEO_MODELS,
    }


@router.get("/api/providers")
async def api_providers_get():
    return {"providers": [providers.public_provider(p) for p in providers.load_api_providers()]}


@router.put("/api/providers")
async def save_providers(payload: List[ApiProviderPayload]):
    normalized = []
    env_updates = {}
    raw_primary_flags = [bool(getattr(item, "primary", False)) for item in payload]
    for item in payload:
        provider = providers.normalize_provider(item.dict(exclude={"api_key"}))
        if any(existing["id"] == provider["id"] for existing in normalized):
            raise HTTPException(status_code=400, detail=f"API 平台 ID 重复：{provider['id']}")
        normalized.append(provider)
        if item.api_key is not None:
            env_updates[providers.provider_key_env(provider["id"])] = item.api_key.strip()
        if provider["id"] == "comfly":
            env_updates["COMFLY_BASE_URL"] = provider["base_url"]
            env_updates["IMAGE_MODELS"] = ",".join(provider["image_models"])
            env_updates["CHAT_MODELS"] = ",".join(provider["chat_models"])
            env_updates["VIDEO_MODELS"] = ",".join(provider.get("video_models") or [])
        if provider["id"] == "modelscope":
            env_updates["MODELSCOPE_CHAT_MODELS"] = ",".join(provider["chat_models"])
    if not normalized:
        raise HTTPException(status_code=400, detail="至少保留一个 API 平台")
    # 最多一个 primary：取最后被标记的；都没标记则保持原样不强制
    primary_indices = [i for i, flag in enumerate(raw_primary_flags) if flag]
    if primary_indices:
        winner = primary_indices[-1]
        for i, p in enumerate(normalized):
            p["primary"] = (i == winner)
    providers.save_api_providers(normalized)
    if env_updates:
        providers.update_env_values(env_updates)
        config.reload_env_globals()  # 立即把最新 env 同步回模块全局变量
    return {"providers": [providers.public_provider(p) for p in normalized]}


@router.get("/api/config/token")
async def get_global_token():
    """ModelScope token 从 env 读取（不再支持 UI 修改），兼容旧版 global_config.json。"""
    if config.MODELSCOPE_API_KEY:
        return {"token": config.MODELSCOPE_API_KEY}
    if os.path.exists(config.GLOBAL_CONFIG_FILE):
        try:
            with open(config.GLOBAL_CONFIG_FILE, "r", encoding="utf-8") as f:
                cfg = json.load(f)
                return {"token": cfg.get("modelscope_token", "")}
        except Exception:
            pass
    return {"token": ""}


def _classify_model_id(mid: str) -> str:
    lc = mid.lower()
    video_keys = ["veo", "sora", "wan2", "wanx", "doubao-seedance", "doubao-1", "kling", "hailuo", "video", "t2v-", "i2v-", "s2v"]
    if any(k in lc for k in video_keys):
        return "video"
    image_keys = ["image", "dalle", "dall-e", "imagen", "flux", "stable", "sdxl", "midjourney", "nano-banana", "ideogram", "fal-ai", "z-image", "qwen-image", "klein"]
    if any(k in lc for k in image_keys):
        return "image"
    return "chat"


def _extract_model_ids(raw) -> list:
    items = raw.get("data") if isinstance(raw, dict) else None
    if not items and isinstance(raw, dict):
        items = raw.get("models") or raw.get("list") or []
    if not isinstance(items, list):
        items = []
    ids = []
    for it in items:
        if isinstance(it, str):
            ids.append(it)
        elif isinstance(it, dict):
            mid = it.get("id") or it.get("name") or it.get("model")
            if mid:
                ids.append(str(mid))
    return sorted(set(ids))


@router.post("/api/providers/test-connection")
async def test_provider_connection(payload: TestConnectionPayload):
    """测试请求地址：调上游 /v1/models；通过则同时返回按类别分组的模型清单。"""
    base_url = (payload.base_url or "").strip().rstrip("/")
    if not base_url:
        raise HTTPException(status_code=400, detail="请先填写请求地址")
    if not re.match(r"^https?://", base_url):
        raise HTTPException(status_code=400, detail="请求地址必须以 http:// 或 https:// 开头")
    api_key = (payload.api_key or "").strip()
    if not api_key and payload.provider_id:
        api_key = os.getenv(providers.provider_key_env(payload.provider_id), "")
    if not api_key:
        raise HTTPException(status_code=400, detail="请先填写或保存 API Key")
    url = f"{base_url}/models" if base_url.endswith("/v1") else f"{base_url}/v1/models"
    try:
        async with httpx.AsyncClient(timeout=15) as client:
            resp = await client.get(url, headers={"Authorization": f"Bearer {api_key}", "Accept": "application/json"})
        if resp.status_code >= 400:
            return {"ok": False, "status": resp.status_code, "message": resp.text[:300]}
        data = resp.json() if resp.text else {}
        ids = _extract_model_ids(data)
        grouped = {"image": [], "chat": [], "video": []}
        for mid in ids:
            grouped[_classify_model_id(mid)].append(mid)
        return {
            "ok": True,
            "status": resp.status_code,
            "model_count": len(ids),
            "image_models": grouped["image"],
            "chat_models": grouped["chat"],
            "video_models": grouped["video"],
            "all": ids,
        }
    except httpx.HTTPError as e:
        return {"ok": False, "status": 0, "message": str(e)[:300]}


@router.post("/api/providers/probe-async")
async def probe_async_endpoint(payload: TestConnectionPayload):
    """验证异步协议：用假 task_id 请求 GET /v1/tasks/{fake_id}。

    400 "Invalid task ID" → 端点存在且 Key 有效；
    401/403 → Key 无效；
    404/连接失败 → 不支持异步端点。"""
    base_url = (payload.base_url or "").strip().rstrip("/")
    if not base_url:
        raise HTTPException(status_code=400, detail="请先填写请求地址")
    api_key = (payload.api_key or "").strip()
    if not api_key and payload.provider_id:
        api_key = os.getenv(providers.provider_key_env(payload.provider_id), "")
    if not api_key:
        raise HTTPException(status_code=400, detail="请先填写或保存 API Key")
    tasks_base = base_url if base_url.endswith("/v1") else f"{base_url}/v1"
    probe_url = f"{tasks_base}/tasks/healthcheck_probe_do_not_submit"
    try:
        async with httpx.AsyncClient(timeout=15) as client:
            resp = await client.get(probe_url, headers={"Authorization": f"Bearer {api_key}", "Accept": "application/json"})
        try:
            body = resp.json()
        except Exception:
            body = resp.text[:500]
        sc = resp.status_code
        err_msg = ""
        if isinstance(body, dict):
            err = body.get("error") or {}
            if isinstance(err, dict):
                err_msg = str(err.get("message") or "").lower()
            else:
                err_msg = str(err).lower()
        if sc == 400 and "invalid task id" in err_msg:
            return {"ok": True, "status_code": sc, "message": "异步任务端点可用，API Key 已通过认证", "raw": body}
        if sc in (401, 403):
            return {"ok": False, "status_code": sc, "message": "API Key 无效或无权限", "raw": body}
        if sc == 404:
            return {"ok": False, "status_code": sc, "message": "平台不支持 /v1/tasks/ 端点，可能不是 APIMart 异步协议", "raw": body}
        if 400 <= sc < 500:
            return {"ok": None, "status_code": sc, "message": f"端点返回 {sc}，请查看原始响应判断", "raw": body}
        if sc < 300:
            return {"ok": True, "status_code": sc, "message": f"端点返回 {sc}（意外成功）", "raw": body}
        return {"ok": False, "status_code": sc, "message": f"服务端错误 {sc}", "raw": body}
    except httpx.HTTPError as e:
        raise HTTPException(status_code=502, detail=str(e)[:300])


@router.get("/api/providers/{provider_id}/fetch-models")
async def fetch_upstream_models(provider_id: str):
    """从上游 OpenAI 兼容接口拉取 /v1/models 列表，按名称智能分类为 image/chat/video。"""
    provider = providers.get_api_provider(provider_id)
    base_url = (provider.get("base_url") or "").rstrip("/")
    if not base_url:
        raise HTTPException(status_code=400, detail=f"{provider.get('name') or provider_id} 未配置 Base URL")
    api_key = os.getenv(providers.provider_key_env(provider["id"]), "")
    if not api_key:
        raise HTTPException(status_code=400, detail=f"{provider.get('name') or provider_id} 未配置 API Key")
    url = f"{base_url}/models" if base_url.endswith("/v1") else f"{base_url}/v1/models"
    try:
        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.get(url, headers={"Authorization": f"Bearer {api_key}", "Accept": "application/json"})
            if resp.status_code >= 400:
                raise HTTPException(status_code=resp.status_code, detail=f"上游 /v1/models 失败：{resp.text[:300]}")
            raw = resp.json()
    except httpx.HTTPError as e:
        raise HTTPException(status_code=502, detail=f"请求上游模型列表失败：{e}")
    ids = _extract_model_ids(raw)
    grouped = {"image": [], "chat": [], "video": []}
    for mid in ids:
        grouped[_classify_model_id(mid)].append(mid)
    return {
        "total": len(ids),
        "image_models": grouped["image"],
        "chat_models": grouped["chat"],
        "video_models": grouped["video"],
        "all": ids,
    }
