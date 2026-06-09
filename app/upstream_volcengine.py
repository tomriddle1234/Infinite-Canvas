"""Volcengine Ark helpers for Seedream and Seedance dedicated nodes."""

import base64
import os
import time

from fastapi import HTTPException
from volcenginesdkarkruntime import Ark
from volcenginesdkarkruntime._exceptions import ArkAPIConnectionError, ArkAPIStatusError, ArkAPITimeoutError, ArkNotFoundError

from . import config, imageproc


TERMINAL_FAILED = {"failed", "fail", "error", "canceled", "cancelled", "timeout", "rejected", "expired"}
TERMINAL_SUCCESS = {"succeeded", "success", "done", "completed"}


def _client():
    key = (config.VOLCENGINE_ARK_API_KEY or "").strip()
    if not key:
        raise HTTPException(status_code=400, detail="未配置 VOLCENGINE_ARK_API_KEY，请先在 API 设置中填写。")
    return Ark(api_key=key, base_url=config.VOLCENGINE_ARK_BASE_URL)


def _valid_model(model: str, models: dict, fallback: str) -> str:
    value = (model or fallback).strip()
    if value in models:
        return models[value]
    if value in models.values():
        return value
    raise HTTPException(status_code=400, detail=f"不支持的模型：{value}")


def _data_url(ref, max_size=None) -> str:
    data = ref.model_dump() if hasattr(ref, "model_dump") else dict(ref or {})
    url = data.get("url", "")
    if not url:
        return ""
    if url.startswith("http://") or url.startswith("https://") or url.startswith("data:") or url.startswith("asset://"):
        return url
    path = imageproc.output_file_from_url(url)
    if not path:
        return url
    if imageproc.content_type_for_path(path).startswith("image/"):
        return imageproc.reference_to_data_url(data, max_size=max_size)
    with open(path, "rb") as handle:
        encoded = base64.b64encode(handle.read()).decode("ascii")
    return f"data:{imageproc.content_type_for_path(path)};base64,{encoded}"


def _is_seedance_1_5(model: str) -> bool:
    text = str(model or "").lower()
    return "seedance-1-5" in text or "seedance-1.5" in text


def _read_attr(obj, *names):
    cur = obj
    for name in names:
        if cur is None:
            return None
        if isinstance(cur, dict):
            cur = cur.get(name)
        else:
            cur = getattr(cur, name, None)
    return cur


def _to_dict(obj):
    if obj is None:
        return {}
    if isinstance(obj, dict):
        return obj
    if hasattr(obj, "model_dump"):
        return obj.model_dump()
    if hasattr(obj, "dict"):
        return obj.dict()
    return {"value": str(obj)}


def _connection_error_detail(prefix: str, exc: Exception) -> str:
    detail = str(exc)
    cause = exc.__cause__
    if cause is not None:
        cause_text = str(cause)
        if cause_text:
            detail = f"{detail}; underlying={type(cause).__name__}: {cause_text}"
        else:
            detail = f"{detail}; underlying={type(cause).__name__}"
    return f"{prefix}: {detail}"


def _seedream_images(raw):
    images = []
    for item in _read_attr(raw, "data") or []:
        b64 = _read_attr(item, "b64_json")
        url = _read_attr(item, "url")
        if b64:
            images.append({"type": "b64", "value": b64})
        elif url:
            images.append({"type": "url", "value": url})
    if _read_attr(raw, "b64_json"):
        images.append({"type": "b64", "value": _read_attr(raw, "b64_json")})
    return images


