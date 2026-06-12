"""对话管理：/api/conversations/*。"""

import os

from fastapi import APIRouter, Header, Request

from .. import store
from ..models import ConversationCreateRequest

router = APIRouter()


@router.get("/api/conversations")
async def conversations(request: Request, x_user_id: str = Header(default="")):
    user_id = store.safe_user_id(x_user_id, request)
    return {"user_id": user_id, "conversations": store.list_conversations(user_id)}


@router.post("/api/conversations")
async def create_conversation(payload: ConversationCreateRequest, request: Request, x_user_id: str = Header(default="")):
    user_id = store.safe_user_id(x_user_id, request)
    return {"conversation": store.new_conversation(user_id, payload.title)}


@router.get("/api/conversations/{conversation_id}")
async def get_conversation(conversation_id: str, request: Request, x_user_id: str = Header(default="")):
    user_id = store.safe_user_id(x_user_id, request)
    return {"conversation": store.load_conversation(user_id, conversation_id)}


@router.delete("/api/conversations/{conversation_id}")
async def delete_conversation(conversation_id: str, request: Request, x_user_id: str = Header(default="")):
    user_id = store.safe_user_id(x_user_id, request)
    path = store.conversation_path(user_id, conversation_id)
    if os.path.exists(path):
        os.remove(path)
    return {"ok": True}
