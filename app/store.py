"""文件型持久化：history.json、对话、画布。

约定：所有写操作走 store；不要在路由层直接 open() 读写这几类文件。
"""

import json
import os
import re
import time
import uuid

from fastapi import HTTPException, Request

from . import config


def now_ms() -> int:
    return int(time.time() * 1000)


def display_title(text: str) -> str:
    title = re.sub(r"\s+", " ", text or "").strip()
    return title[:24] or "新对话"


def safe_user_id(user_id: str, request: Request) -> str:
    candidate = (user_id or "").strip()
    if not candidate and request.client:
        candidate = f"ip-{request.client.host}"
    if not candidate:
        candidate = "anonymous"
    candidate = re.sub(r"[^a-zA-Z0-9_.-]", "-", candidate)[:80].strip(".-")
    return candidate or "anonymous"


# --- history.json ---

def save_to_history(record: dict) -> None:
    with config.HISTORY_LOCK:
        history = []
        if os.path.exists(config.HISTORY_FILE):
            try:
                with open(config.HISTORY_FILE, "r", encoding="utf-8") as f:
                    history = json.load(f)
            except Exception:
                pass
        if "timestamp" not in record:
            record["timestamp"] = time.time()
        history.insert(0, record)
        with open(config.HISTORY_FILE, "w", encoding="utf-8") as f:
            json.dump(history[:5000], f, ensure_ascii=False, indent=4)


