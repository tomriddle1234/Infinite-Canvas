"""上游 LLM/图像/视频接口的协议相关工具与高层调用。

包含：
- 响应解析（apimart 包装、chat 文本提取、image/task 提取）
- 通用图像生成 ``generate_ai_image`` 与 ModelScope 异步任务等待
- 视频接口请求构造和轮询
"""

import asyncio
import base64
import mimetypes
import os
import time
from io import BytesIO

import httpx
from fastapi import HTTPException
from PIL import Image

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


def images_api_unsupported(response):
    text = str(getattr(response, "text", "") or "").lower()
    return "images api is not supported" in text or "not supported for this platform" in text


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
        status = str(task_data.get("status") or task_data.get("task_status") or "").upper()
        if status in {"SUCCESS", "SUCCEED", "SUCCEEDED", "COMPLETED", "COMPLETE", "DONE", "FINISHED", "OK", "READY"}:
            return last_payload
        if status in {"FAILURE", "FAILED", "FAIL", "ERROR", "ERRORED", "CANCELED", "CANCELLED", "TIMEOUT", "REJECTED", "EXPIRED"}:
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

def valid_apimart_video_image_input(value: str) -> bool:
    if not isinstance(value, str):
        return False
    value = value.strip()
    return value.startswith("http://") or value.startswith("https://") or value.startswith("asset://")


def apimart_upload_file_payload(path: str):
    max_bytes = 9_500_000
    if os.path.getsize(path) <= max_bytes:
        with open(path, "rb") as handle:
            return os.path.basename(path), handle.read(), imageproc.content_type_for_path(path)
    with Image.open(path) as img:
        base = img.convert("RGBA")
        bg = Image.new("RGB", base.size, (255, 255, 255))
        bg.paste(base, mask=base.split()[-1])
        quality = 92
        while quality >= 62:
            buf = BytesIO()
            bg.save(buf, format="JPEG", quality=quality, optimize=True)
            data = buf.getvalue()
            if len(data) <= max_bytes:
                return os.path.splitext(os.path.basename(path))[0] + ".jpg", data, "image/jpeg"
            quality -= 8
    raise ValueError("图片超过 10MB，且压缩后仍无法满足 VEO3.1 图片限制")


def apimart_upload_payload_from_bytes(data: bytes, mime: str, name_hint: str = "image"):
    max_bytes = 9_500_000
    ext = mimetypes.guess_extension(mime or "image/png") or ".png"
    if len(data) <= max_bytes and (mime or "").lower() in {"image/png", "image/jpeg", "image/webp"}:
        return f"{name_hint}{ext}", data, mime or "image/png"
    with Image.open(BytesIO(data)) as img:
        has_alpha = img.mode in ("RGBA", "LA") or (img.mode == "P" and "transparency" in img.info)
        if has_alpha:
            base = img.convert("RGBA")
            bg = Image.new("RGB", base.size, (255, 255, 255))
            bg.paste(base, mask=base.split()[-1])
            target = bg
        else:
            target = img.convert("RGB")
        quality = 92
        while quality >= 62:
            buf = BytesIO()
            target.save(buf, format="JPEG", quality=quality, optimize=True)
            payload = buf.getvalue()
            if len(payload) <= max_bytes:
                return f"{name_hint}.jpg", payload, "image/jpeg"
            quality -= 8
    raise ValueError("data URL 图片超过 10MB，且压缩后仍无法满足 APIMart 限制")


def extract_apimart_asset_url(payload):
    if isinstance(payload, list):
        for item in payload:
            found = extract_apimart_asset_url(item)
            if found:
                return found
        return ""
    if not isinstance(payload, dict):
        return ""
    for key in ("url", "asset_url", "assetUrl", "uri", "file_url", "fileUrl"):
        value = str(payload.get(key) or "").strip()
        if valid_apimart_video_image_input(value):
            return value
    for key in ("asset_id", "assetId", "file_id", "fileId", "id"):
        value = str(payload.get(key) or "").strip()
        if value:
            return value if value.startswith("asset://") else f"asset://{value}"
    for key in ("data", "file", "asset", "result"):
        found = extract_apimart_asset_url(payload.get(key))
        if found:
            return found
    return ""