def generate_seedream_once(payload, index: int = 0) -> tuple[list[dict], dict]:
    client = _client()
    model = _valid_model(payload.model, config.SEEDREAM_MODELS, config.SEEDREAM_MODELS["seedream-4.5"])
    body = {
        "model": model,
        "prompt": payload.prompt,
        "size": imageproc.normalize_seedream_size(payload.size or "2048x2048", model),
        "response_format": "b64_json",
        "watermark": bool(payload.watermark),
        "sequential_image_generation": "disabled",
    }
    if payload.seed is not None and int(payload.seed) >= 0:
        body["seed"] = int(payload.seed) + index
    refs = [_data_url(ref, max_size=2048) for ref in payload.reference_images[:9] if ref.url]
    if refs:
        body["image"] = refs
    raw_items = []
    images = []
    try:
        try:
            stream = client.images.generate(**body, stream=True)
            for event in stream:
                raw_items.append(_to_dict(event))
                images.extend(_seedream_images(event))
        except TypeError:
            raw = client.images.generate(**body)
            raw_items.append(_to_dict(raw))
            images.extend(_seedream_images(raw))
    except ArkAPITimeoutError as exc:
        raise HTTPException(status_code=502, detail=_connection_error_detail("Seedream 请求超时", exc)) from exc
    except ArkAPIConnectionError as exc:
        raise HTTPException(status_code=502, detail=_connection_error_detail("Seedream 连接火山 Ark 失败", exc)) from exc
    if not images:
        raise HTTPException(status_code=502, detail=f"Seedream 未返回图片：{raw_items[-1] if raw_items else '{}'}")
    return images, {"events": raw_items, "model": model, "size": body["size"]}


def _content_item(kind: str, url: str, role: str) -> dict:
    key = f"{kind}_url"
    return {"type": key, key: {"url": url}, "role": role}


def _local_ref_path(ref):
    data = ref.model_dump() if hasattr(ref, "model_dump") else dict(ref or {})
    return imageproc.output_file_from_url(data.get("url", ""))


def _validate_seedance_media_refs(image_refs, video_refs, audio_refs):
    if audio_refs and not image_refs and not video_refs:
        raise HTTPException(status_code=400, detail="Seedance 参考音频必须同时提供至少一张参考图片或一个参考视频。")
    for ref in video_refs[:3]:
        url = ref.url if hasattr(ref, "url") else (ref or {}).get("url", "")
        path = _local_ref_path(ref)
        ext = os.path.splitext(path or url.split("?", 1)[0])[1].lower()
        if ext not in {".mp4", ".mov"}:
            raise HTTPException(status_code=400, detail="Seedance 参考视频仅支持 MP4 或 MOV。")
        if path and os.path.getsize(path) > 50 * 1024 * 1024:
            raise HTTPException(status_code=400, detail="Seedance 单个参考视频不能超过 50MB。")
    for ref in audio_refs[:3]:
        url = ref.url if hasattr(ref, "url") else (ref or {}).get("url", "")
        path = _local_ref_path(ref)
        ext = os.path.splitext(path or url.split("?", 1)[0])[1].lower()
        if ext not in {".mp3", ".wav"}:
            raise HTTPException(status_code=400, detail="Seedance 参考音频仅支持 MP3 或 WAV。")
        if path and os.path.getsize(path) > 15 * 1024 * 1024:
            raise HTTPException(status_code=400, detail="Seedance 单个参考音频不能超过 15MB。")


def _seedance_duration(model: str, duration) -> int:
    value = int(duration or 5)
    max_duration = 12 if _is_seedance_1_5(model) else 15
    return max(4, min(max_duration, value))


