"""图像处理：格式/分辨率转换、本地落盘、data URL 编解码。

约定：纯字节/文件路径出入，路由层只调用本模块完成存图、压缩等工作。
"""

import base64
import os
import re
import urllib.parse
import uuid
from io import BytesIO

import httpx
from PIL import Image

from . import config


# --- 路径推断 ---

def output_storage(category: str = "output"):
    return (config.OUTPUT_INPUT_DIR, "input") if category == "input" else (config.OUTPUT_OUTPUT_DIR, "output")


def output_url_for(filename: str, category: str = "output") -> str:
    _, subdir = output_storage(category)
    return f"/assets/{subdir}/{filename}"


def output_path_for(filename: str, category: str = "output") -> str:
    folder, _ = output_storage(category)
    return os.path.join(folder, filename)


def output_file_from_url(url):
    if isinstance(url, dict):
        url = url.get("url", "")
    if not url or not (url.startswith("/output/") or url.startswith("/assets/")):
        return None
    clean = urllib.parse.unquote(url.split("?", 1)[0]).replace("\\", "/")
    if clean.startswith("/assets/"):
        root = config.ASSETS_DIR
        rel = clean[len("/assets/"):]
    else:
        root = config.OUTPUT_DIR
        rel = clean[len("/output/"):]
    rel = rel.lstrip("/")
    if not rel:
        return None
    path = os.path.abspath(os.path.join(root, rel))
    output_root = os.path.abspath(root)
    if os.path.commonpath([output_root, path]) != output_root or not os.path.exists(path):
        return None
    return path


def content_type_for_path(path: str) -> str:
    ext = os.path.splitext(path)[1].lower()
    if ext in [".mp4", ".m4v"]:
        return "video/mp4"
    if ext == ".webm":
        return "video/webm"
    if ext == ".mov":
        return "video/quicktime"
    if ext == ".wav":
        return "audio/wav"
    if ext == ".mp3":
        return "audio/mpeg"
    if ext in [".m4a", ".aac"]:
        return "audio/aac"
    if ext == ".flac":
        return "audio/flac"
    if ext == ".ogg":
        return "audio/ogg"
    if ext in [".jpg", ".jpeg"]:
        return "image/jpeg"
    if ext == ".webp":
        return "image/webp"
    return "image/png"


# --- 转换 / 压缩 ---

def convert_output_to_jpg(url: str, quality: int = 88) -> str:
    path = output_file_from_url(url)
    if not path:
        return url
    root, ext = os.path.splitext(path)
    if ext.lower() in [".jpg", ".jpeg"]:
        return url
    jpg_path = f"{root}.jpg"
    try:
        with Image.open(path) as img:
            if img.mode in ("RGBA", "LA") or (img.mode == "P" and "transparency" in img.info):
                bg = Image.new("RGB", img.size, (255, 255, 255))
                bg.paste(img.convert("RGBA"), mask=img.convert("RGBA").split()[-1])
                img = bg
            else:
                img = img.convert("RGB")
            img.save(jpg_path, "JPEG", quality=quality, optimize=True)
        try:
            root_dir = (
                config.ASSETS_DIR
                if os.path.commonpath([os.path.abspath(config.ASSETS_DIR), os.path.abspath(jpg_path)]) == os.path.abspath(config.ASSETS_DIR)
                else config.OUTPUT_DIR
            )
        except ValueError:
            root_dir = config.OUTPUT_DIR
        rel = os.path.relpath(jpg_path, root_dir).replace("\\", "/")
        prefix = "/assets" if root_dir == config.ASSETS_DIR else "/output"
        return f"{prefix}/{rel}"
    except Exception as e:
        print(f"转换 JPG 失败: {e}")
        return url


def reference_to_data_url(ref: dict, max_size=None) -> str:
    """把本地输出文件转为 data URL（base64）。max_size 限制最长边像素，避免 payload 过大。"""
    path = output_file_from_url(ref.get("url", ""))
    if not path:
        return ref.get("url", "")
    if max_size:
        try:
            with Image.open(path) as img:
                img.load()
                w, h = img.size
                if max(w, h) > max_size:
                    img.thumbnail((max_size, max_size), Image.LANCZOS)
                if img.mode not in ("RGB", "RGBA"):
                    img = img.convert("RGB")
                buf = BytesIO()
                fmt = "PNG" if img.mode == "RGBA" else "JPEG"
                img.save(buf, format=fmt, quality=88 if fmt == "JPEG" else None)
                encoded = base64.b64encode(buf.getvalue()).decode("ascii")
                mime = "image/png" if fmt == "PNG" else "image/jpeg"
                return f"data:{mime};base64,{encoded}"
        except Exception as e:
            print(f"reference resize failed, fallback to raw: {e}")
    with open(path, "rb") as f:
        encoded = base64.b64encode(f.read()).decode("ascii")
    return f"data:{content_type_for_path(path)};base64,{encoded}"


