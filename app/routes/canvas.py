"""画布 CRUD：/api/canvases/*。"""

import os

from fastapi import APIRouter

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


@router.get("/api/canvases/{canvas_id}")
async def get_canvas(canvas_id: str):
    return {"canvas": store.load_canvas(canvas_id)}


@router.put("/api/canvases/{canvas_id}")
async def update_canvas(canvas_id: str, payload: CanvasSaveRequest):
    canvas = store.load_canvas(canvas_id)
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
