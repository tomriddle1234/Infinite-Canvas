"""Public shell, media proxy, uploads, and history APIs."""

import os
import re
import urllib.parse
import uuid
import zipfile
from io import BytesIO
from typing import List

import requests
from fastapi import APIRouter, File, Form, HTTPException, UploadFile
from fastapi.responses import FileResponse, Response
from PIL import Image
from pydantic import BaseModel

from .. import comfyui, config, imageproc, store
from ..models import CanvasAssetCheckRequest, CanvasAssetDownloadRequest, DeleteHistoryRequest

router = APIRouter()


class NodeMediaDeleteRequest(BaseModel):
    url: str = ""


@router.get("/")
async def index():
    return FileResponse(os.path.join(config.STATIC_DIR, "index.html"))


@router.get("/api/view")
def view_image(filename: str, type: str = "input", subfolder: str = ""):
    for addr in config.COMFYUI_INSTANCES:
        try:
            response = requests.get(
                f"http://{addr}/view",
                params={"filename": filename, "type": type, "subfolder": subfolder},
                timeout=1,
            )
            if response.status_code == 200:
                return Response(content=response.content, media_type=response.headers.get("Content-Type"))
        except Exception:
            continue
    raise HTTPException(status_code=404, detail="Image not found on any available backend")


@router.get("/api/download-output")
def download_output(url: str, name: str = ""):
    path = imageproc.output_file_from_url(url)
    if not path:
        raise HTTPException(status_code=404, detail="文件不存在")
    filename = os.path.basename(name) if name else os.path.basename(path)
    return FileResponse(path, media_type=imageproc.content_type_for_path(path), filename=filename)


@router.post("/api/canvas-assets/check")
async def check_canvas_assets(payload: CanvasAssetCheckRequest):
    result = {}
    for url in payload.urls[:3000]:
        text = str(url or "").strip()
        if not text:
            continue
        if text.startswith("/output/") or text.startswith("/assets/"):
            result[text] = bool(imageproc.output_file_from_url(text))
        else:
            result[text] = True
    return {"exists": result}


@router.post("/api/canvas-assets/download")
async def download_canvas_assets(payload: CanvasAssetDownloadRequest):
    buffer = BytesIO()
    used_names = set()
    count = 0
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as zf:
        for url in payload.urls[:1000]:
            text = str(url or "").strip()
            if not text or not (text.startswith("/output/") or text.startswith("/assets/")):
                continue
            path = imageproc.output_file_from_url(text)
            if not path or not os.path.isfile(path):
                continue
            base = os.path.basename(path) or f"image-{count + 1}.png"
            name, ext = os.path.splitext(base)
            archive_name = base
            suffix = 2
            while archive_name in used_names:
                archive_name = f"{name}-{suffix}{ext}"
                suffix += 1
            used_names.add(archive_name)
            zf.write(path, archive_name)
            count += 1
    if count <= 0:
        raise HTTPException(status_code=404, detail="没有可下载的本地图片")
    buffer.seek(0)
    filename = re.sub(r'[\\/:*?"<>|]+', "_", payload.filename or "canvas-output-images.zip")
    if not filename.lower().endswith(".zip"):
        filename += ".zip"
    encoded = urllib.parse.quote(filename)
    headers = {"Content-Disposition": f"attachment; filename*=UTF-8''{encoded}"}
    return Response(buffer.getvalue(), media_type="application/zip", headers=headers)


@router.post("/api/upload")
async def upload_image(files: List[UploadFile] = File(...)):
    uploaded_files = []
    files_content = []
    for file in files:
        files_content.append((file, await file.read()))

    for file, content in files_content:
        success_count = 0
        last_result = None
        for addr in config.COMFYUI_INSTANCES:
            try:
                response = requests.post(
                    f"http://{addr}/upload/image",
                    files={"image": (file.filename, content, file.content_type)},
                    timeout=5,
                )
                if response.status_code == 200:
                    last_result = response.json()
                    success_count += 1
            except Exception as exc:
                print(f"Upload error for {addr}: {exc}")

        if success_count <= 0 or not last_result:
            raise HTTPException(status_code=500, detail="Failed to upload to any backend")
        uploaded_files.append({"comfy_name": last_result.get("name", file.filename)})

    return {"files": uploaded_files}


@router.post("/api/ai/upload")
async def upload_ai_reference(files: List[UploadFile] = File(...)):
    uploaded = []
    for file in files:
        content = await file.read()
        if not content:
            continue
        ext = os.path.splitext(file.filename or "")[1].lower()
        if ext not in [".png", ".jpg", ".jpeg", ".webp"]:
            content_type = (file.content_type or "").lower()
            ext = ".jpg" if "jpeg" in content_type else ".webp" if "webp" in content_type else ".png"
        filename = f"ai_ref_{uuid.uuid4().hex[:12]}{ext}"
        path = imageproc.output_path_for(filename, "input")
        with open(path, "wb") as handle:
            handle.write(content)
        uploaded.append({"url": imageproc.output_url_for(filename, "input"), "name": file.filename or filename})
    return {"files": uploaded}


