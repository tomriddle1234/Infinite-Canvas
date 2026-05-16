"""上游 LLM/图像/视频接口的协议相关工具与高层调用。

包含：
- 响应解析（apimart 包装、chat 文本提取、image/task 提取）
- 通用图像生成 ``generate_ai_image`` 与 ModelScope 异步任务等待
- 视频接口请求构造和轮询
"""

import asyncio
import os
import time

import httpx
from fastapi import HTTPException

from . import config
from . import imageproc
from . import providers


# --- 响应解包 / 文本提取 ---

def unwrap_apimart_response(raw):
    """APIMart 将标准 OpenAI 响应包在 {"code":200,"data":{...}} 里；检测到就解包。"""
    if isinstance(raw, dict) and "data" in raw and isinstance(raw.get("data"), dict) and "choices" not in raw:
        return raw["data"]
    return raw


def text_from_chat_response(data):
    data = unwrap_apimart_response(data)
    choices = data.get("choices") or []
    if not choices:
        return ""
    message = choices[0].get("message") or {}
    content = message.get("content", "")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for item in content:
            if isinstance(item, dict):
                parts.append(item.get("text") or item.get("content") or "")
        return "\n".join(part for part in parts if part)
    return str(content)


def text_delta_from_chat_chunk(data):
    choices = data.get("choices") or []
    if not choices:
        return ""
    delta = choices[0].get("delta") or {}
    content = delta.get("content", "")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for item in content:
            if isinstance(item, dict):
                parts.append(item.get("text") or item.get("content") or "")
        return "".join(parts)
    return str(content) if content else ""


def sse_event(data) -> str:
    import json
    return f"data: {json.dumps(data, ensure_ascii=False)}\n\n"


def extract_image(data):
    if isinstance(data.get("data"), dict) and isinstance(data["data"].get("result"), dict):
        data = data["data"]
    if isinstance(data.get("result"), dict):
        result_images = data["result"].get("images") or []
        if result_images:
            first = result_images[0]
            url = first.get("url")
            if isinstance(url, list) and url:
                return {"type": "url", "value": url[0]}
            if isinstance(url, str) and url:
                return {"type": "url", "value": url}
    if isinstance(data.get("data"), dict) and isinstance(data["data"].get("data"), dict):
        data = data["data"]["data"]
    images = data.get("data") or []
    if not images:
        raise HTTPException(status_code=502, detail="生图接口没有返回图片数据")
    first = images[0]
    if first.get("url"):
        return {"type": "url", "value": first["url"]}
    if first.get("b64_json"):
        return {"type": "b64", "value": first["b64_json"]}
    raise HTTPException(status_code=502, detail="无法识别生图接口返回格式")


def extract_task_id(data):
    if not isinstance(data, dict):
        return None
    if data.get("task_id"):
        return str(data["task_id"])
    if data.get("id") and str(data.get("id", "")).startswith("task"):
        return str(data["id"])
    nested = data.get("data")
    if isinstance(nested, list) and nested:
        first = nested[0]
        if isinstance(first, dict):
            return extract_task_id(first)
    if isinstance(nested, dict):
        return extract_task_id(nested)
    return None


# --- 通用任务轮询 ---

async def wait_for_image_task(client, task_id, provider=None):
    base_url = (provider.get("base_url") if provider else config.AI_BASE_URL).rstrip("/")
    is_apimart = providers.is_apimart_provider(provider)
    if is_apimart:
        task_url = f"{base_url}/tasks/{task_id}" if base_url.endswith("/v1") else f"{base_url}/v1/tasks/{task_id}"
    else:
        task_url = f"{base_url}/images/tasks/{task_id}" if base_url.endswith("/v1") else f"{base_url}/v1/images/tasks/{task_id}"
    timeout = config.APIMART_IMAGE_TASK_TIMEOUT if is_apimart else config.IMAGE_TASK_TIMEOUT
    interval = config.APIMART_IMAGE_POLL_INTERVAL if is_apimart else config.IMAGE_POLL_INTERVAL
    initial_delay = config.APIMART_IMAGE_INITIAL_POLL_DELAY if is_apimart else 0
    deadline = time.monotonic() + timeout
    last_payload = {}
    while time.monotonic() < deadline:
        if initial_delay:
            await asyncio.sleep(min(initial_delay, max(0.0, deadline - time.monotonic())))
            initial_delay = 0
            if time.monotonic() >= deadline:
                break
        response = await client.get(task_url, headers=providers.api_headers(provider=provider))
        response.raise_for_status()
        last_payload = response.json()
        task_data = last_payload.get("data") if isinstance(last_payload.get("data"), dict) else last_payload
        status = str(task_data.get("status", "")).upper()
        if status in {"SUCCESS", "COMPLETED"}:
            return last_payload
        if status in {"FAILURE", "FAILED", "ERROR"}:
            error = task_data.get("error") if isinstance(task_data.get("error"), dict) else {}
            reason = task_data.get("fail_reason") or error.get("message") or last_payload.get("message") or "生图任务失败"
            raise HTTPException(status_code=502, detail=f"生图任务失败：{reason}")
        await asyncio.sleep(min(interval, max(0.0, deadline - time.monotonic())))
    raise HTTPException(status_code=504, detail=f"生图任务超时（已等待 {int(timeout)} 秒），task_id={task_id}")


