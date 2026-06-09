"""Volcengine Ark portrait asset library helpers.

This module browses and previews existing real-person portrait assets.
It does not create, upload, verify, update, or delete portrait assets.
Preset virtual portraits are platform-provided media asset groups exposed by
the Ark experience center. They are fetched from Volcengine over the network;
do not maintain a local JSON catalog for this library.
"""

import datetime
import hashlib
import hmac
import json
import mimetypes
import os
import re
import urllib.parse
from typing import Any, Dict, List, Optional

import httpx
from fastapi import HTTPException

from . import config


VOLCENGINE_ARK_ASSET_SERVICE = "ark"
VOLCENGINE_ARK_ASSET_VERSION = "2024-01-01"
GROUP_TYPES = {"LivenessFace"}
ASSET_STATUSES = {"Active", "Processing", "Failed"}
PRESET_VIRTUAL_PORTRAIT_FILTER = {
    "Field": "metadata.type",
    "Op": "must",
    "Conds": {"StrValues": ["portrait"]},
}


def _project_name(project_name: str = "") -> str:
    return (project_name or config.VOLCENGINE_PROJECT_NAME or "default").strip() or "default"


def _region(region: str = "") -> str:
    return (region or config.VOLCENGINE_REGION or "cn-beijing").strip() or "cn-beijing"


def _asset_host(region: str = "") -> str:
    return f"ark.{_region(region)}.volcengineapi.com"


def normalize_group_type(value: str = "") -> str:
    text = (value or "LivenessFace").strip()
    if text not in GROUP_TYPES:
        raise HTTPException(status_code=400, detail=f"不支持的人像库类型：{text}。预置虚拟人像请从火山方舟体验中心复制 asset ID/URI 后直接使用。")
    return text


def normalize_statuses(values: Optional[List[str]]) -> List[str]:
    result = []
    for value in values or []:
        text = str(value or "").strip()
        if text and text not in ASSET_STATUSES:
            raise HTTPException(status_code=400, detail=f"不支持的素材状态：{text}")
        if text and text not in result:
            result.append(text)
    return result


def asset_uri(asset_id: str) -> str:
    text = str(asset_id or "").strip()
    return text if text.startswith("asset://") else f"asset://{text}"


def _volc_hmac(key: bytes, msg: str) -> bytes:
    return hmac.new(key, msg.encode("utf-8"), hashlib.sha256).digest()


def sign_v4_headers(ak: str, sk: str, action: str, body_str: str, region: str = "") -> Dict[str, str]:
    method = "POST"
    content_type = "application/json"
    resolved_region = _region(region)
    host = _asset_host(resolved_region)
    now = datetime.datetime.now(datetime.timezone.utc)
    x_date = now.strftime("%Y%m%dT%H%M%SZ")
    short_date = x_date[:8]
    payload_hash = hashlib.sha256(body_str.encode("utf-8")).hexdigest()
    canonical_query = (
        f"Action={urllib.parse.quote(action, safe='')}"
        f"&Version={urllib.parse.quote(VOLCENGINE_ARK_ASSET_VERSION, safe='')}"
    )
    canonical_headers = (
        f"content-type:{content_type}\n"
        f"host:{host}\n"
        f"x-content-sha256:{payload_hash}\n"
        f"x-date:{x_date}\n"
    )
    signed_headers = "content-type;host;x-content-sha256;x-date"
    canonical_request = "\n".join([method, "/", canonical_query, canonical_headers, signed_headers, payload_hash])
    algorithm = "HMAC-SHA256"
    credential_scope = f"{short_date}/{resolved_region}/{VOLCENGINE_ARK_ASSET_SERVICE}/request"
    string_to_sign = "\n".join([
        algorithm,
        x_date,
        credential_scope,
        hashlib.sha256(canonical_request.encode("utf-8")).hexdigest(),
    ])
    k_date = _volc_hmac(sk.encode("utf-8"), short_date)
    k_region = _volc_hmac(k_date, resolved_region)
    k_service = _volc_hmac(k_region, VOLCENGINE_ARK_ASSET_SERVICE)
    k_signing = _volc_hmac(k_service, "request")
    signature = hmac.new(k_signing, string_to_sign.encode("utf-8"), hashlib.sha256).hexdigest()
    return {
        "Content-Type": content_type,
        "Host": host,
        "X-Date": x_date,
        "X-Content-Sha256": payload_hash,
        "Authorization": (
            f"{algorithm} Credential={ak}/{credential_scope}, "
            f"SignedHeaders={signed_headers}, Signature={signature}"
        ),
    }


