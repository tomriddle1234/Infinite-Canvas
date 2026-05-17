"""画布 CRUD：/api/canvases/*。"""

import os

from fastapi import APIRouter, HTTPException

from .. import store
from ..models import CanvasCreateRequest, CanvasSaveRequest

router = APIRouter()


@router.get("/api/canvases")
async def canvases():
    return {"canvases": store.list_canvases()}


@router.get("/api/canvases/trash")
async def trashed_canvases():
    return {"canvases": store.list_deleted_canvases(), "retention_days": 30}


@router.post("/api/canvases")
async def create_canvas(payload: CanvasCreateRequest):
    return {"canvas": store.new_canvas(payload.title, payload.icon)}


@router.get("/api/canvases/{canvas_id}/meta")
async def get_canvas_meta(canvas_id: str):
    canvas = store.load_canvas(canvas_id)
    return {
        "id": canvas.get("id"),
        "updated_at": canvas.get("updated_at", 0),
        "title": canvas.get("title", "未命名画布"),
        "icon": canvas.get("icon", "layers"),
    }


@router.get("/api/canvases/{canvas_id}")
async def get_canvas(canvas_id: str):
    return {"canvas": store.load_canvas(canvas_id)}


@router.put("/api/canvases/{canvas_id}")
async def update_canvas(canvas_id: str, payload: CanvasSaveRequest):
    canvas = store.load_canvas(canvas_id)
    current_updated_at = int(canvas.get("updated_at") or 0)
    if payload.base_updated_at and current_updated_at and int(payload.base_updated_at) < current_updated_at:
        raise HTTPException(status_code=409, detail={
            "message": "画布已被其他页面更新，已拒绝旧版本覆盖。",
            "canvas": canvas,
            "updated_at": current_updated_at,
        })
    canvas["title"] = (payload.title or canvas.get("title") or "未命名画布")[:80]
    canvas["icon"] = (payload.icon or canvas.get("icon") or "layers")[:32]
    canvas["nodes"] = payload.nodes
    canvas["connections"] = payload.connections
    canvas["viewport"] = payload.viewport
    canvas["logs"] = payload.logs[-500:]
    store.save_canvas(canvas)
    return {"canvas": canvas}


@router.delete("/api/canvases/{canvas_id}")
async def delete_canvas(canvas_id: str):
    canvas = store.load_canvas_any(canvas_id)
    if not canvas.get("deleted_at"):
        canvas["deleted_at"] = store.now_ms()
        store.save_canvas(canvas)
    return {"ok": True}


@router.post("/api/canvases/{canvas_id}/restore")
async def restore_canvas(canvas_id: str):
    canvas = store.load_canvas_any(canvas_id)
    if canvas.get("deleted_at"):
        canvas.pop("deleted_at", None)
        store.save_canvas(canvas)
    return {"canvas": canvas}


@router.delete("/api/canvases/{canvas_id}/purge")
async def purge_canvas(canvas_id: str):
    path = store.canvas_path(canvas_id)
    if os.path.exists(path):
        os.remove(path)
    return {"ok": True}