def compress_data_url_image(value, max_size: int = 1536, jpeg_quality: int = 88):
    if not isinstance(value, str) or not value.startswith("data:image/") or ";base64," not in value:
        return value
    _, encoded = value.split(";base64,", 1)
    try:
        raw = base64.b64decode(encoded)
        with Image.open(BytesIO(raw)) as img:
            img.load()
            if max_size and max(img.size) > max_size:
                img.thumbnail((max_size, max_size), Image.LANCZOS)
            has_alpha = img.mode in ("RGBA", "LA") or (img.mode == "P" and "transparency" in img.info)
            if has_alpha:
                if img.mode != "RGBA":
                    img = img.convert("RGBA")
                fmt, mime = "PNG", "image/png"
            else:
                if img.mode != "RGB":
                    img = img.convert("RGB")
                fmt, mime = "JPEG", "image/jpeg"
            buf = BytesIO()
            if fmt == "JPEG":
                img.save(buf, format=fmt, quality=jpeg_quality, optimize=True)
            else:
                img.save(buf, format=fmt, optimize=True)
            return f"data:{mime};base64,{base64.b64encode(buf.getvalue()).decode('ascii')}"
    except Exception as e:
        print(f"data url image compress failed, fallback to raw: {e}")
        return value


def modelscope_image_url(value, max_size: int = 1536):
    """本地路径或 data URL → 适合 ModelScope 的 data URL；http(s) URL 原样返回。"""
    if not value:
        return value
    if isinstance(value, str) and (value.startswith("/output/") or value.startswith("/assets/")):
        return reference_to_data_url({"url": value}, max_size=max_size)
    if isinstance(value, str) and value.startswith("data:image/"):
        return compress_data_url_image(value, max_size=max_size)
    return value


# --- 远端落盘 ---

async def save_ai_image_to_output(image_data: dict, prefix: str = "online_", category: str = "output") -> str:
    filename = f"{prefix}{uuid.uuid4().hex[:10]}.png"
    path = output_path_for(filename, category)
    if image_data["type"] == "b64":
        with open(path, "wb") as f:
            f.write(base64.b64decode(image_data["value"]))
        return output_url_for(filename, category)
    value = image_data["value"]
    if value.startswith("/output/") or value.startswith("/assets/"):
        return value
    try:
        timeout = httpx.Timeout(connect=20.0, read=300.0, write=60.0, pool=20.0)
        async with httpx.AsyncClient(timeout=timeout, follow_redirects=True) as client:
            response = await client.get(value)
            response.raise_for_status()
            content_type = response.headers.get("Content-Type", "")
            if "jpeg" in content_type or "jpg" in content_type:
                filename = filename[:-4] + ".jpg"
                path = output_path_for(filename, category)
            elif "webp" in content_type:
                filename = filename[:-4] + ".webp"
                path = output_path_for(filename, category)
            with open(path, "wb") as f:
                f.write(response.content)
            return output_url_for(filename, category)
    except Exception as e:
        print(f"保存上游图片失败: {e}")
        return value


async def save_remote_video_to_output(url: str, prefix: str = "video_", category: str = "output") -> str:
    if not url:
        return ""
    if url.startswith("/output/") or url.startswith("/assets/"):
        return url
    filename = f"{prefix}{uuid.uuid4().hex[:10]}.mp4"
    path = output_path_for(filename, category)
    try:
        async with httpx.AsyncClient(timeout=config.VIDEO_POLL_TIMEOUT) as client:
            response = await client.get(url)
            response.raise_for_status()
            content_type = (response.headers.get("Content-Type") or "").lower()
            clean_path = urllib.parse.urlparse(url).path
            ext = os.path.splitext(clean_path)[1].lower()
            if ext in {".mp4", ".webm", ".mov"}:
                filename = filename[:-4] + ext
                path = output_path_for(filename, category)
            elif "webm" in content_type:
                filename = filename[:-4] + ".webm"
                path = output_path_for(filename, category)
            elif "quicktime" in content_type or "mov" in content_type:
                filename = filename[:-4] + ".mov"
                path = output_path_for(filename, category)
            with open(path, "wb") as f:
                f.write(response.content)
            return output_url_for(filename, category)
    except Exception as e:
        print(f"保存上游视频失败: {e}")
        return url


# --- 尺寸归一化 ---