# --- APIMart 文件上传 ---

async def upload_image_for_apimart(client, provider, ref_url: str) -> str:
    """本地 /output/* 或 /assets/* 图片 → APIMart 文件接口 → 返回 https URL；
    已是 http(s) 或 asset:// 时直接原样返回。"""
    if not ref_url:
        return ref_url
    if ref_url.startswith("http://") or ref_url.startswith("https://") or ref_url.startswith("asset://"):
        return ref_url
    if ref_url.startswith("data:"):
        return ""  # APIMart 不接受 base64
    path = imageproc.output_file_from_url(ref_url)
    if not path:
        return ref_url
    try:
        ct = imageproc.content_type_for_path(path)
        base_url = video_api_root(provider)
        upload_url = f"{base_url}/v1/files"
        with open(path, "rb") as fh:
            files = {"file": (os.path.basename(path), fh, ct)}
            resp = await client.post(upload_url, headers=providers.api_headers(provider=provider), files=files, timeout=60)
        if resp.status_code == 200:
            rj = resp.json()
            url = (rj.get("url") or
                   (rj.get("data") or {}).get("url") or
                   (rj.get("file") or {}).get("url") or "")
            if url:
                return url
        print(f"APIMart 文件上传失败 ({resp.status_code})，降级使用原路径: {resp.text[:200]}")
        return ref_url
    except Exception as e:
        print(f"APIMart 文件上传异常，降级使用原路径: {e}")
        return ref_url


# --- ModelScope 异步生图（作为 provider 模式） ---

async def generate_modelscope_provider_image(prompt, size, model, reference_images=None, provider=None):
    clean_token = config.MODELSCOPE_API_KEY.strip()
    if not clean_token:
        raise HTTPException(status_code=400, detail="未配置 ModelScope API Key，请在 API 设置中填写。")
    width, height = imageproc.parse_size_pair(size)
    refs = []
    for ref in (reference_images or [])[:4]:
        if not ref.get("url"):
            continue
        refs.append(imageproc.modelscope_image_url(ref.get("url", ""), max_size=1536))
    headers = {
        "Authorization": f"Bearer {clean_token}",
        "Content-Type": "application/json",
        "X-ModelScope-Async-Mode": "true",
    }
    payload = {
        "model": config.selected_model(model, "Tongyi-MAI/Z-Image-Turbo"),
        "prompt": prompt.strip(),
    }
    if width and height:
        payload["width"] = width
        payload["height"] = height
        payload["size"] = f"{width}x{height}"
    if refs:
        payload["image_url"] = refs

    base_root = ((provider or {}).get("base_url") or config.MODELSCOPE_CHAT_BASE_URL).rstrip("/")
    api_root = base_root if base_root.endswith("/v1") else f"{base_root}/v1"
    async with httpx.AsyncClient(timeout=config.AI_REQUEST_TIMEOUT) as client:
        submit_res = await client.post(f"{api_root}/images/generations", headers=headers, json=payload)
        submit_res.raise_for_status()
        raw = submit_res.json()
        task_id = raw.get("task_id")
        if not task_id:
            try:
                return extract_image(raw), raw
            except HTTPException:
                raise HTTPException(status_code=502, detail=f"ModelScope 未返回 task_id：{raw}")

        deadline = time.monotonic() + config.AI_REQUEST_TIMEOUT
        last_payload = raw
        while time.monotonic() < deadline:
            await asyncio.sleep(config.IMAGE_POLL_INTERVAL)
            result = await client.get(
                f"{api_root}/tasks/{task_id}",
                headers={**headers, "X-ModelScope-Task-Type": "image_generation"},
            )
            result.raise_for_status()
            data = result.json()
            last_payload = data
            status = str(data.get("task_status") or "").upper()
            if status == "SUCCEED":
                images = data.get("output_images") or []
                if not images:
                    raise HTTPException(status_code=502, detail=f"ModelScope 成功但没有返回图片：{data}")
                return {"type": "url", "value": images[0]}, data
            if status in {"FAILED", "FAIL", "ERROR", "CANCELED", "CANCELLED", "TIMEOUT", "REVOKED"}:
                detail = data.get("error_info") or data.get("message") or data.get("detail") or str(data)
                raise HTTPException(status_code=502, detail=f"ModelScope 任务失败：{detail}")
        raise HTTPException(status_code=504, detail=f"ModelScope 生图任务超时：{last_payload}")