async def asset_call(action: str, body: Dict[str, Any]) -> Dict[str, Any]:
    ak = (config.VOLCENGINE_ACCESS_KEY_ID or "").strip()
    sk = (config.VOLCENGINE_SECRET_ACCESS_KEY or "").strip()
    if not ak or not sk:
        raise HTTPException(status_code=400, detail="未配置火山引擎 AK/SK，请先在 API 设置中填写 Access Key ID / Secret Access Key。")
    resolved_region = _region()
    host = _asset_host(resolved_region)
    body_str = json.dumps(body, ensure_ascii=False, separators=(",", ":"))
    headers = sign_v4_headers(ak, sk, action, body_str, resolved_region)
    url = (
        f"https://{host}/?Action={urllib.parse.quote(action, safe='')}"
        f"&Version={urllib.parse.quote(VOLCENGINE_ARK_ASSET_VERSION, safe='')}"
    )
    try:
        async with httpx.AsyncClient(timeout=60) as client:
            resp = await client.post(url, headers=headers, content=body_str.encode("utf-8"))
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=502, detail=f"请求火山 {action} 失败：{str(exc)[:300]}") from exc
    try:
        payload = resp.json()
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"火山 {action} 返回非 JSON（{resp.status_code}）：{resp.text[:300]}") from exc
    meta = payload.get("ResponseMetadata") if isinstance(payload, dict) else None
    err = meta.get("Error") if isinstance(meta, dict) else None
    if isinstance(err, dict):
        code = err.get("Code") or err.get("CodeN") or ""
        msg = err.get("Message") or ""
        error_text = f"{code} {msg}".lower()
        status_code = 403 if "subscriptionrequired" in error_text or "subscription" in error_text else 502
        raise HTTPException(status_code=status_code, detail=f"火山 {action} 失败：{code} {msg}".strip())
    if resp.status_code not in (200, 201):
        raise HTTPException(status_code=502, detail=f"火山 {action} 失败（{resp.status_code}）：{resp.text[:300]}")
    result = payload.get("Result") if isinstance(payload, dict) else None
    return result if isinstance(result, dict) else (payload if isinstance(payload, dict) else {})


def _page(value, default=1, maximum=100):
    try:
        number = int(value)
    except Exception:
        number = default
    return max(1, min(maximum, number))


def _normalize_group(item: Dict[str, Any]) -> Dict[str, Any]:
    group_id = str(item.get("Id") or item.get("GroupId") or "").strip()
    return {
        "id": group_id,
        "name": str(item.get("Name") or item.get("Title") or group_id or "未命名素材组"),
        "title": str(item.get("Title") or item.get("Name") or ""),
        "description": str(item.get("Description") or ""),
        "group_type": str(item.get("GroupType") or ""),
        "project_name": str(item.get("ProjectName") or ""),
        "create_time": str(item.get("CreateTime") or ""),
        "update_time": str(item.get("UpdateTime") or ""),
    }


def _normalize_asset(item: Dict[str, Any]) -> Dict[str, Any]:
    asset_id = str(item.get("Id") or item.get("AssetId") or "").strip()
    name = str(item.get("Name") or item.get("Title") or asset_id or "未命名素材")
    url = str(item.get("URL") or item.get("Url") or item.get("url") or item.get("PreviewURL") or "")
    return {
        "id": asset_id,
        "asset_id": asset_id,
        "asset_uri": asset_uri(asset_id) if asset_id else "",
        "name": name,
        "title": str(item.get("Title") or ""),
        "description": str(item.get("Description") or ""),
        "asset_type": str(item.get("AssetType") or ""),
        "group_id": str(item.get("GroupId") or ""),
        "group_type": str(item.get("GroupType") or ""),
        "status": str(item.get("Status") or ""),
        "project_name": str(item.get("ProjectName") or ""),
        "create_time": str(item.get("CreateTime") or ""),
        "update_time": str(item.get("UpdateTime") or ""),
        "url": url,
        "preview_url": f"/api/volcengine/assets/preview/{urllib.parse.quote(asset_id, safe='')}" if asset_id else "",
    }


