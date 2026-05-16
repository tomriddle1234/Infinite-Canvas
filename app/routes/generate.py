"""图像 / 视频生成相关路由。

- /api/online-image      OpenAI 兼容 / APIMart 在线生图
- /api/canvas-video      视频生成（doubao / veo / sora ...）
- /api/angle/*           ModelScope 旧版「角度」流程（兼容旧前端）
- /generate              ModelScope Z-Image 云端生图
- /api/ms/generate       ModelScope 通用图片生成（含图生图）
- /api/generate          本地 ComfyUI 工作流执行
"""

import asyncio
import collections
import json
import logging
import os
import random
import re
import time
import urllib.error
import urllib.parse
import urllib.request

import httpx
import requests
from fastapi import APIRouter, HTTPException

from .. import comfyui, config, imageproc, providers, store, upstream, upstream_openai_image, upstream_volcengine, ws
from ..models import (
    CanvasVideoRequest,
    CloudGenRequest,
    CloudPollRequest,
    GenerateRequest,
    GptImage2Request,
    MsGenerateRequest,
    OnlineImageRequest,
    SeedanceRequest,
    SeedanceStatusRequest,
    SeedreamRequest,
)

log = logging.getLogger(__name__)

router = APIRouter()

SEEDANCE_CACHE_TTL_SECONDS = 30 * 60
SEEDANCE_CACHE_MAX_ENTRIES = 200
SEEDANCE_RESULT_CACHE: "collections.OrderedDict[tuple, tuple[float, dict]]" = collections.OrderedDict()
SEEDANCE_STATUS_LOCK = asyncio.Lock()


def _seedance_cache_get(key: tuple):
    entry = SEEDANCE_RESULT_CACHE.get(key)
    if not entry:
        return None
    timestamp, value = entry
    if time.time() - timestamp > SEEDANCE_CACHE_TTL_SECONDS:
        SEEDANCE_RESULT_CACHE.pop(key, None)
        return None
    SEEDANCE_RESULT_CACHE.move_to_end(key)
    return value


def _seedance_cache_put(key: tuple, value: dict) -> None:
    SEEDANCE_RESULT_CACHE[key] = (time.time(), value)
    SEEDANCE_RESULT_CACHE.move_to_end(key)
    while len(SEEDANCE_RESULT_CACHE) > SEEDANCE_CACHE_MAX_ENTRIES:
        SEEDANCE_RESULT_CACHE.popitem(last=False)


# ---------------- Dedicated nodes: GPT Image 2.0 / Seedream / Seedance ----------------

@router.post("/api/nodes/gpt-image-2")
async def node_gpt_image_2(payload: GptImage2Request):
    count = max(1, min(8, int(payload.count or 1)))
    refs = [ref.model_dump() for ref in payload.reference_images if ref.url]
    try:
        image_items, raw = await upstream_openai_image.generate_gpt_image_2(payload.prompt, payload.size, payload.quality, refs, count=count)
    except httpx.HTTPStatusError as exc:
        raise HTTPException(status_code=exc.response.status_code, detail=f"OpenAI 图片接口错误：{exc.response.text[:500]}") from exc
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=502, detail=f"请求 OpenAI 图片接口失败：{exc}") from exc
    images = [await imageproc.save_ai_image_to_output(item, prefix="gpt_image2_") for item in image_items]
    result = {
        "prompt": payload.prompt,
        "images": images,
        "timestamp": time.time(),
        "type": "gpt-image-2",
        "model": "gpt-image-2",
        "provider_id": "openai",
        "params": {"size": payload.size, "quality": payload.quality, "count": count, "reference_images": refs},
        "raw": raw,
    }
    store.save_to_history(result)
    if ws.GLOBAL_LOOP:
        asyncio.run_coroutine_threadsafe(ws.manager.broadcast_new_image(result), ws.GLOBAL_LOOP)
    return result


@router.post("/api/nodes/seedream")
async def node_seedream(payload: SeedreamRequest):
    count = max(1, min(8, int(payload.count or 1)))
    images = []
    raws = []
    model = config.SEEDREAM_MODELS.get(payload.model, payload.model)
    try:
        for index in range(count):
            generated, raw = await asyncio.to_thread(upstream_volcengine.generate_seedream_once, payload, index=index)
            for item in generated:
                images.append(await imageproc.save_ai_image_to_output(item, prefix="seedream_"))
            raws.append(raw)
    except HTTPException:
        raise
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"Seedream 调用失败：{exc}") from exc
    result = {
        "prompt": payload.prompt,
        "images": images,
        "timestamp": time.time(),
        "type": "seedream",
        "model": model,
        "provider_id": "volcengine-ark",
        "params": {"size": payload.size, "count": count, "seed": payload.seed, "watermark": payload.watermark},
        "raw": raws[0] if len(raws) == 1 else {"items": raws},
    }
    store.save_to_history(result)
    if ws.GLOBAL_LOOP:
        asyncio.run_coroutine_threadsafe(ws.manager.broadcast_new_image(result), ws.GLOBAL_LOOP)
    return result


