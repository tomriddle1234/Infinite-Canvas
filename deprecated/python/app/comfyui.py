"""ComfyUI 后端集群相关：负载均衡、文件下载、历史查询。

模块级状态（QUEUE / BACKEND_LOCAL_LOAD / NEXT_TASK_ID）使用 ``from app import comfyui``
后用 ``comfyui.XXX`` 访问以便在运行期被路由层修改。
"""

import json
import os
import shutil
import urllib.parse
import urllib.request
import uuid

import requests

from . import config
from . import imageproc

# --- 运行时状态 ---

QUEUE: list = []
NEXT_TASK_ID = 1
BACKEND_LOCAL_LOAD = {addr: 0 for addr in config.COMFYUI_INSTANCES}


def reset_backend_local_load(instances) -> None:
    """切换 ComfyUI 实例列表时调用。"""
    global BACKEND_LOCAL_LOAD
    new_load = {addr: 0 for addr in instances}
    for addr, n in (BACKEND_LOCAL_LOAD or {}).items():
        if addr in new_load:
            new_load[addr] = n
    BACKEND_LOCAL_LOAD = new_load


# --- 负载均衡 ---

def check_images_exist(backend_addr: str, images) -> bool:
    if not images:
        return True
    for img in images:
        try:
            url = f"http://{backend_addr}/view?filename={urllib.parse.quote(img)}&type=input"
            r = requests.get(url, stream=True, timeout=0.5)
            r.close()
            if r.status_code != 200:
                return False
        except Exception:
            return False
    return True


def get_best_backend(required_images=None) -> str:
    best_backend = config.COMFYUI_INSTANCES[0]
    min_queue_size = float("inf")
    candidates_with_images = []
    candidates_others = []
    backend_stats = {}

    for addr in config.COMFYUI_INSTANCES:
        try:
            with urllib.request.urlopen(f"http://{addr}/queue", timeout=1) as response:
                data = json.loads(response.read())
                remote_load = len(data.get("queue_running", [])) + len(data.get("queue_pending", []))
                with config.LOAD_LOCK:
                    local_load = BACKEND_LOCAL_LOAD.get(addr, 0)
                effective_load = max(remote_load, local_load)
                has_images = check_images_exist(addr, required_images)
                backend_stats[addr] = {"load": effective_load, "has_images": has_images}
                if has_images:
                    candidates_with_images.append(addr)
                else:
                    candidates_others.append(addr)
        except Exception as e:
            print(f"Backend {addr} unreachable: {e}")
            continue

    target_candidates = candidates_with_images if candidates_with_images else candidates_others
    if not target_candidates:
        if candidates_others:
            target_candidates = candidates_others
        else:
            return config.COMFYUI_INSTANCES[0]

    for addr in target_candidates:
        load = backend_stats[addr]["load"]
        if load < min_queue_size:
            min_queue_size = load
            best_backend = addr

    return best_backend


# --- 下载输出 ---

def download_image(comfy_address: str, comfy_url_path: str, prefix: str = "studio_") -> str:
    filename = f"{prefix}{uuid.uuid4().hex[:10]}.png"
    local_path = imageproc.output_path_for(filename, "output")
    full_url = f"http://{comfy_address}{comfy_url_path}"
    try:
        with urllib.request.urlopen(full_url) as response, open(local_path, "wb") as out_file:
            shutil.copyfileobj(response, out_file)
        return imageproc.output_url_for(filename, "output")
    except Exception as e:
        print(f"下载图片失败: {e}")
        if comfy_url_path.startswith("/view"):
            return comfy_url_path.replace("/view", "/api/view", 1)
        return full_url


def comfy_output_extension(item) -> str:
    filename = str((item or {}).get("filename") or "")
    ext = os.path.splitext(filename)[1].lower()
    if ext in {".png", ".jpg", ".jpeg", ".webp", ".mp4", ".webm", ".mov", ".m4v", ".gif"}:
        return ext
    fmt = str((item or {}).get("format") or "").lower()
    if "webm" in fmt:
        return ".webm"
    if "quicktime" in fmt or "mov" in fmt:
        return ".mov"
    if "mp4" in fmt or "h264" in fmt or "video" in fmt:
        return ".mp4"
    return ".png"


def is_video_output_item(item) -> bool:
    ext = comfy_output_extension(item)
    fmt = str((item or {}).get("format") or "").lower()
    return ext in {".mp4", ".webm", ".mov", ".m4v"} or "video" in fmt


def download_comfy_output(comfy_address: str, item: dict, prefix: str = "studio_") -> str:
    ext = comfy_output_extension(item)
    filename = f"{prefix}{uuid.uuid4().hex[:10]}{ext}"
    local_path = imageproc.output_path_for(filename, "output")
    subfolder = urllib.parse.quote(str(item.get("subfolder") or ""))
    file_type = urllib.parse.quote(str(item.get("type") or "output"))
    comfy_url_path = f"/view?filename={urllib.parse.quote(str(item['filename']))}&subfolder={subfolder}&type={file_type}"
    full_url = f"http://{comfy_address}{comfy_url_path}"
    try:
        with urllib.request.urlopen(full_url) as response, open(local_path, "wb") as out_file:
            shutil.copyfileobj(response, out_file)
        return imageproc.output_url_for(filename, "output")
    except Exception as e:
        print(f"下载 ComfyUI 输出失败: {e}")
        if comfy_url_path.startswith("/view"):
            return comfy_url_path.replace("/view", "/api/view", 1)
        return full_url


def get_comfy_history(comfy_address: str, prompt_id: str) -> dict:
    try:
        with urllib.request.urlopen(f"http://{comfy_address}/history/{prompt_id}") as response:
            return json.loads(response.read())
    except Exception:
        return {}