def submit_seedance(payload) -> dict:
    client = _client()
    model = _valid_model(payload.model, config.SEEDANCE_MODELS, config.SEEDANCE_MODELS["seedance-1.5-pro"])
    content = [{"type": "text", "text": payload.prompt}]
    image_refs = [ref for ref in payload.reference_images if ref.url]
    video_refs = [ref for ref in (payload.reference_videos or []) if ref.url]
    audio_refs = [ref for ref in (payload.reference_audios or []) if ref.url]
    _validate_seedance_media_refs(image_refs, video_refs, audio_refs)
    for index, ref in enumerate(image_refs[:9]):
        role = ref.role or ("first_frame" if len(image_refs) <= 2 and index == 0 else "last_frame" if len(image_refs) <= 2 and index == 1 else "reference_image")
        content.append(_content_item("image", _data_url(ref, max_size=1536), role))
    for ref in video_refs[:3]:
        content.append(_content_item("video", _data_url(ref), "reference_video"))
    for ref in audio_refs[:3]:
        content.append(_content_item("audio", _data_url(ref), "reference_audio"))
    body = {
        "model": model,
        "content": content,
        "return_last_frame": bool(payload.return_last_frame),
        "duration": _seedance_duration(model, payload.duration),
        "ratio": payload.aspect_ratio or "16:9",
        "resolution": payload.resolution or "720p",
        "generate_audio": bool(payload.generate_audio),
    }
    if payload.seed is not None:
        body["seed"] = int(payload.seed)
    try:
        task = client.content_generation.tasks.create(**body)
    except ArkAPITimeoutError as exc:
        raise HTTPException(status_code=502, detail=_connection_error_detail("Seedance 请求超时", exc)) from exc
    except ArkAPIConnectionError as exc:
        raise HTTPException(status_code=502, detail=_connection_error_detail("Seedance 连接火山 Ark 失败", exc)) from exc
    except ArkAPIStatusError as exc:
        raise HTTPException(status_code=getattr(exc, "status_code", None) or 502, detail=f"Seedance 提交失败：{exc}") from exc
    raw = _to_dict(task)
    task_id = _read_attr(task, "id") or raw.get("id") or raw.get("task_id")
    if not task_id:
        raise HTTPException(status_code=502, detail=f"Seedance 未返回 task_id：{raw}")
    return {"task_ids": [task_id], "raw": raw, "model": model, "submitted_at": time.time()}


def task_urls(raw) -> list[str]:
    urls = []
    for path in [
        ("content", "video_url"),
        ("content", "url"),
        ("video_url",),
        ("url",),
        ("result", "video_url"),
        ("output", "video_url"),
    ]:
        value = _read_attr(raw, *path)
        if isinstance(value, str) and value:
            urls.append(value)
    return list(dict.fromkeys(urls))


def _task_status(task, raw: dict) -> str:
    return str(_read_attr(task, "status") or raw.get("status") or raw.get("task_status") or "").lower()


def _normalize_task_id(task, raw: dict) -> str:
    return str(_read_attr(task, "id") or raw.get("id") or raw.get("task_id") or "").strip()


def _list_seedance_tasks(client, task_ids: list[str]) -> dict[str, dict]:
    cleaned = [item for item in task_ids if item]
    if not cleaned:
        return {}
    response = client.content_generation.tasks.list(
        task_ids=cleaned,
        page_num=1,
        page_size=max(len(cleaned), 10),
    )
    items = getattr(response, "items", None) or _read_attr(response, "items") or []
    found = {}
    for item in items:
        raw = _to_dict(item)
        tid = _normalize_task_id(item, raw)
        if tid:
            found[tid] = raw
    return found


def _seedance_task_summary(item, raw: dict | None = None) -> dict:
    raw = raw or _to_dict(item)
    task_id = _normalize_task_id(item, raw)
    return {
        "task_id": task_id,
        "status": _task_status(item, raw),
        "model": raw.get("model") or _read_attr(item, "model") or "",
        "created_at": raw.get("created_at") or _read_attr(item, "created_at"),
        "updated_at": raw.get("updated_at") or _read_attr(item, "updated_at"),
        "videos": task_urls(raw),
        "raw": raw,
    }