@router.post("/api/nodes/seedance")
async def node_seedance(payload: SeedanceRequest):
    try:
        submitted = await asyncio.to_thread(upstream_volcengine.submit_seedance, payload)
    except HTTPException:
        raise
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"Seedance 提交失败：{exc}") from exc
    log.info("Seedance submitted: task_ids=%s model=%s", submitted["task_ids"], submitted["model"])
    return {
        "status": "submitted",
        "task_ids": submitted["task_ids"],
        "model": submitted["model"],
        "raw": submitted["raw"],
        "submitted_at": submitted["submitted_at"],
    }


@router.post("/api/nodes/seedance/status")
async def node_seedance_status(payload: SeedanceStatusRequest):
    if not payload.task_ids:
        raise HTTPException(status_code=400, detail="缺少 Seedance task_ids")
    cache_key = tuple(item for item in payload.task_ids if item)
    async with SEEDANCE_STATUS_LOCK:
        cached = _seedance_cache_get(cache_key)
        if cached is not None:
            return cached
        try:
            result = await asyncio.to_thread(upstream_volcengine.poll_seedance, payload.task_ids)
        except HTTPException:
            raise
        except Exception as exc:
            raise HTTPException(status_code=502, detail=f"Seedance 查询失败：{exc}") from exc
        local_videos = []
        if result.get("status") == "succeeded":
            local_videos = [await imageproc.save_remote_video_to_output(url, prefix="seedance_") for url in result.get("videos", []) if url]
            record = {
                "prompt": "",
                "videos": local_videos,
                "outputs": local_videos,
                "timestamp": time.time(),
                "type": "seedance",
                "task_ids": payload.task_ids,
                "raw": result,
            }
            store.save_to_history(record)
        response = {**result, "videos": local_videos or result.get("videos", [])}
        if response.get("status") == "succeeded":
            _seedance_cache_put(cache_key, response)
        log.info("Seedance status: task_ids=%s status=%s videos=%d", list(cache_key), response.get("status"), len(response.get("videos") or []))
        return response


# ---------------- 在线生图 ----------------

@router.post("/api/online-image")
async def online_image(payload: OnlineImageRequest):
    provider = providers.get_api_provider(payload.provider_id)
    default_model = (provider.get("image_models") or [config.IMAGE_MODEL])[0]
    model = config.selected_model(payload.model, default_model)
    refs = [ref.model_dump() for ref in payload.reference_images if ref.url]
    try:
        image_data, raw = await upstream.generate_ai_image(payload.prompt, payload.size, payload.quality, model, refs, provider["id"])
        local_url = await imageproc.save_ai_image_to_output(image_data, prefix="online_")
    except httpx.HTTPStatusError as exc:
        text = exc.response.text or ""
        friendly = None
        m = re.search(r"longest edge must be less than or equal to (\d+)", text)
        if m:
            limit = m.group(1)
            friendly = f"该模型不支持当前分辨率：最长边超过 {limit}px。请把图片分辨率调低（例如换到 2K 或更小），或更换支持高分辨率的模型。"
        elif "Invalid size" in text or "invalid_value" in text:
            friendly = f"该模型不支持当前尺寸：{payload.size}。请尝试更换分辨率或模型。"
        elif "rate limit" in text.lower() or "429" in text:
            friendly = "请求过于频繁，已被上游限流，请稍后再试。"
        elif "Unauthorized" in text or "401" in text:
            friendly = "API Key 无效或已过期，请到「API 设置」检查 Key。"
        elif "model_not_found" in text or "channel not found" in text:
            friendly = f"上游平台找不到模型「{model}」可用通道。可能该模型未在此账号开通，请换一个已开通的模型。"
        detail = friendly or f"上游生图接口错误：{text[:300]}"
        raise HTTPException(status_code=exc.response.status_code, detail=detail) from exc
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=502, detail=f"请求上游生图接口失败：{exc}") from exc

    result = {
        "prompt": payload.prompt,
        "images": [local_url],
        "timestamp": time.time(),
        "type": "online",
        "model": model,
        "provider_id": provider["id"],
        "provider_name": provider.get("name") or provider["id"],
        "task_id": upstream.extract_task_id(raw) if isinstance(raw, dict) else None,
        "request_id": raw.get("id") if isinstance(raw, dict) else None,
        "params": {"provider_id": provider["id"], "model": model, "size": payload.size, "quality": payload.quality, "reference_images": refs},
        "raw_usage": raw.get("usage") if isinstance(raw, dict) else None,
    }
    store.save_to_history(result)
    if ws.GLOBAL_LOOP:
        asyncio.run_coroutine_threadsafe(ws.manager.broadcast_new_image(result), ws.GLOBAL_LOOP)
    return result