async def upload_image_for_apimart(client, provider, ref_url: str) -> str:
    ref_url = str(ref_url or "").strip()
    if not ref_url:
        return "ERR:空地址"
    if valid_apimart_video_image_input(ref_url):
        return ref_url
    upload_url = f"{video_api_root(provider)}/v1/uploads/images"
    if ref_url.startswith("data:"):
        try:
            if ";base64," not in ref_url:
                return "ERR:不支持的 data URL（缺少 base64 段）"
            header, encoded = ref_url.split(";base64,", 1)
            mime = header.split(":", 1)[1].split(";", 1)[0] if ":" in header else "image/png"
            filename, content, ct = apimart_upload_payload_from_bytes(base64.b64decode(encoded), mime, "canvas_image")
            resp = await client.post(upload_url, headers=providers.api_headers(json_body=False, provider=provider), files={"file": (filename, content, ct)}, timeout=60)
            if resp.status_code in (200, 201):
                url = extract_apimart_asset_url(resp.json())
                if valid_apimart_video_image_input(url):
                    return url
                return "ERR:APIMart 上传响应未包含可用 URL"
            return f"ERR:APIMart 上传失败({resp.status_code})"
        except ValueError as exc:
            return f"ERR:{exc}"
        except Exception as exc:
            print(f"APIMart 上传 data URL 异常: {exc}")
            return f"ERR:上传异常 {exc}"
    if ref_url.startswith("/output/") or ref_url.startswith("/assets/"):
        path = imageproc.output_file_from_url(ref_url)
        if not path:
            return "ERR:本地文件不存在或已被删除"
        try:
            filename, content, ct = apimart_upload_file_payload(path)
            resp = await client.post(upload_url, headers=providers.api_headers(json_body=False, provider=provider), files={"file": (filename, content, ct)}, timeout=60)
            if resp.status_code in (200, 201):
                url = extract_apimart_asset_url(resp.json())
                if valid_apimart_video_image_input(url):
                    return url
                return "ERR:APIMart 上传响应未包含可用 URL"
            return f"ERR:APIMart 上传失败({resp.status_code})"
        except ValueError as exc:
            return f"ERR:{exc}"
        except Exception as exc:
            print(f"APIMart 文件上传异常: {exc}")
            return f"ERR:上传异常 {exc}"
    return "ERR:不支持的图片来源（仅支持 http/https/asset/data 或本地 /output/ /assets/ 路径）"


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
    quality = str(quality or "").strip().lower()
    if quality not in {"low", "medium", "high"}:
        quality = ""
    if imageproc.is_gpt_image_2_model(model) and not is_apimart:
        size = imageproc.normalize_gpt_image_2_size(size)
    base_url = (provider.get("base_url") or config.AI_BASE_URL).rstrip("/")
    if not base_url:
        raise HTTPException(status_code=400, detail=f"{provider.get('name') or provider['id']} 未配置 Base URL")
    gen_url = providers.provider_endpoint_url(provider, "image_generation_endpoint", "/v1/images/generations")
    edit_url = providers.provider_endpoint_url(provider, "image_edit_endpoint", "/v1/images/edits")
    refs = [ref for ref in (reference_images or []) if ref.get("url")]
    mask_refs = [ref for ref in refs if str(ref.get("role") or "").strip().lower() == "mask" or str(ref.get("name") or "").lower().endswith("_mask.png")]
    image_refs = [ref for ref in refs if ref not in mask_refs]
    request_timeout = httpx.Timeout(connect=20.0, read=600.0, write=120.0, pool=20.0) if (is_gpt2 or is_apimart) else config.AI_REQUEST_TIMEOUT
    async with httpx.AsyncClient(timeout=request_timeout) as client:
        response = None
        async def post_openai_edits(edit_files=None):
            data = {"model": model, "prompt": prompt, "size": size}
            if quality:
                data["quality"] = quality
            return await client.post(
                edit_url,
                headers=providers.api_headers(json_body=False, provider=provider),
                data=data,
                files=edit_files if edit_files is not None else {},
            )

        if is_apimart:
            apimart_size, resolution = imageproc.apimart_size_resolution(size)
            body = {
                "model": model,
                "prompt": prompt,
                "n": 1,
                "size": apimart_size,
                "resolution": resolution,
                "official_fallback": False,
            }
            if image_refs:
                body["image_urls"] = [imageproc.reference_to_data_url(ref, max_size=1536) for ref in image_refs[:16]]
            response = await client.post(gen_url, headers=providers.api_headers(provider=provider), json=body)
        elif is_gpt2 and not image_refs and not mask_refs:
            body = {"model": model, "prompt": prompt, "size": size}
            if quality:
                body["quality"] = quality
            response = await client.post(gen_url, headers=providers.api_headers(provider=provider), json=body)
            if response.status_code >= 400 and images_api_unsupported(response):
                response = await post_openai_edits()
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
                try:
                    response = await post_openai_edits(files)
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
                if is_gpt2:
                    raise HTTPException(
                        status_code=502,
                        detail=f"GPT-Image-2 编辑接口 /images/edits 调用失败：{edit_failed_text[:300] or edit_failed_status}",
                    )
                print(f"/images/edits failed ({edit_failed_status}): {edit_failed_text[:200]} → 回退到 /images/generations + image:[] JSON")
                image_payload = [imageproc.reference_to_data_url(ref, max_size=1536) for ref in image_refs[:4]]
                body = {
                    "model": model, "prompt": prompt, "size": size,
                    "response_format": "url", "n": 1,
                    "image": image_payload,
                }
                if quality:
                    body["quality"] = quality
                response = await client.post(gen_url, headers=providers.api_headers(provider=provider), json=body)
                if response.status_code >= 400 and images_api_unsupported(response):
                    raise HTTPException(
                        status_code=502,
                        detail=f"编辑接口 /images/edits 调用失败，且该平台不支持 /images/generations：{edit_failed_text[:300] or edit_failed_status}",
                    )
        else:
            body = {"model": model, "prompt": prompt, "size": size, "response_format": "url", "n": 1}
            if quality:
                body["quality"] = quality
            response = await client.post(
                gen_url,
                headers=providers.api_headers(provider=provider),
                json=body,
            )
            if response.status_code >= 400 and images_api_unsupported(response):
                response = await post_openai_edits()
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