def list_seedance_tasks(model: str = "", status: str = "all", page_size: int = 10) -> dict:
    client = _client()
    kwargs = {"page_num": 1, "page_size": max(1, min(50, int(page_size or 10)))}
    if status and status != "all":
        kwargs["status"] = status
    if model:
        kwargs["model"] = _valid_model(model, config.SEEDANCE_MODELS, model)
    try:
        response = client.content_generation.tasks.list(**kwargs)
    except ArkAPITimeoutError as exc:
        raise HTTPException(status_code=502, detail=_connection_error_detail("Seedance 历史任务查询超时", exc)) from exc
    except ArkAPIConnectionError as exc:
        raise HTTPException(status_code=502, detail=_connection_error_detail("Seedance 连接火山 Ark 失败", exc)) from exc
    items = getattr(response, "items", None) or _read_attr(response, "items") or []
    tasks = [_seedance_task_summary(item) for item in items]
    tasks = [item for item in tasks if item.get("task_id")]
    return {"tasks": tasks, "total": getattr(response, "total", None) or _read_attr(response, "total") or len(tasks)}


def _collect_seedance_task(raw: dict, task_id: str, tasks: list, videos: list, failed: list, pending: list):
    status = str(raw.get("status") or raw.get("task_status") or "").lower()
    tasks.append(raw)
    if status in TERMINAL_FAILED:
        failed.append({"task_id": task_id, "status": status, "raw": raw})
    elif status in TERMINAL_SUCCESS or task_urls(raw):
        videos.extend(task_urls(raw))
    else:
        pending.append({"task_id": task_id, "status": status or "pending", "raw": raw})


def poll_seedance(task_ids: list[str]) -> dict:
    client = _client()
    tasks = []
    videos = []
    failed = []
    pending = []
    missing = []
    for task_id in [item for item in task_ids if item]:
        try:
            task = client.content_generation.tasks.get(task_id=task_id)
        except ArkAPITimeoutError as exc:
            raise HTTPException(status_code=502, detail=_connection_error_detail("Seedance 查询超时", exc)) from exc
        except ArkAPIConnectionError as exc:
            raise HTTPException(status_code=502, detail=_connection_error_detail("Seedance 连接火山 Ark 失败", exc)) from exc
        except ArkNotFoundError as exc:
            missing.append({"task_id": task_id, "error": str(exc)})
            continue
        except ArkAPIStatusError as exc:
            if getattr(exc, "status_code", None) == 404:
                missing.append({"task_id": task_id, "error": str(exc)})
                continue
            raise HTTPException(status_code=502, detail=f"Seedance 查询失败：{exc}") from exc
        except Exception as exc:
            raise HTTPException(status_code=502, detail=f"Seedance 查询失败：{exc}") from exc
        raw = _to_dict(task)
        _collect_seedance_task(raw, task_id, tasks, videos, failed, pending)
    if missing:
        missing_ids = [item["task_id"] for item in missing]
        try:
            listed = _list_seedance_tasks(client, missing_ids)
        except ArkAPITimeoutError as exc:
            raise HTTPException(status_code=502, detail=_connection_error_detail("Seedance 历史任务查询超时", exc)) from exc
        except ArkAPIConnectionError as exc:
            raise HTTPException(status_code=502, detail=_connection_error_detail("Seedance 连接火山 Ark 失败", exc)) from exc
        except Exception as exc:
            raise HTTPException(status_code=502, detail=f"Seedance 历史任务查询失败：{exc}") from exc
        resolved = set()
        for task_id in missing_ids:
            raw = listed.get(task_id)
            if not raw:
                continue
            _collect_seedance_task(raw, task_id, tasks, videos, failed, pending)
            resolved.add(task_id)
        missing = [item for item in missing if item["task_id"] not in resolved]
    if failed:
        return {"status": "failed", "tasks": tasks, "failed": failed, "missing": missing, "videos": videos}
    if missing and not tasks:
        return {"status": "missing", "tasks": tasks, "missing": missing, "videos": videos}
    if pending or not videos:
        return {"status": "pending", "tasks": tasks, "pending": pending, "missing": missing, "videos": videos}
    return {"status": "succeeded", "tasks": tasks, "missing": missing, "videos": videos}