# ---------------- 视频生成 ----------------

@router.post("/api/canvas-video")
async def canvas_video(payload: CanvasVideoRequest):
    provider = providers.get_api_provider(payload.provider_id)
    base_url = upstream.video_api_root(provider)
    if not base_url:
        raise HTTPException(status_code=400, detail=f"{provider.get('name') or provider['id']} 未配置 Base URL")
    api_key = os.getenv(providers.provider_key_env(provider["id"]), "")
    if not api_key:
        raise HTTPException(status_code=400, detail=f"未配置 {provider.get('name') or provider['id']} 的 API Key，请在 API 设置中填写。")
    is_apimart = providers.is_apimart_provider(provider)
    submit_url = (
        f"{base_url}/videos/generations" if is_apimart and base_url.endswith("/v1")
        else f"{base_url}/v1/videos/generations" if is_apimart
        else f"{base_url}/v2/videos/generations"
    )
    body = {}
    try:
        async with httpx.AsyncClient(timeout=config.VIDEO_POLL_TIMEOUT) as client:
            # 图片载荷
            if is_apimart:
                # APIMart 只接受 http/https 或 asset:// URL，先上传本地图片取回网络 URL
                image_with_roles = []
                for ref in payload.images[:9]:
                    if not ref.url:
                        continue
                    role = str(ref.role or "").strip()
                    if role in {"first_frame", "last_frame", "reference_image"}:
                        up_url = await upstream.upload_image_for_apimart(client, provider, ref.url)
                        if up_url:
                            image_with_roles.append({"url": up_url, "role": role})
                image_payload = []
                if not image_with_roles:
                    for ref in payload.images[:9]:
                        if not ref.url:
                            continue
                        up_url = await upstream.upload_image_for_apimart(client, provider, ref.url)
                        if up_url:
                            image_payload.append(up_url)
                body = {
                    "prompt": payload.prompt,
                    "model": config.selected_model(payload.model, "doubao-seedance-2.0"),
                    "duration": payload.duration,
                    "size": upstream.apimart_video_size(payload.aspect_ratio or payload.size),
                    "resolution": payload.resolution or "480p",
                }
                if image_with_roles:
                    body["image_with_roles"] = image_with_roles
                elif image_payload:
                    body["image_urls"] = image_payload[:9]
                if payload.videos:
                    body["video_urls"] = [v for v in payload.videos if v][:3]
                if payload.seed is not None:
                    body["seed"] = payload.seed
                if payload.return_last_frame:
                    body["return_last_frame"] = True
                if payload.generate_audio:
                    body["generate_audio"] = True
            else:
                # 非 APIMart：data URL 方式
                image_payload = []
                for ref in payload.images[:4]:
                    if ref.url:
                        image_payload.append(imageproc.reference_to_data_url(ref.model_dump(), max_size=1536))
                body = {
                    "prompt": payload.prompt,
                    "model": config.selected_model(payload.model, "veo3-fast"),
                    "duration": payload.duration,
                    "watermark": payload.watermark,
                }
                if payload.aspect_ratio:
                    body["aspect_ratio"] = payload.aspect_ratio
                    body["ratio"] = payload.aspect_ratio
                if payload.size:
                    body["size"] = payload.size
                if payload.resolution:
                    body["resolution"] = payload.resolution
                if image_payload:
                    body["images"] = image_payload
                if payload.videos:
                    body["videos"] = [v for v in payload.videos if v]
                if payload.enhance_prompt:
                    body["enhance_prompt"] = True
                if payload.enable_upsample:
                    body["enable_upsample"] = True
                if payload.seed is not None:
                    body["seed"] = payload.seed
                if payload.camerafixed:
                    body["camerafixed"] = True
                if payload.return_last_frame:
                    body["return_last_frame"] = True
                if payload.generate_audio:
                    body["generate_audio"] = True

            response = await client.post(submit_url, headers=providers.api_headers(provider=provider), json=body)
            response.raise_for_status()
            try:
                raw = response.json()
            except Exception:
                resp_text = response.text[:500]
                raise HTTPException(status_code=502, detail=f"上游视频接口返回非 JSON 响应（状态 {response.status_code}）：{resp_text}")
            task_id = upstream.extract_task_id(raw) or raw.get("task_id") or raw.get("id")
            result = raw
            if task_id and not upstream.video_output_urls(raw):
                result = await upstream.wait_for_video_task(client, provider, task_id)
            urls = upstream.video_output_urls(result)
            if not urls:
                raise HTTPException(status_code=502, detail=f"视频生成成功但没有返回视频：{result}")
            local_urls = [await imageproc.save_remote_video_to_output(url) for url in urls]
            return {"videos": local_urls, "task_id": task_id, "raw": result}
    except httpx.HTTPStatusError as exc:
        text = exc.response.text
        requested_model = (body.get("model") if isinstance(body, dict) else "") or payload.model or ""
        provider_name = provider.get("name") or provider["id"]
        valid_models_match = re.search(r"not in\s*\[([^\]]+)\]", text)
        if valid_models_match:
            valid_models = [m.strip() for m in valid_models_match.group(1).split(",") if m.strip()]
            sample = valid_models[:30]
            more = f"（共 {len(valid_models)} 个，仅显示前 {len(sample)} 个）" if len(valid_models) > len(sample) else ""
            hint = (
                f"上游「{provider_name}」不识别模型「{requested_model}」。\n\n"
                f"上游支持的视频模型清单{more}：\n  {', '.join(sample)}\n\n"
                f"请到「API 设置」里把视频模型改成上面列表中的一个。"
            )
            raise HTTPException(status_code=exc.response.status_code, detail=hint) from exc
        if "channel not found" in text or "model_not_found" in text:
            hint = (
                f"上游「{provider_name}」识别了模型「{requested_model}」，但你的 API Key 账号下**没有该模型的可用通道**。\n\n"
                f"原因：你的账号没开通这个模型的访问权限（付费/订阅相关）。\n\n"
                f"解决方法：\n"
                f"  1. 登录 {provider.get('base_url') or '上游平台'} 控制台，开通该模型 / 充值；\n"
                f"  2. 或在「API 设置」里把视频模型改成你账号已开通的型号（如 veo3-fast / veo2-fast / sora-2 等）。"
            )
            raise HTTPException(status_code=exc.response.status_code, detail=hint) from exc
        raise HTTPException(status_code=exc.response.status_code, detail=f"上游视频接口错误：{text}") from exc
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=502, detail=f"请求上游视频接口失败：{exc}") from exc