GPT_IMAGE2_MAX_EDGE = 3840
GPT_IMAGE2_MAX_PIXELS = 8_294_400
GPT_IMAGE2_MIN_PIXELS = 655_360
SEEDREAM_MIN_PIXELS_V4 = 1280 * 720
SEEDREAM_MIN_PIXELS_V4_5 = 3_686_400
SEEDREAM_MAX_PIXELS_V4 = 4096 * 4096
SEEDREAM_MIN_PIXELS_V5 = 3_686_400
SEEDREAM_MAX_PIXELS_V5 = 10_404_496


def parse_size_pair(size):
    match = re.fullmatch(r"\s*(\d+)\s*[xX*]\s*(\d+)\s*", str(size or ""))
    if not match:
        return 0, 0
    return int(match.group(1)), int(match.group(2))


def is_gpt_image_2_model(model) -> bool:
    return str(model or "").strip().lower() == "gpt-image-2"


def normalize_gpt_image_2_size(size):
    width, height = parse_size_pair(size)
    if not width or not height:
        return size or "auto"
    if width == height and (width > 2048 or width * height > 4_194_304):
        return "3840x2160"
    ratio = width / height
    if ratio > 3:
        width = height * 3
    elif ratio < 1 / 3:
        height = width * 3
    scale = min(
        1.0,
        GPT_IMAGE2_MAX_EDGE / max(width, height),
        (GPT_IMAGE2_MAX_PIXELS / max(1, width * height)) ** 0.5,
    )
    width = max(16, int((width * scale) // 16) * 16)
    height = max(16, int((height * scale) // 16) * 16)
    if width * height < GPT_IMAGE2_MIN_PIXELS:
        grow = (GPT_IMAGE2_MIN_PIXELS / max(1, width * height)) ** 0.5
        width = int((width * grow + 15) // 16) * 16
        height = int((height * grow + 15) // 16) * 16
    return f"{width}x{height}"


def normalize_seedream_size(size, model=""):
    """Preserve ratio while fitting Seedream model-specific pixel limits."""
    width, height = parse_size_pair(size)
    model_text = str(model or "").lower()
    min_pixels = SEEDREAM_MIN_PIXELS_V4
    max_pixels = SEEDREAM_MAX_PIXELS_V4
    if "4-5" in model_text or "4.5" in model_text:
        min_pixels = SEEDREAM_MIN_PIXELS_V4_5
    if "5-0" in model_text or "5.0" in model_text:
        min_pixels = SEEDREAM_MIN_PIXELS_V5
        max_pixels = SEEDREAM_MAX_PIXELS_V5
    if not width or not height:
        return "2048x2048"
    ratio = max(1.0 / 16.0, min(16.0, width / max(1, height)))
    if ratio != width / max(1, height):
        if ratio >= 1:
            width = int(height * ratio)
        else:
            height = int(width / ratio)
    pixels = width * height
    if pixels < min_pixels:
        grow = (min_pixels / max(1, pixels)) ** 0.5
        width = max(16, int((width * grow + 15) // 16) * 16)
        height = max(16, int((height * grow + 15) // 16) * 16)
    elif pixels > max_pixels:
        shrink = (max_pixels / max(1, pixels)) ** 0.5
        width = max(16, int((width * shrink) // 16) * 16)
        height = max(16, int((height * shrink) // 16) * 16)
    if min_pixels <= width * height <= max_pixels:
        return f"{width}x{height}"
    while width * height < min_pixels:
        if width <= height:
            width += 16
        else:
            height += 16
    while width * height > max_pixels and width > 16 and height > 16:
        if width >= height:
            width -= 16
        else:
            height -= 16
    return f"{width}x{height}"


def apimart_size_resolution(size):
    width, height = parse_size_pair(size)
    if not width or not height:
        raw = str(size or "").strip().lower()
        if raw in {"1k", "2k", "4k"}:
            return "1:1", raw
        if re.fullmatch(r"(auto|\d+\s*:\s*\d+)", raw):
            return raw.replace(" ", ""), "1k"
        return "1:1", "1k"
    long_edge = max(width, height)
    pixels = width * height
    if long_edge >= 3000 or pixels > 4_500_000:
        resolution = "4k"
    elif long_edge >= 1800 or pixels > 1_800_000:
        resolution = "2k"
    else:
        resolution = "1k"
    common = [
        (1, 1, "1:1"), (3, 2, "3:2"), (2, 3, "2:3"), (4, 3, "4:3"), (3, 4, "3:4"),
        (5, 4, "5:4"), (4, 5, "4:5"), (16, 9, "16:9"), (9, 16, "9:16"),
        (2, 1, "2:1"), (1, 2, "1:2"), (3, 1, "3:1"), (1, 3, "1:3"),
        (21, 9, "21:9"), (9, 21, "9:21"),
    ]
    ratio = width / height
    best = min(common, key=lambda item: abs(ratio - item[0] / item[1]))
    return best[2], resolution