@router.post("/api/extension/webgen-import")
async def import_web_generator_image(
    file: UploadFile = File(...),
    prompt: str = Form(""),
    prompt_id: str = Form(""),
    source_url: str = Form(""),
    source_site: str = Form(""),
    source_tab_title: str = Form(""),
    captured_at: str = Form(""),
    width: str = Form(""),
    height: str = Form(""),
    byte_size: str = Form(""),
):
    content = await file.read()
    if not content:
        raise HTTPException(status_code=400, detail="Empty image file")

    max_bytes = 64 * 1024 * 1024
    if len(content) > max_bytes:
        raise HTTPException(status_code=413, detail="Image file is too large")

    content_type = (file.content_type or "").lower()
    if content_type and not content_type.startswith("image/"):
        raise HTTPException(status_code=400, detail=f"Unsupported image content type: {file.content_type}")

    try:
        with Image.open(BytesIO(content)) as img:
            img.verify()
        with Image.open(BytesIO(content)) as img:
            detected_format = (img.format or "").upper()
            detected_width, detected_height = img.size
    except Exception as exc:
        raise HTTPException(status_code=400, detail="Uploaded file is not a valid image") from exc

    ext_by_format = {"PNG": ".png", "JPEG": ".jpg", "WEBP": ".webp"}
    ext = ext_by_format.get(detected_format)
    if not ext:
        raise HTTPException(status_code=400, detail=f"Unsupported image format: {detected_format or file.content_type or file.filename}")

    filename = f"webgen_{uuid.uuid4().hex[:12]}{ext}"
    path = imageproc.output_path_for(filename, "output")
    with open(path, "wb") as handle:
        handle.write(content)

    local_url = imageproc.output_url_for(filename, "output")
    return {
        "url": local_url,
        "filename": filename,
        "name": file.filename or filename,
        "content_type": imageproc.content_type_for_path(path),
        "width": width or detected_width,
        "height": height or detected_height,
        "byte_size": byte_size or len(content),
        "metadata": {
            "prompt": prompt,
            "prompt_id": prompt_id,
            "source": "chat-image-web-extension",
            "source_site": source_site,
            "source_url": source_url,
            "source_tab_title": source_tab_title,
            "captured_at": captured_at,
        },
    }


@router.post("/api/nodes/media-upload")
async def upload_node_media(files: List[UploadFile] = File(...)):
    allowed = {
        ".png", ".jpg", ".jpeg", ".webp",
        ".mp4", ".mov", ".webm",
        ".wav", ".mp3", ".m4a", ".aac", ".flac", ".ogg",
    }
    uploaded = []
    for file in files:
        content = await file.read()
        if not content:
            continue
        ext = os.path.splitext(file.filename or "")[1].lower()
        if ext not in allowed:
            content_type = (file.content_type or "").lower()
            if content_type.startswith("image/"):
                ext = ".jpg" if "jpeg" in content_type else ".webp" if "webp" in content_type else ".png"
            elif content_type.startswith("video/"):
                ext = ".mov" if "quicktime" in content_type else ".webm" if "webm" in content_type else ".mp4"
            elif content_type.startswith("audio/"):
                ext = ".mp3" if "mpeg" in content_type else ".m4a" if "mp4" in content_type else ".wav"
            else:
                raise HTTPException(status_code=400, detail=f"不支持的媒体格式：{file.filename or file.content_type}")
        filename = f"node_media_{uuid.uuid4().hex[:12]}{ext}"
        path = imageproc.output_path_for(filename, "input")
        with open(path, "wb") as handle:
            handle.write(content)
        uploaded.append({
            "url": imageproc.output_url_for(filename, "input"),
            "name": file.filename or filename,
            "content_type": imageproc.content_type_for_path(path),
        })
    return {"files": uploaded}


@router.post("/api/nodes/media-delete")
async def delete_node_media(payload: NodeMediaDeleteRequest):
    path = imageproc.output_file_from_url(payload.url)
    if not path:
        return {"deleted": False}

    abs_path = os.path.abspath(path)
    input_root = os.path.abspath(config.OUTPUT_INPUT_DIR)
    filename = os.path.basename(abs_path)
    ext = os.path.splitext(filename)[1].lower()
    if (
        os.path.commonpath([input_root, abs_path]) != input_root
        or not filename.startswith("node_media_")
        or ext != ".wav"
    ):
        raise HTTPException(status_code=400, detail="Only generated project WAV media can be deleted")

    try:
        os.remove(abs_path)
        return {"deleted": True}
    except FileNotFoundError:
        return {"deleted": False}
    except OSError as exc:
        raise HTTPException(status_code=500, detail=f"Failed to delete media: {exc}") from exc


@router.get("/api/history")
async def get_history_api(type: str = None):
    return store.load_history(type)


@router.get("/api/queue_status")
async def get_queue_status(client_id: str):
    with config.QUEUE_LOCK:
        total = len(comfyui.QUEUE)
        positions = [index + 1 for index, task in enumerate(comfyui.QUEUE) if task["client_id"] == client_id]
        position = positions[0] if positions else 0
    return {"total": total, "position": position}


@router.post("/api/history/delete")
async def delete_history(req: DeleteHistoryRequest):
    target_record = store.delete_history(req.timestamp)
    if not target_record:
        return {"success": False, "message": "Record not found"}

    for img_url in target_record.get("images", []):
        file_path = imageproc.output_file_from_url(img_url)
        if file_path and os.path.exists(file_path):
            try:
                os.remove(file_path)
            except Exception as exc:
                print(f"Failed to delete file {file_path}: {exc}")
    return {"success": True}