# ---------------- ModelScope 旧版「角度」流程 ----------------

@router.post("/api/angle/poll_status")
async def poll_angle_cloud(req: CloudPollRequest):
    base_url = "https://api-inference.modelscope.cn/"
    clean_token = (req.api_key or config.MODELSCOPE_API_KEY).strip()
    if not clean_token:
        raise HTTPException(status_code=400, detail="未提供 ModelScope API Key")

    headers = {
        "Authorization": f"Bearer {clean_token}",
        "Content-Type": "application/json",
        "X-ModelScope-Async-Mode": "true",
    }
    task_id = req.task_id
    print(f"Resuming polling for Angle Task: {task_id}")

    try:
        async with httpx.AsyncClient(timeout=30) as client:
            for i in range(300):
                await asyncio.sleep(2)
                try:
                    result = await client.get(
                        f"{base_url}v1/tasks/{task_id}",
                        headers={**headers, "X-ModelScope-Task-Type": "image_generation"},
                    )
                    data = result.json()
                    status = data.get("task_status")

                    if status == "SUCCEED":
                        img_url = data["output_images"][0]
                        local_path = ""
                        try:
                            async with httpx.AsyncClient() as dl_client:
                                img_res = await dl_client.get(img_url)
                                if img_res.status_code == 200:
                                    filename = f"cloud_angle_{int(time.time())}.png"
                                    file_path = imageproc.output_path_for(filename, "output")
                                    with open(file_path, "wb") as f:
                                        f.write(img_res.content)
                                    local_path = imageproc.output_url_for(filename, "output")
                                else:
                                    local_path = img_url
                        except Exception:
                            local_path = img_url

                        record = {"timestamp": time.time(), "prompt": f"Resumed {task_id}", "images": [local_path], "type": "angle"}
                        store.save_to_history(record)
                        if req.client_id:
                            await ws.manager.send_personal_message({"type": "cloud_status", "status": "SUCCEED", "task_id": task_id}, req.client_id)
                        return {"url": local_path}

                    elif status == "FAILED":
                        if req.client_id:
                            await ws.manager.send_personal_message({"type": "cloud_status", "status": "FAILED", "task_id": task_id}, req.client_id)
                        raise Exception(f"ModelScope task failed: {data}")

                    if i % 5 == 0 and req.client_id:
                        await ws.manager.send_personal_message({
                            "type": "cloud_status", "status": f"{status} ({i}/300)",
                            "task_id": task_id, "progress": i, "total": 300,
                        }, req.client_id)

                except Exception as loop_e:
                    print(f"Angle polling error: {loop_e}")
                    continue

            if req.client_id:
                await ws.manager.send_personal_message({"type": "cloud_status", "status": "TIMEOUT", "task_id": task_id}, req.client_id)
            return {"status": "timeout", "task_id": task_id, "message": "Task still pending"}

    except Exception as e:
        print(f"Angle polling error: {e}")
        raise HTTPException(status_code=400, detail=str(e))