def _first_media(content: Dict[str, Any]) -> tuple[str, Dict[str, Any]]:
    images = content.get("Image") if isinstance(content.get("Image"), list) else []
    videos = content.get("Video") if isinstance(content.get("Video"), list) else []
    if images and isinstance(images[0], dict):
        return "Image", images[0]
    if videos and isinstance(videos[0], dict):
        return "Video", videos[0]
    return "Image", {}


def _normalize_preset_virtual_asset(item: Dict[str, Any]) -> Dict[str, Any]:
    group = item.get("AssetGroup") if isinstance(item.get("AssetGroup"), dict) else item
    content = group.get("Content") if isinstance(group.get("Content"), dict) else {}
    asset_type, media = _first_media(content)
    asset_id = str(
        media.get("AssetID")
        or media.get("AssetId")
        or item.get("SID")
        or group.get("SID")
        or item.get("asset_id")
        or item.get("AssetId")
        or item.get("id")
        or item.get("Id")
        or ""
    ).strip()
    name = str(
        group.get("Name")
        or group.get("Title")
        or item.get("Name")
        or item.get("Title")
        or asset_id
        or "Preset virtual portrait"
    )
    tags = group.get("Tags") or item.get("Tags") or item.get("tags") or []
    if isinstance(tags, str):
        tags = [part.strip() for part in re.split(r"[,\s]+", tags) if part.strip()]
    if not isinstance(tags, list):
        tags = []
    preview_url = str(
        item.get("preview_url")
        or item.get("PreviewURL")
        or item.get("CoverURL")
        or group.get("CoverURL")
        or media.get("CoverURL")
        or media.get("URL")
        or item.get("url")
        or item.get("URL")
        or ""
    )
    asset_uri_value = str(media.get("AssetURI") or group.get("AssetURI") or item.get("AssetURI") or "").strip()
    metadata = group.get("Metadata") if isinstance(group.get("Metadata"), dict) else {}
    return {
        "id": asset_id,
        "asset_id": asset_id,
        "asset_uri": asset_uri_value or (asset_uri(asset_id) if asset_id else ""),
        "name": name,
        "title": str(group.get("Title") or item.get("Title") or ""),
        "description": str(group.get("Description") or item.get("Description") or ""),
        "tags": [str(tag) for tag in tags],
        "metadata": metadata,
        "source": "volcengine_preset_virtual",
        "asset_type": asset_type,
        "score": item.get("Score"),
        "preview_url": preview_url or (f"/api/volcengine/assets/preview/{urllib.parse.quote(asset_id, safe='')}" if asset_id else ""),
    }


async def search_preset_virtual_portraits(query: str = "", page: int = 1, page_size: int = 24) -> Dict[str, Any]:
    clean_page = _page(page)
    clean_page_size = _page(page_size, default=24)
    clean_query = (query or "").strip()
    body: Dict[str, Any] = {
        "Query": {"Text": clean_query} if clean_query else {},
        "Filters": [PRESET_VIRTUAL_PORTRAIT_FILTER],
        "PageNum": clean_page,
        "PageSize": clean_page_size,
        "ProjectName": _project_name(),
    }
    if not clean_query:
        body["SortBy"] = "score"
        body["SortOrder"] = "desc"
    raw = await asset_call("ListMediaAssetGroup", body)
    raw_items = raw.get("Items") or []
    page_items = [
        _normalize_preset_virtual_asset(item)
        for item in raw_items
        if isinstance(item, dict)
    ]
    return {
        "items": page_items,
        "total": int(raw.get("Total") or raw.get("TotalCount") or len(page_items)),
        "page": int(raw.get("PageNum") or raw.get("PageNumber") or clean_page),
        "page_size": int(raw.get("PageSize") or clean_page_size),
        "source": "volcengine_list_media_asset_group",
    }

async def list_asset_groups(group_type: str = "LivenessFace", query: str = "", page: int = 1, page_size: int = 20) -> Dict[str, Any]:
    filter_body: Dict[str, Any] = {"GroupType": normalize_group_type(group_type)}
    if (query or "").strip():
        filter_body["Name"] = query.strip()[:64]
    body = {
        "Filter": filter_body,
        "PageNumber": _page(page),
        "PageSize": _page(page_size, default=20),
        "ProjectName": _project_name(),
    }
    raw = await asset_call("ListAssetGroups", body)
    items = [_normalize_group(item) for item in raw.get("Items") or [] if isinstance(item, dict)]
    return {
        "items": items,
        "total": int(raw.get("TotalCount") or len(items)),
        "page": int(raw.get("PageNumber") or body["PageNumber"]),
        "page_size": int(raw.get("PageSize") or body["PageSize"]),
    }