def load_history(history_type: str = None) -> list:
    if not os.path.exists(config.HISTORY_FILE):
        return []
    try:
        with open(config.HISTORY_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
    except Exception as exc:
        print(f"读取历史文件失败: {exc}")
        return []

    if history_type:
        data = [item for item in data if item.get("type", "zimage") == history_type]
    data = [item for item in data if item.get("images") and len(item["images"]) > 0]
    data.sort(key=lambda item: float(item.get("timestamp", 0) or 0), reverse=True)
    return data


def _load_seedance_task_map() -> dict:
    if not os.path.exists(config.SEEDANCE_TASKS_FILE):
        return {}
    try:
        with open(config.SEEDANCE_TASKS_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
    except Exception:
        return {}
    return data if isinstance(data, dict) else {}


def _save_seedance_task_map(data: dict) -> None:
    os.makedirs(os.path.dirname(config.SEEDANCE_TASKS_FILE), exist_ok=True)
    with open(config.SEEDANCE_TASKS_FILE, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


def get_seedance_task(run_id: str) -> dict | None:
    cleaned = (run_id or "").strip()
    if not cleaned:
        return None
    with config.SEEDANCE_TASKS_LOCK:
        return _load_seedance_task_map().get(cleaned)


def save_seedance_task(record: dict) -> dict:
    run_id = (record.get("run_id") or "").strip()
    if not run_id:
        raise HTTPException(status_code=400, detail="缺少 Seedance run_id")
    now = time.time()
    with config.SEEDANCE_TASKS_LOCK:
        data = _load_seedance_task_map()
        existing = data.get(run_id, {})
        merged = {**existing, **record, "run_id": run_id, "updated_at": now}
        if "created_at" not in merged:
            merged["created_at"] = now
        data[run_id] = merged
        _save_seedance_task_map(data)
        return merged


def update_seedance_task(run_id: str, updates: dict) -> dict | None:
    cleaned = (run_id or "").strip()
    if not cleaned:
        return None
    with config.SEEDANCE_TASKS_LOCK:
        data = _load_seedance_task_map()
        existing = data.get(cleaned)
        if not existing:
            return None
        merged = {**existing, **updates, "updated_at": time.time()}
        data[cleaned] = merged
        _save_seedance_task_map(data)
        return merged


def seedance_claimed_task_ids() -> set[str]:
    with config.SEEDANCE_TASKS_LOCK:
        data = _load_seedance_task_map()
    claimed = set()
    for record in data.values():
        for task_id in record.get("task_ids") or []:
            if task_id:
                claimed.add(str(task_id))
    return claimed


def find_seedance_run_by_task_id(task_id: str) -> dict | None:
    target = (task_id or "").strip()
    if not target:
        return None
    with config.SEEDANCE_TASKS_LOCK:
        data = _load_seedance_task_map()
    for record in data.values():
        if target in [str(item) for item in (record.get("task_ids") or [])]:
            return record
    return None


def delete_history(timestamp: float):
    if not os.path.exists(config.HISTORY_FILE):
        return None

    target_record = None
    with config.HISTORY_LOCK:
        with open(config.HISTORY_FILE, "r", encoding="utf-8") as f:
            history = json.load(f)

        new_history = []
        for item in history:
            item_ts = item.get("timestamp", 0)
            if (
                isinstance(timestamp, (int, float))
                and isinstance(item_ts, (int, float))
                and abs(float(item_ts) - float(timestamp)) < 0.001
            ) or str(item_ts) == str(timestamp):
                target_record = item
                continue
            new_history.append(item)

        if target_record is not None:
            with open(config.HISTORY_FILE, "w", encoding="utf-8") as f:
                json.dump(new_history, f, ensure_ascii=False, indent=4)
    return target_record


# --- 对话 ---

def user_dir(user_id: str) -> str:
    path = os.path.join(config.CONVERSATION_DIR, user_id)
    os.makedirs(path, exist_ok=True)
    return path


def conversation_path(user_id: str, conversation_id: str) -> str:
    cleaned = re.sub(r"[^a-zA-Z0-9_-]", "", conversation_id or "")
    if not cleaned:
        raise HTTPException(status_code=400, detail="无效的对话 ID")
    return os.path.join(user_dir(user_id), f"{cleaned}.json")


def save_conversation(user_id: str, conversation: dict) -> None:
    with config.CONVERSATION_LOCK:
        path = conversation_path(user_id, conversation["id"])
        with open(path, "w", encoding="utf-8") as f:
            json.dump(conversation, f, ensure_ascii=False, indent=2)


def new_conversation(user_id: str, title: str = "新对话") -> dict:
    timestamp = now_ms()
    conversation = {
        "id": uuid.uuid4().hex,
        "title": (title or "新对话")[:80],
        "created_at": timestamp,
        "updated_at": timestamp,
        "messages": [],
    }
    save_conversation(user_id, conversation)
    return conversation


def load_conversation(user_id: str, conversation_id: str) -> dict:
    path = conversation_path(user_id, conversation_id)
    if not os.path.exists(path):
        raise HTTPException(status_code=404, detail="对话不存在")
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def list_conversations(user_id: str) -> list:
    records = []
    for filename in os.listdir(user_dir(user_id)):
        if not filename.endswith(".json"):
            continue
        path = os.path.join(user_dir(user_id), filename)
        try:
            with open(path, "r", encoding="utf-8") as f:
                data = json.load(f)
        except Exception:
            continue
        messages = data.get("messages", [])
        last_message = next((m for m in reversed(messages) if m.get("role") != "system"), None)
        records.append({
            "id": data.get("id"),
            "title": data.get("title", "新对话"),
            "created_at": data.get("created_at", 0),
            "updated_at": data.get("updated_at", 0),
            "last_message": (last_message or {}).get("content", ""),
        })
    return sorted(records, key=lambda item: item["updated_at"], reverse=True)


# --- 画布 ---

def canvas_path(canvas_id: str) -> str:
    cleaned = re.sub(r"[^a-zA-Z0-9_-]", "", canvas_id or "")
    if not cleaned:
        raise HTTPException(status_code=400, detail="无效的画布 ID")
    return os.path.join(config.CANVAS_DIR, f"{cleaned}.json")


def save_canvas(canvas: dict) -> None:
    canvas["updated_at"] = now_ms()
    with config.CANVAS_LOCK:
        with open(canvas_path(canvas["id"]), "w", encoding="utf-8") as f:
            json.dump(canvas, f, ensure_ascii=False, indent=2)


def new_canvas(title: str = "未命名画布", icon: str = "layers", kind: str = "classic") -> dict:
    timestamp = now_ms()
    canvas = {
        "id": uuid.uuid4().hex,
        "title": (title or "未命名画布")[:80],
        "icon": (icon or "🧩")[:32],
        "kind": "classic",
        "created_at": timestamp,
        "updated_at": timestamp,
        "nodes": [],
        "connections": [],
        "viewport": {"x": 0, "y": 0, "scale": 1},
        "logs": [],
        "settings": {},
    }
    save_canvas(canvas)
    return canvas


def load_canvas(canvas_id: str) -> dict:
    path = canvas_path(canvas_id)
    if not os.path.exists(path):
        raise HTTPException(status_code=404, detail="画布不存在")
    with open(path, "r", encoding="utf-8") as f:
        canvas = json.load(f)
    if canvas.get("deleted_at"):
        raise HTTPException(status_code=404, detail="画布已在回收站")
    return canvas


def load_canvas_any(canvas_id: str) -> dict:
    """不过滤回收站状态。"""
    path = canvas_path(canvas_id)
    if not os.path.exists(path):
        raise HTTPException(status_code=404, detail="画布不存在")
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def canvas_record(data: dict) -> dict:
    return {
        "id": data.get("id"),
        "title": data.get("title", "未命名画布"),
        "icon": data.get("icon", "🧩"),
        "kind": data.get("kind", "classic"),
        "created_at": data.get("created_at", 0),
        "updated_at": data.get("updated_at", 0),
        "deleted_at": data.get("deleted_at", 0),
        "node_count": len(data.get("nodes", [])),
    }


def cleanup_expired_canvas_trash() -> None:
    cutoff = now_ms() - config.CANVAS_TRASH_RETENTION_MS
    with config.CANVAS_LOCK:
        for filename in os.listdir(config.CANVAS_DIR):
            if not filename.endswith(".json"):
                continue
            path = os.path.join(config.CANVAS_DIR, filename)
            try:
                with open(path, "r", encoding="utf-8") as f:
                    data = json.load(f)
                deleted_at = int(data.get("deleted_at") or 0)
                if deleted_at and deleted_at < cutoff:
                    os.remove(path)
            except Exception:
                continue


def iter_canvas_records(include_deleted: bool = False) -> list:
    cleanup_expired_canvas_trash()
    records = []
    for filename in os.listdir(config.CANVAS_DIR):
        if not filename.endswith(".json"):
            continue
        try:
            with open(os.path.join(config.CANVAS_DIR, filename), "r", encoding="utf-8") as f:
                data = json.load(f)
        except Exception:
            continue
        is_deleted = bool(data.get("deleted_at"))
        if include_deleted != is_deleted:
            continue
        records.append(canvas_record(data))
    return records


def list_canvases() -> list:
    return sorted(iter_canvas_records(include_deleted=False),
                  key=lambda item: item["updated_at"], reverse=True)


def list_deleted_canvases() -> list:
    return sorted(iter_canvas_records(include_deleted=True),
                  key=lambda item: item["deleted_at"], reverse=True)