@router.post("/api/angle/generate")
async def generate_angle_cloud(req: CloudGenRequest):
    base_url = "https://api-inference.modelscope.cn/"
    clean_token = (req.api_key or config.MODELSCOPE_API_KEY).strip()
    if not clean_token:
        raise HTTPException(status_code=400, detail="未提供 ModelScope API Key")

    headers = {
        "Authorization": f"Bearer {clean_token}",
        "Content-Type": "application/json",
        "X-ModelScope-Async-Mode": "true",
    }
    model = config.selected_model(req.model, "Qwen/Qwen-Image-Edit-2511")
    payload = {
        "model": model,
        "prompt": req.prompt.strip(),
        "image_url": [imageproc.modelscope_image_url(url, max_size=1536) for url in req.image_urls],
    }
    if req.resolution:
        payload["size"] = config.modelscope_size(req.resolution)
    if req.loras is not None:
        payload["loras"] = req.loras

    try:
        async with httpx.AsyncClient(timeout=30) as client:
            submit_res = await client.post(f"{base_url}v1/images/generations", headers=headers, json=payload)
            if submit_res.status_code != 200:
                try:
                    detail = submit_res.json()
                except Exception:
                    detail = submit_res.text
                raise HTTPException(status_code=submit_res.status_code, detail=detail)

            task_id = submit_res.json().get("task_id")
            print(f"Angle Task submitted, ID: {task_id}")

            for i in range(300):
                await asyncio.sleep(2)
                try:
                    result = await client.get(
                        f"{base_url}v1/tasks/{task_id}",
                        headers={**headers, "X-ModelScope-Task-Type": "image_generation"},
                    )
                    data = result.json()
                    status = data.get("task_status")

                    if status == "SUCCEED":
                        img_url = data["output_images"][0]
                        local_path = ""
                        try:
                            async with httpx.AsyncClient() as dl_client:
                                img_res = await dl_client.get(img_url)
                                if img_res.status_code == 200:
                                    filename = f"cloud_angle_{int(time.time())}.png"
                                    file_path = imageproc.output_path_for(filename, "output")
                                    with open(file_path, "wb") as f:
                                        f.write(img_res.content)
                                    local_path = imageproc.output_url_for(filename, "output")
                                else:
                                    local_path = img_url
                        except Exception:
                            local_path = img_url

                        record = {"timestamp": time.time(), "prompt": req.prompt, "images": [local_path], "type": "angle"}
                        store.save_to_history(record)
                        if req.client_id:
                            await ws.manager.send_personal_message({"type": "cloud_status", "status": "SUCCEED", "task_id": task_id}, req.client_id)
                        if ws.GLOBAL_LOOP:
                            asyncio.run_coroutine_threadsafe(ws.manager.broadcast_new_image(record), ws.GLOBAL_LOOP)
                        return {"url": local_path, "task_id": task_id}

                    elif status == "FAILED":
                        if req.client_id:
                            await ws.manager.send_personal_message({"type": "cloud_status", "status": "FAILED", "task_id": task_id}, req.client_id)
                        raise Exception(f"ModelScope task failed: {data}")

                    if i % 5 == 0 and req.client_id:
                        await ws.manager.send_personal_message({
                            "type": "cloud_status", "status": f"{status} ({i}/300)",
                            "task_id": task_id, "progress": i, "total": 300,
                        }, req.client_id)

                except Exception as loop_e:
                    print(f"Angle polling error: {loop_e}")
                    continue

            if req.client_id:
                await ws.manager.send_personal_message({"type": "cloud_status", "status": "TIMEOUT", "task_id": task_id}, req.client_id)
            return {"status": "timeout", "task_id": task_id, "message": "Task still pending"}

    except HTTPException:
        raise
    except Exception as e:
        print(f"Angle generation error: {e}")
        raise HTTPException(status_code=400, detail=str(e))


# ---------------- ModelScope Z-Image 云端生图 ----------------