async def list_assets(
    group_type: str = "LivenessFace",
    group_id: str = "",
    query: str = "",
    statuses: Optional[List[str]] = None,
    page: int = 1,
    page_size: int = 24,
) -> Dict[str, Any]:
    filter_body: Dict[str, Any] = {"GroupType": normalize_group_type(group_type)}
    if (group_id or "").strip():
        filter_body["GroupIds"] = [group_id.strip()]
    if (query or "").strip():
        filter_body["Name"] = query.strip()[:64]
    clean_statuses = normalize_statuses(statuses)
    if clean_statuses:
        filter_body["Statuses"] = clean_statuses
    body = {
        "Filter": filter_body,
        "PageNumber": _page(page),
        "PageSize": _page(page_size, default=24),
        "ProjectName": _project_name(),
    }
    raw = await asset_call("ListAssets", body)
    items = [_normalize_asset(item) for item in raw.get("Items") or [] if isinstance(item, dict)]
    return {
        "items": items,
        "total": int(raw.get("TotalCount") or len(items)),
        "page": int(raw.get("PageNumber") or body["PageNumber"]),
        "page_size": int(raw.get("PageSize") or body["PageSize"]),
    }


async def get_asset(asset_id: str, project_name: str = "") -> Dict[str, Any]:
    text = str(asset_id or "").replace("asset://", "").strip()
    if not text:
        raise HTTPException(status_code=400, detail="缺少素材 Asset ID")
    raw = await asset_call("GetAsset", {"Id": text, "ProjectName": _project_name(project_name)})
    return _normalize_asset(raw)


def _cache_file_for(asset_id: str, content_type: str = "", source_url: str = "") -> str:
    safe_id = re.sub(r"[^a-zA-Z0-9_.-]+", "_", str(asset_id or "").replace("asset://", "").strip())
    ext = ""
    if content_type:
        ext = mimetypes.guess_extension(content_type.split(";", 1)[0].strip()) or ""
    if not ext and source_url:
        ext = os.path.splitext(urllib.parse.urlparse(source_url).path)[1]
    if ext.lower() not in {".jpg", ".jpeg", ".png", ".webp", ".gif"}:
        ext = ".jpg"
    return os.path.join(config.VOLCENGINE_ASSET_CACHE_DIR, f"{safe_id}{ext}")


def _cached_url_for(path: str) -> str:
    rel = os.path.relpath(path, config.ASSETS_DIR).replace("\\", "/")
    return f"/assets/{rel}"


async def cached_asset_preview(asset_id: str, refresh: bool = False) -> str:
    text = str(asset_id or "").replace("asset://", "").strip()
    if not text:
        return ""
    os.makedirs(config.VOLCENGINE_ASSET_CACHE_DIR, exist_ok=True)
    for name in os.listdir(config.VOLCENGINE_ASSET_CACHE_DIR):
        if name.startswith(re.sub(r"[^a-zA-Z0-9_.-]+", "_", text) + "."):
            existing = os.path.join(config.VOLCENGINE_ASSET_CACHE_DIR, name)
            if os.path.isfile(existing) and not refresh:
                return _cached_url_for(existing)
    try:
        info = await get_asset(text)
        source_url = info.get("url") or ""
        if not source_url:
            return ""
        async with httpx.AsyncClient(timeout=45, follow_redirects=True) as client:
            resp = await client.get(source_url)
        if resp.status_code >= 400 or not resp.content:
            return ""
        content_type = resp.headers.get("content-type", "")
        path = _cache_file_for(text, content_type, source_url)
        with open(path, "wb") as handle:
            handle.write(resp.content)
        return _cached_url_for(path)
    except Exception:
        for name in os.listdir(config.VOLCENGINE_ASSET_CACHE_DIR):
            if name.startswith(re.sub(r"[^a-zA-Z0-9_.-]+", "_", text) + "."):
                existing = os.path.join(config.VOLCENGINE_ASSET_CACHE_DIR, name)
                if os.path.isfile(existing):
                    return _cached_url_for(existing)
        return ""
