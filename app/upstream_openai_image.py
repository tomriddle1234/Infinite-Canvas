"""OpenAI first-party image generation helpers for dedicated canvas nodes."""

import os

import httpx
from fastapi import HTTPException

from . import config, imageproc


def _api_key() -> str:
    key = (config.OPENAI_API_KEY or "").strip()
    if not key:
        raise HTTPException(status_code=400, detail="未配置 OPENAI_API_KEY，请先在 API 设置中填写。")
    return key


def _headers() -> dict:
    return {"Authorization": f"Bearer {_api_key()}", "Accept": "application/json"}


def _image_result(item: dict) -> dict:
    if item.get("b64_json"):
        return {"type": "b64", "value": item["b64_json"]}
    if item.get("url"):
        return {"type": "url", "value": item["url"]}
    raise HTTPException(status_code=502, detail=f"OpenAI 图片接口未返回图片数据：{item}")


def _local_image_files(refs):
    files = []
    for index, ref in enumerate(refs or []):
        url = ref.get("url") if isinstance(ref, dict) else getattr(ref, "url", "")
        path = imageproc.output_file_from_url(url)
        if not path:
            continue
        name = os.path.basename(path) or f"reference_{index}.png"
        files.append(("image", (name, open(path, "rb"), imageproc.content_type_for_path(path))))
    return files


async def generate_gpt_image_2(prompt: str, size: str, quality: str, refs=None) -> tuple[dict, dict]:
    """Return one image item and the raw OpenAI response."""
    base_url = config.OPENAI_API_BASE_URL.rstrip("/")
    timeout = httpx.Timeout(connect=20.0, read=config.IMAGE_TASK_TIMEOUT, write=120.0, pool=20.0)
    async with httpx.AsyncClient(timeout=timeout) as client:
        if refs:
            files = _local_image_files(refs)
            if not files:
                raise HTTPException(status_code=400, detail="参考图必须是画布中的本地图片或输出图片。")
            try:
                data = {"model": "gpt-image-2", "prompt": prompt, "size": size or "1024x1024"}
                if quality:
                    data["quality"] = quality
                resp = await client.post(f"{base_url}/images/edits", headers=_headers(), data=data, files=files)
            finally:
                for _, file_tuple in files:
                    file_tuple[1].close()
        else:
            body = {"model": "gpt-image-2", "prompt": prompt, "size": size or "1024x1024"}
            if quality:
                body["quality"] = quality
            resp = await client.post(f"{base_url}/images/generations", headers={**_headers(), "Content-Type": "application/json"}, json=body)
        resp.raise_for_status()
        raw = resp.json()
    data = raw.get("data") or []
    if not data:
        raise HTTPException(status_code=502, detail=f"OpenAI 图片接口未返回 data：{raw}")
    return _image_result(data[0]), raw