@router.post("/generate")
async def generate_cloud(req: CloudGenRequest):
    base_url = "https://api-inference.modelscope.cn/"
    clean_token = (req.api_key or config.MODELSCOPE_API_KEY).strip()
    if not clean_token:
        raise HTTPException(status_code=400, detail="未提供 ModelScope API Key")

    headers = {
        "Authorization": f"Bearer {clean_token}",
        "Content-Type": "application/json",
    }
    payload = {
        "model": "Tongyi-MAI/Z-Image-Turbo",
        "prompt": req.prompt.strip(),
        "size": config.modelscope_size(req.resolution),
        "n": 1,
    }
    if req.loras is not None:
        payload["loras"] = req.loras

    try:
        async with httpx.AsyncClient(timeout=30) as client:
            submit_res = await client.post(
                f"{base_url}v1/images/generations",
                headers={**headers, "X-ModelScope-Async-Mode": "true"},
                json=payload,
            )
            if submit_res.status_code != 200:
                try:
                    detail = submit_res.json()
                except Exception:
                    detail = submit_res.text
                raise HTTPException(status_code=submit_res.status_code, detail=detail)

            task_id = submit_res.json().get("task_id")
            print(f"Z-Image Task submitted, ID: {task_id}")

            for i in range(200):
                await asyncio.sleep(3)
                try:
                    result = await client.get(
                        f"{base_url}v1/tasks/{task_id}",
                        headers={**headers, "X-ModelScope-Task-Type": "image_generation"},
                    )
                    data = result.json()
                    status = data.get("task_status")

                    if i % 5 == 0:
                        print(f"Task {task_id} status check {i}: {status}")

                    if status == "SUCCEED":
                        img_url = data["output_images"][0]
                        local_path = ""
                        try:
                            async with httpx.AsyncClient() as dl_client:
                                img_res = await dl_client.get(img_url)
                                if img_res.status_code == 200:
                                    filename = f"cloud_{int(time.time())}.png"
                                    file_path = imageproc.output_path_for(filename, "output")
                                    with open(file_path, "wb") as f:
                                        f.write(img_res.content)
                                    local_path = imageproc.output_url_for(filename, "output")
                                else:
                                    local_path = img_url
                        except Exception as dl_e:
                            print(f"Download error: {dl_e}")
                            local_path = img_url

                        record = {"timestamp": time.time(), "prompt": req.prompt, "images": [local_path], "type": "cloud"}
                        store.save_to_history(record)
                        try:
                            await ws.manager.broadcast_new_image(record)
                        except Exception:
                            pass
                        return {"url": local_path}

                    elif status == "FAILED":
                        raise Exception(f"ModelScope task failed: {data}")

                except Exception as loop_e:
                    print(f"Polling error (retrying): {loop_e}")
                    continue

            raise Exception("Cloud generation timeout")

    except HTTPException:
        raise
    except Exception as e:
        print(f"Cloud generation error: {e}")
        raise HTTPException(status_code=400, detail=str(e))


# ---------------- ModelScope 通用图片生成 ----------------