# --- 通用生图入口 ---

async def generate_ai_image(prompt, size, quality, model, reference_images=None, provider_id="comfly"):
    provider = providers.get_api_provider(provider_id)
    if provider["id"] == "modelscope":
        return await generate_modelscope_provider_image(prompt, size, model, reference_images, provider)
    is_gpt2 = imageproc.is_gpt_image_2_model(model)
    is_apimart = providers.is_apimart_provider(provider)
    if imageproc.is_gpt_image_2_model(model) and not is_apimart:
        size = imageproc.normalize_gpt_image_2_size(size)
    base_url = (provider.get("base_url") or config.AI_BASE_URL).rstrip("/")
    if not base_url:
        raise HTTPException(status_code=400, detail=f"{provider.get('name') or provider['id']} 未配置 Base URL")
    gen_url = f"{base_url}/images/generations" if base_url.endswith("/v1") else f"{base_url}/v1/images/generations"
    edit_url = f"{base_url}/images/edits" if base_url.endswith("/v1") else f"{base_url}/v1/images/edits"
    refs = [ref for ref in (reference_images or []) if ref.get("url")]
    mask_refs = [ref for ref in refs if str(ref.get("role") or "").strip().lower() == "mask" or str(ref.get("name") or "").lower().endswith("_mask.png")]
    image_refs = [ref for ref in refs if ref not in mask_refs]
    request_timeout = httpx.Timeout(connect=20.0, read=600.0, write=120.0, pool=20.0) if (is_gpt2 or is_apimart) else config.AI_REQUEST_TIMEOUT
    async with httpx.AsyncClient(timeout=request_timeout) as client:
        response = None
        if is_apimart:
            apimart_size, resolution = imageproc.apimart_size_resolution(size)
            body = {
                "model": model,
                "prompt": prompt,
                "n": 1,
                "size": apimart_size,
                "resolution": resolution.upper(),
                "official_fallback": False,
            }
            if image_refs:
                body["image_urls"] = [imageproc.reference_to_data_url(ref, max_size=1536) for ref in image_refs[:14]]
            response = await client.post(gen_url, headers=providers.api_headers(provider=provider), json=body)
        elif is_gpt2 and not mask_refs:
            body = {"model": model, "prompt": prompt, "size": size}
            if quality:
                body["quality"] = quality
            if image_refs:
                body["image"] = [imageproc.reference_to_data_url(ref, max_size=1536) for ref in image_refs[:4]]
            response = await client.post(gen_url, headers=providers.api_headers(provider=provider), json=body)
        elif image_refs:
            # 1) 优先用 multipart 提交到 /images/edits（OpenAI / Comfly 风格）
            files = []
            opened = []
            edit_failed_status = None
            edit_failed_text = ""
            try:
                for ref in image_refs[:4]:
                    path = imageproc.output_file_from_url(ref.get("url", ""))
                    if not path:
                        continue
                    fh = open(path, "rb")
                    opened.append(fh)
                    files.append(("image", (os.path.basename(path), fh, imageproc.content_type_for_path(path))))
                if mask_refs:
                    mask_path = imageproc.output_file_from_url(mask_refs[0].get("url", ""))
                    if mask_path:
                        fh = open(mask_path, "rb")
                        opened.append(fh)
                        files.append(("mask", (os.path.basename(mask_path), fh, imageproc.content_type_for_path(mask_path))))
                data = {"model": model, "prompt": prompt, "size": size, "quality": quality, "response_format": "url", "n": "1"}
                try:
                    response = await client.post(edit_url, headers=providers.api_headers(json_body=False, provider=provider), data=data, files=files)
                    if response.status_code >= 400:
                        edit_failed_status = response.status_code
                        edit_failed_text = response.text[:500]
                        response = None
                except httpx.HTTPError as e:
                    edit_failed_status = -1
                    edit_failed_text = str(e)
                    response = None
            finally:
                for fh in opened:
                    fh.close()
            # 2) edits 失败 → 回退到 /images/generations + JSON image:[urls/base64]（grsai 风格）
            if response is None:
                print(f"/images/edits failed ({edit_failed_status}): {edit_failed_text[:200]} → 回退到 /images/generations + image:[] JSON")
                image_payload = [imageproc.reference_to_data_url(ref, max_size=1536) for ref in image_refs[:4]]
                body = {
                    "model": model, "prompt": prompt, "size": size,
                    "quality": quality, "response_format": "url", "n": 1,
                    "image": image_payload,
                }
                response = await client.post(gen_url, headers=providers.api_headers(provider=provider), json=body)
        else:
            response = await client.post(
                gen_url,
                headers=providers.api_headers(provider=provider),
                json={"model": model, "prompt": prompt, "size": size, "quality": quality, "response_format": "url", "n": 1},
            )
        response.raise_for_status()
        raw = response.json()
        try:
            return extract_image(raw), raw
        except HTTPException:
            task_id = extract_task_id(raw)
            if not task_id:
                raise
        task_result = await wait_for_image_task(client, task_id, provider)
        return extract_image(task_result), task_result


