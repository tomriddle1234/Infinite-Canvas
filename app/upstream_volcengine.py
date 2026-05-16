"""Volcengine Ark helpers for Seedream and Seedance dedicated nodes."""

import base64
import os
import time

from fastapi import HTTPException

from . import config, imageproc


TERMINAL_FAILED = {"failed", "fail", "error", "canceled", "cancelled", "timeout", "rejected"}
TERMINAL_SUCCESS = {"succeeded", "success", "done", "completed"}


def _client():
    key = (config.VOLCENGINE_ARK_API_KEY or "").strip()
    if not key:
        raise HTTPException(status_code=400, detail="未配置 VOLCENGINE_ARK_API_KEY，请先在 API 设置中填写。")
    try:
        from volcenginesdkarkruntime import Ark
    except Exception as exc:
        raise HTTPException(status_code=500, detail="缺少火山 Ark SDK，请先安装 requirements.txt 中的 volcengine-python-sdk[ark]。") from exc
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
    if url.startswith("http://") or url.startswith("https://") or url.startswith("data:"):
        return url
    path = imageproc.output_file_from_url(url)
    if not path:
        return url
    if imageproc.content_type_for_path(path).startswith("image/"):
        return imageproc.reference_to_data_url(data, max_size=max_size)
    with open(path, "rb") as handle:
        encoded = base64.b64encode(handle.read()).decode("ascii")
    return f"data:{imageproc.content_type_for_path(path)};base64,{encoded}"


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
        stream = client.images.generate(**body, stream=True)
        for event in stream:
            raw_items.append(_to_dict(event))
            images.extend(_seedream_images(event))
    except TypeError:
        raw = client.images.generate(**body)
        raw_items.append(_to_dict(raw))
        images.extend(_seedream_images(raw))
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
    max_duration = 12 if "1-5" in str(model or "") else 15
    return max(4, min(max_duration, value))


def submit_seedance(payload) -> dict:
    client = _client()
    model = _valid_model(payload.model, config.SEEDANCE_MODELS, config.SEEDANCE_MODELS["seedance-2.0-fast"])
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
    task = client.content_generation.tasks.create(**body)
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


def poll_seedance(task_ids: list[str]) -> dict:
    client = _client()
    tasks = []
    videos = []
    failed = []
    pending = []
    for task_id in [item for item in task_ids if item]:
        task = client.content_generation.tasks.get(task_id=task_id)
        raw = _to_dict(task)
        status = str(_read_attr(task, "status") or raw.get("status") or raw.get("task_status") or "").lower()
        tasks.append(raw)
        if status in TERMINAL_FAILED:
            failed.append({"task_id": task_id, "status": status, "raw": raw})
        elif status in TERMINAL_SUCCESS or task_urls(raw):
            videos.extend(task_urls(raw))
        else:
            pending.append({"task_id": task_id, "status": status or "pending", "raw": raw})
    if failed:
        return {"status": "failed", "tasks": tasks, "failed": failed, "videos": videos}
    if pending or not videos:
        return {"status": "pending", "tasks": tasks, "pending": pending, "videos": videos}
    return {"status": "succeeded", "tasks": tasks, "videos": videos}