@router.post("/api/ms/generate")
async def ms_generate(req: MsGenerateRequest):
    base_url = "https://api-inference.modelscope.cn/"
    clean_token = (req.api_key or config.MODELSCOPE_API_KEY).strip()
    if not clean_token:
        raise HTTPException(status_code=400, detail="未配置 ModelScope API Key，请在 API 设置中填写，或重新保存 ModelScope Token。")

    headers = {
        "Authorization": f"Bearer {clean_token}",
        "Content-Type": "application/json",
        "X-ModelScope-Async-Mode": "true",
    }
    payload = {
        "model": req.model,
        "prompt": req.prompt.strip(),
    }
    if req.width and req.height:
        payload["width"] = req.width
        payload["height"] = req.height
        payload["size"] = config.modelscope_size(req.size or f"{req.width}x{req.height}")
    elif req.size:
        payload["size"] = config.modelscope_size(req.size)
    if req.image_urls:
        payload["image_url"] = [imageproc.modelscope_image_url(url, max_size=1536) for url in req.image_urls]
    if req.loras is not None:
        payload["loras"] = req.loras

    try:
        async with httpx.AsyncClient(timeout=30) as client:
            submit_res = await client.post(f"{base_url}v1/images/generations", headers=headers, json=payload)
            if submit_res.status_code != 200:
                try:
                    detail = submit_res.json()
                except Exception:
                    detail = submit_res.text
                raise HTTPException(status_code=submit_res.status_code, detail=detail)

            task_id = submit_res.json().get("task_id")
            print(f"MS Generate Task submitted ({req.model}), ID: {task_id}")

            TERMINAL_FAILED_STATUSES = {"FAILED", "FAIL", "ERROR", "CANCELED", "CANCELLED", "TIMEOUT", "REVOKED"}

            for i in range(300):
                await asyncio.sleep(2)
                try:
                    result = await client.get(
                        f"{base_url}v1/tasks/{task_id}",
                        headers={**headers, "X-ModelScope-Task-Type": "image_generation"},
                    )
                    data = result.json()
                    status = data.get("task_status")
                    print(f"MS Task {task_id} poll {i}: status={status}")

                    if status == "SUCCEED":
                        img_url = data["output_images"][0]
                        local_path = ""
                        try:
                            async with httpx.AsyncClient() as dl_client:
                                img_res = await dl_client.get(img_url)
                                if img_res.status_code == 200:
                                    filename = f"ms_{req.model.replace('/', '_').replace(':', '_')}_{int(time.time())}.png"
                                    file_path = imageproc.output_path_for(filename, "output")
                                    with open(file_path, "wb") as f:
                                        f.write(img_res.content)
                                    local_path = imageproc.output_url_for(filename, "output")
                                else:
                                    local_path = img_url
                        except Exception:
                            local_path = img_url

                        record = {
                            "timestamp": time.time(),
                            "prompt": req.prompt,
                            "images": [local_path],
                            "type": "klein",
                            "model": req.model,
                        }
                        store.save_to_history(record)
                        if ws.GLOBAL_LOOP:
                            asyncio.run_coroutine_threadsafe(ws.manager.broadcast_new_image(record), ws.GLOBAL_LOOP)
                        return {"url": local_path, "task_id": task_id}

                    elif status in TERMINAL_FAILED_STATUSES:
                        error_info = data.get("error_info") or data.get("message") or data.get("detail") or str(data)
                        raise HTTPException(status_code=502, detail=f"MS task {status}: {error_info}")

                except HTTPException:
                    raise
                except Exception as loop_e:
                    print(f"MS polling error: {loop_e}")
                    continue

            raise HTTPException(status_code=504, detail="MS 生图超时")

    except HTTPException:
        raise
    except Exception as e:
        print(f"MS generate error: {e}")
        raise HTTPException(status_code=400, detail=str(e))


# ---------------- 本地 ComfyUI 生图 ----------------