# --- 视频 ---

def video_output_urls(raw):
    data = raw.get("data") if isinstance(raw, dict) else {}
    if isinstance(data, list) and data:
        data = data[0] if isinstance(data[0], dict) else {}
    if not isinstance(data, dict):
        data = {}
    urls = []
    result = data.get("result") if isinstance(data.get("result"), dict) else raw.get("result") if isinstance(raw, dict) and isinstance(raw.get("result"), dict) else {}
    output = data.get("output") or raw.get("output")
    outputs = data.get("outputs") or raw.get("outputs") or []
    videos = result.get("videos") or data.get("videos") or raw.get("videos") or []
    if isinstance(output, str) and output:
        urls.append(output)
    if isinstance(outputs, list):
        for item in outputs:
            if isinstance(item, str) and item:
                urls.append(item)
            elif isinstance(item, dict):
                value = item.get("url") or item.get("output")
                if value:
                    urls.extend(value if isinstance(value, list) else [value])
    if isinstance(videos, list):
        for item in videos:
            if isinstance(item, str) and item:
                urls.append(item)
            elif isinstance(item, dict):
                value = item.get("url") or item.get("video_url") or item.get("output")
                if value:
                    urls.extend(value if isinstance(value, list) else [value])
    elif isinstance(videos, str) and videos:
        urls.append(videos)
    deduped = []
    for url in urls:
        if isinstance(url, str) and url and url not in deduped:
            deduped.append(url)
    return deduped


def video_api_root(provider) -> str:
    base_url = (provider.get("base_url") or config.AI_BASE_URL).rstrip("/")
    if base_url.endswith("/v1") or base_url.endswith("/v2"):
        base_url = base_url.rsplit("/", 1)[0]
    return base_url


async def wait_for_video_task(client, provider, task_id):
    base_url = video_api_root(provider)
    if not base_url:
        raise HTTPException(status_code=400, detail=f"{provider.get('name') or provider['id']} 未配置 Base URL")
    if providers.is_apimart_provider(provider):
        task_path = f"{base_url}/tasks/{task_id}" if base_url.endswith("/v1") else f"{base_url}/v1/tasks/{task_id}"
        task_url = f"{task_path}?language=zh"
    else:
        task_url = f"{base_url}/v2/videos/generations/{task_id}"
    deadline = time.monotonic() + config.VIDEO_POLL_TIMEOUT
    delay = max(2.0, config.IMAGE_POLL_INTERVAL)
    last_payload = {}
    while time.monotonic() < deadline:
        await asyncio.sleep(delay)
        response = await client.get(task_url, headers=providers.api_headers(provider=provider))
        response.raise_for_status()
        raw = response.json()
        last_payload = raw
        task_data = raw.get("data") if isinstance(raw.get("data"), dict) else raw
        status = str(task_data.get("status") or raw.get("status") or "").upper()
        if status in {"SUCCESS", "COMPLETED"}:
            return raw
        if status in {"FAILURE", "FAILED", "FAIL", "ERROR", "CANCELED", "CANCELLED", "TIMEOUT"}:
            error = task_data.get("error") if isinstance(task_data.get("error"), dict) else {}
            reason = task_data.get("fail_reason") or error.get("message") or raw.get("error") or raw.get("message") or str(raw)
            raise HTTPException(status_code=502, detail=f"视频生成任务失败：{reason}")
        delay = min(delay * 1.6, 12)
    raise HTTPException(status_code=504, detail=f"视频生成任务超时：{last_payload or task_id}")


def apimart_video_size(size) -> str:
    value = str(size or "16:9").strip()
    if value == "keep_ratio":
        return "adaptive"
    allowed = {"16:9", "9:16", "1:1", "4:3", "3:4", "21:9", "adaptive"}
    return value if value in allowed else "16:9"