VIDEO_URL_KEYS = (
    "url", "video_url", "videoUrl", "mp4_url", "mp4Url",
    "output", "output_url", "outputUrl", "download_url", "downloadUrl",
    "video", "src", "uri", "preview_url", "previewUrl",
)


def _collect_video_url(value, urls):
    if not value:
        return
    if isinstance(value, str):
        if value.startswith("http://") or value.startswith("https://") or value.startswith("/output/") or value.startswith("/assets/"):
            urls.append(value)
        return
    if isinstance(value, list):
        for item in value:
            _collect_video_url(item, urls)
        return
    if isinstance(value, dict):
        for key in VIDEO_URL_KEYS:
            if key in value:
                _collect_video_url(value.get(key), urls)


def video_output_urls(raw):
    urls = []
    if not isinstance(raw, dict):
        return urls
    candidates = [raw]
    data = raw.get("data")
    if isinstance(data, dict):
        candidates.append(data)
    elif isinstance(data, list):
        candidates.extend(item for item in data if isinstance(item, dict))
    for node in list(candidates):
        result = node.get("result") if isinstance(node, dict) else None
        if isinstance(result, dict):
            candidates.append(result)
        elif isinstance(result, list):
            candidates.extend(item for item in result if isinstance(item, dict))
    for node in candidates:
        if not isinstance(node, dict):
            continue
        for key in ("videos", "outputs"):
            _collect_video_url(node.get(key), urls)
        for key in VIDEO_URL_KEYS:
            if key in node:
                _collect_video_url(node.get(key), urls)
    deduped = []
    for url in urls:
        if isinstance(url, str) and url and url not in deduped:
            deduped.append(url)
    return deduped


VIDEO_TASK_SUCCESS_STATUSES = {
    "SUCCESS", "SUCCEED", "SUCCEEDED", "COMPLETED", "COMPLETE",
    "DONE", "FINISHED", "FINISH", "OK", "READY",
}
VIDEO_TASK_FAILURE_STATUSES = {
    "FAILURE", "FAILED", "FAIL", "ERROR", "ERRORED",
    "CANCELED", "CANCELLED", "TIMEOUT", "TIMEDOUT", "REJECTED", "EXPIRED",
}


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
        status = str(task_data.get("status") or task_data.get("task_status") or raw.get("status") or raw.get("task_status") or "").upper()
        if status in VIDEO_TASK_SUCCESS_STATUSES:
            return raw
        if not status and video_output_urls(raw):
            return raw
        if status in VIDEO_TASK_FAILURE_STATUSES:
            error = task_data.get("error") if isinstance(task_data.get("error"), dict) else {}
            reason = task_data.get("fail_reason") or task_data.get("message") or error.get("message") or raw.get("error") or raw.get("message") or str(raw)
            raise HTTPException(status_code=502, detail=f"视频生成任务失败：{reason}")
        delay = min(delay * 1.6, 12)
    raise HTTPException(status_code=504, detail=f"视频生成任务超时：{last_payload or task_id}")


def is_apimart_veo31_model(model: str) -> bool:
    return str(model or "").strip().lower().startswith("veo3.1")


def apimart_veo31_model(model: str) -> str:
    value = str(model or "").strip().lower()
    aliases = {
        "veo3.1": "veo3.1-fast",
        "veo3.1-pro": "veo3.1-quality",
        "veo3.1-preview": "veo3.1-fast",
    }
    value = aliases.get(value, value or "veo3.1-fast")
    allowed = {"veo3.1-fast", "veo3.1-quality", "veo3.1-lite"}
    return value if value in allowed else "veo3.1-fast"


def apimart_veo31_aspect(aspect: str) -> str:
    value = str(aspect or "16:9").strip()
    return value if value in {"16:9", "9:16"} else "16:9"


def apimart_veo31_resolution(resolution: str) -> str:
    value = str(resolution or "").strip().lower()
    aliases = {"": "720p", "auto": "720p", "480p": "720p", "780p": "720p", "1080": "1080p", "4k": "4k"}
    value = aliases.get(value, value)
    return value if value in {"720p", "1080p", "4k"} else "720p"


def apimart_video_size(size) -> str:
    value = str(size or "16:9").strip()
    if value == "keep_ratio":
        return "adaptive"
    allowed = {"16:9", "9:16", "1:1", "4:3", "3:4", "21:9", "adaptive"}
    return value if value in allowed else "16:9"