@router.post("/api/generate")
def generate(req: GenerateRequest):
    current_task = None
    target_backend = None
    with config.QUEUE_LOCK:
        task_id = comfyui.NEXT_TASK_ID
        comfyui.NEXT_TASK_ID += 1
        current_task = {"task_id": task_id, "client_id": req.client_id}
        comfyui.QUEUE.append(current_task)

    try:
        required_images = []
        for _, node_inputs in req.params.items():
            if isinstance(node_inputs, dict) and "image" in node_inputs:
                image_name = node_inputs["image"]
                if isinstance(image_name, str) and image_name:
                    required_images.append(image_name)

        target_backend = comfyui.get_best_backend(required_images)
        with config.LOAD_LOCK:
            comfyui.BACKEND_LOCAL_LOAD[target_backend] += 1

        # 把缺失的参考图同步到目标后端
        for image_name in required_images:
            need_sync = False
            try:
                check_url = f"http://{target_backend}/view?filename={urllib.parse.quote(image_name)}&type=input"
                resp = requests.get(check_url, stream=True, timeout=0.5)
                resp.close()
                if resp.status_code != 200:
                    need_sync = True
            except Exception:
                need_sync = True

            if need_sync:
                image_content = None
                image_type = "image/png"
                for addr in config.COMFYUI_INSTANCES:
                    if addr == target_backend:
                        continue
                    try:
                        src_url = f"http://{addr}/view?filename={urllib.parse.quote(image_name)}&type=input"
                        r = requests.get(src_url, timeout=5)
                        if r.status_code == 200:
                            image_content = r.content
                            image_type = r.headers.get("Content-Type", "image/png")
                            break
                    except Exception:
                        continue

                if image_content:
                    try:
                        files = {"image": (image_name, image_content, image_type)}
                        requests.post(f"http://{target_backend}/upload/image", files=files, timeout=10)
                    except Exception as e:
                        print(f"Sync upload failed: {e}")

        workflow_path = os.path.join(config.WORKFLOW_DIR, req.workflow_json)
        if not os.path.exists(workflow_path) and req.workflow_json == "Z-Image.json":
            workflow_path = config.WORKFLOW_PATH
        if not os.path.exists(workflow_path):
            raise Exception(f"Workflow file not found: {req.workflow_json}")

        with open(workflow_path, "r", encoding="utf-8") as f:
            workflow = json.load(f)

        seed = random.randint(1, 10 ** 15)

        # 已知节点 ID 的种子注入。新增工作流如果不命中以下 ID，需要从 params 显式覆盖
        if "23" in workflow and req.prompt:
            workflow["23"]["inputs"]["text"] = req.prompt
        if "144" in workflow:
            workflow["144"]["inputs"]["width"] = req.width
            workflow["144"]["inputs"]["height"] = req.height
        if "22" in workflow:
            workflow["22"]["inputs"]["seed"] = seed
        if "158" in workflow:
            workflow["158"]["inputs"]["noise_seed"] = seed
        for node_id in ["146", "181"]:
            if node_id in workflow and "inputs" in workflow[node_id] and "seed" in workflow[node_id]["inputs"]:
                workflow[node_id]["inputs"]["seed"] = seed
        if "184" in workflow and "inputs" in workflow["184"] and "seed" in workflow["184"]["inputs"]:
            workflow["184"]["inputs"]["seed"] = seed
        if "172" in workflow and "inputs" in workflow["172"] and "seed" in workflow["172"]["inputs"]:
            workflow["172"]["inputs"]["seed"] = seed % 4294967295
        if "14" in workflow and "inputs" in workflow["14"] and "seed" in workflow["14"]["inputs"]:
            workflow["14"]["inputs"]["seed"] = seed

        for node_id, node_inputs in req.params.items():
            if node_id in workflow:
                if "inputs" not in workflow[node_id]:
                    workflow[node_id]["inputs"] = {}
                for input_name, value in node_inputs.items():
                    workflow[node_id]["inputs"][input_name] = value

        p = {"prompt": workflow, "client_id": config.CLIENT_ID}
        data = json.dumps(p).encode("utf-8")
        try:
            post_req = urllib.request.Request(f"http://{target_backend}/prompt", data=data)
            prompt_id = json.loads(urllib.request.urlopen(post_req, timeout=10).read())["prompt_id"]
        except urllib.error.HTTPError as e:
            error_body = e.read().decode("utf-8")
            raise Exception(f"HTTP Error {e.code}: {error_body}")

        history_data = None
        for _ in range(config.COMFYUI_HISTORY_TIMEOUT):
            try:
                res = comfyui.get_comfy_history(target_backend, prompt_id)
                if prompt_id in res:
                    history_data = res[prompt_id]
                    break
            except Exception:
                pass
            time.sleep(1)

        if not history_data:
            raise Exception("ComfyUI 渲染超时")

        local_images = []
        local_videos = []
        local_urls = []
        current_timestamp = time.time()
        if "outputs" in history_data:
            for node_id in history_data["outputs"]:
                node_output = history_data["outputs"][node_id]
                if "images" in node_output:
                    for img in node_output["images"]:
                        prefix = f"{req.type}_{int(current_timestamp)}_"
                        local_path = comfyui.download_comfy_output(target_backend, img, prefix=prefix)
                        if req.convert_to_jpg:
                            local_path = imageproc.convert_output_to_jpg(local_path)
                        local_images.append(local_path)
                        local_urls.append(local_path)
                for output_key in ("videos", "gifs", "animated"):
                    for video in node_output.get(output_key, []) or []:
                        if not isinstance(video, dict) or not video.get("filename"):
                            continue
                        prefix = f"{req.type}_{int(current_timestamp)}_"
                        local_path = comfyui.download_comfy_output(target_backend, video, prefix=prefix)
                        local_videos.append(local_path)
                        local_urls.append(local_path)

        result = {
            "prompt": req.prompt if req.prompt else "Detail Enhance",
            "images": local_images,
            "videos": local_videos,
            "outputs": local_urls,
            "seed": seed,
            "timestamp": current_timestamp,
            "type": req.type,
            "workflow_json": req.workflow_json,
            "task_id": task_id,
            "prompt_id": prompt_id,
            "backend": target_backend,
            "params": req.params,
        }
        store.save_to_history(result)
        if ws.GLOBAL_LOOP:
            asyncio.run_coroutine_threadsafe(ws.manager.broadcast_new_image(result), ws.GLOBAL_LOOP)
        return result

    except Exception as e:
        return {"images": [], "error": str(e)}
    finally:
        if target_backend:
            with config.LOAD_LOCK:
                if comfyui.BACKEND_LOCAL_LOAD.get(target_backend, 0) > 0:
                    comfyui.BACKEND_LOCAL_LOAD[target_backend] -= 1
        if current_task:
            with config.QUEUE_LOCK:
                if current_task in comfyui.QUEUE:
                    comfyui.QUEUE.remove(current_task)
