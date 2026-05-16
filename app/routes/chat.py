"""LLM 对话：/api/chat, /api/chat/stream, /api/canvas-llm。"""

import asyncio
import json
import uuid

import httpx
from fastapi import APIRouter, Header, HTTPException, Request
from fastapi.responses import StreamingResponse

from .. import config, imageproc, providers, store, upstream
from ..models import CanvasLLMRequest, ChatRequest

router = APIRouter()


def upstream_message_from_record(item: dict):
    """会话记录条目 → 上游 chat completion message。包含图片附件时构造 vision 多模态。"""
    role = item.get("role")
    if role not in {"user", "assistant"} or item.get("type") == "image":
        return None
    refs = item.get("attachments") or []
    if refs and role == "user":
        content = [{"type": "text", "text": item.get("content", "")}]
        for ref in refs[:4]:
            url = imageproc.reference_to_data_url(ref)
            if url:
                content.append({"type": "image_url", "image_url": {"url": url}})
        return {"role": role, "content": content}
    return {"role": role, "content": item.get("content", "")}


@router.post("/api/canvas-llm")
async def canvas_llm(payload: CanvasLLMRequest):
    chat_base, chat_hdrs, model = providers.resolve_chat_provider(payload.provider, payload.model, payload.ms_model)
    llm_provider = providers.get_api_provider(payload.provider) if payload.provider not in ("modelscope",) else {}
    is_apimart = providers.is_apimart_provider(llm_provider)

    upstream_messages = [{"role": "system", "content": payload.system_prompt or config.SYSTEM_PROMPT}]
    for item in payload.messages[-config.MAX_HISTORY_MESSAGES:]:
        role = item.get("role")
        content = item.get("content")
        if role in {"user", "assistant"} and content:
            upstream_messages.append({"role": role, "content": content})

    # 有图片时用 OpenAI vision 多模态格式
    if payload.images:
        content_parts = [{"type": "text", "text": payload.message}]
        ok_imgs = 0
        for img in payload.images[:8]:
            if not img or not isinstance(img, str):
                continue
            if img.startswith("/output/") or img.startswith("/assets/"):
                ref_url = imageproc.reference_to_data_url({"url": img}, max_size=1024)
            else:
                ref_url = img
            if not ref_url:
                continue
            content_parts.append({"type": "image_url", "image_url": {"url": ref_url}})
            ok_imgs += 1
        print(f"[canvas-llm] model={model} provider={payload.provider} text_len={len(payload.message)} images={ok_imgs}/{len(payload.images)}")
        upstream_messages.append({"role": "user", "content": content_parts})
    else:
        upstream_messages.append({"role": "user", "content": payload.message})

    raw = None
    try:
        async with httpx.AsyncClient(timeout=config.AI_REQUEST_TIMEOUT) as client:
            req_body = {"model": model, "messages": upstream_messages}
            if is_apimart:
                req_body["stream"] = False  # APIMart 默认流式，强制关闭
            response = await client.post(f"{chat_base}/chat/completions", headers=chat_hdrs, json=req_body)
            response.raise_for_status()
            if not response.content:
                raise HTTPException(status_code=502, detail="上游接口返回了空响应")
            raw = response.json()
    except httpx.HTTPStatusError as exc:
        body = exc.response.text or ""
        raise HTTPException(status_code=exc.response.status_code, detail=f"上游接口错误：{body}") from exc
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=502, detail=f"请求上游接口失败：{exc}") from exc
    except HTTPException:
        raise
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"解析上游响应失败：{exc}") from exc

    try:
        text = upstream.text_from_chat_response(raw).strip() if isinstance(raw, dict) else ""
        text = text or "接口返回了空回复。"
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"解析回复内容失败：{exc}") from exc
    raw_data = upstream.unwrap_apimart_response(raw) if isinstance(raw, dict) else {}
    return {"text": text, "model": model, "raw_usage": raw_data.get("usage")}


@router.post("/api/chat")
async def chat(payload: ChatRequest, request: Request, x_user_id: str = Header(default="")):
    user_id = store.safe_user_id(x_user_id, request)
    conversation = (
        store.load_conversation(user_id, payload.conversation_id)
        if payload.conversation_id
        else store.new_conversation(user_id, store.display_title(payload.message))
    )
    if not conversation.get("messages"):
        conversation["title"] = store.display_title(payload.message)

    refs = [ref.model_dump() for ref in payload.reference_images if ref.url]
    user_message = {
        "id": uuid.uuid4().hex,
        "role": "user",
        "content": payload.message,
        "created_at": store.now_ms(),
        "attachments": refs,
        "mode": payload.mode,
    }
    conversation["messages"].append(user_message)
    conversation["updated_at"] = store.now_ms()
    store.save_conversation(user_id, conversation)

    if payload.mode == "image":
        image_provider_id = payload.provider if payload.provider not in {"modelscope"} else "comfly"
        provider = providers.get_api_provider(image_provider_id)
        default_model = (provider.get("image_models") or [config.IMAGE_MODEL])[0]
        model = config.selected_model(payload.image_model or payload.model, default_model)
        try:
            image_data, raw = await upstream.generate_ai_image(payload.message, payload.size, payload.quality, model, refs, provider["id"])
            local_url = await imageproc.save_ai_image_to_output(image_data, prefix="chat_")
        except httpx.HTTPStatusError as exc:
            raise HTTPException(status_code=exc.response.status_code, detail=f"上游生图接口错误：{exc.response.text}") from exc
        except httpx.HTTPError as exc:
            raise HTTPException(status_code=502, detail=f"请求上游生图接口失败：{exc}") from exc
        assistant_message = {
            "id": uuid.uuid4().hex,
            "role": "assistant",
            "type": "image",
            "content": payload.message,
            "image_url": local_url,
            "created_at": store.now_ms(),
            "model": model,
            "raw_usage": raw.get("usage") if isinstance(raw, dict) else None,
        }
    else:
        chat_base, chat_hdrs, model = providers.resolve_chat_provider(payload.provider, payload.model, payload.ms_model)
        conv_provider = providers.get_api_provider(payload.provider) if payload.provider not in ("modelscope",) else {}
        conv_is_apimart = providers.is_apimart_provider(conv_provider)
        history = conversation["messages"][-config.MAX_HISTORY_MESSAGES:]
        upstream_messages = [{"role": "system", "content": config.SYSTEM_PROMPT}]
        for item in history:
            msg = upstream_message_from_record(item)
            if msg:
                upstream_messages.append(msg)
        try:
            async with httpx.AsyncClient(timeout=config.AI_REQUEST_TIMEOUT) as client:
                conv_req_body = {"model": model, "messages": upstream_messages}
                if conv_is_apimart:
                    conv_req_body["stream"] = False
                response = await client.post(f"{chat_base}/chat/completions", headers=chat_hdrs, json=conv_req_body)
                response.raise_for_status()
                raw = response.json()
        except httpx.HTTPStatusError as exc:
            raise HTTPException(status_code=exc.response.status_code, detail=f"上游接口错误：{exc.response.text}") from exc
        except httpx.HTTPError as exc:
            raise HTTPException(status_code=502, detail=f"请求上游接口失败：{exc}") from exc
        raw_data = upstream.unwrap_apimart_response(raw) if isinstance(raw, dict) else raw
        assistant_message = {
            "id": uuid.uuid4().hex,
            "role": "assistant",
            "content": upstream.text_from_chat_response(raw).strip() or "接口返回了空回复。",
            "created_at": store.now_ms(),
            "model": model,
            "raw_usage": raw_data.get("usage") if isinstance(raw_data, dict) else None,
        }

    conversation["messages"].append(assistant_message)
    conversation["updated_at"] = store.now_ms()
    store.save_conversation(user_id, conversation)
    return {"conversation": conversation, "message": assistant_message}


@router.post("/api/chat/stream")
async def chat_stream(payload: ChatRequest, request: Request, x_user_id: str = Header(default="")):
    if payload.mode == "image":
        raise HTTPException(status_code=400, detail="图片模式请使用 /api/chat")

    user_id = store.safe_user_id(x_user_id, request)
    conversation = (
        store.load_conversation(user_id, payload.conversation_id)
        if payload.conversation_id
        else store.new_conversation(user_id, store.display_title(payload.message))
    )
    if not conversation.get("messages"):
        conversation["title"] = store.display_title(payload.message)

    refs = [ref.model_dump() for ref in payload.reference_images if ref.url]
    user_message = {
        "id": uuid.uuid4().hex,
        "role": "user",
        "content": payload.message,
        "created_at": store.now_ms(),
        "attachments": refs,
        "mode": payload.mode,
    }
    conversation["messages"].append(user_message)
    conversation["updated_at"] = store.now_ms()
    store.save_conversation(user_id, conversation)

    chat_base, chat_hdrs, model = providers.resolve_chat_provider(payload.provider, payload.model, payload.ms_model)
    history = conversation["messages"][-config.MAX_HISTORY_MESSAGES:]
    upstream_messages = [{"role": "system", "content": config.SYSTEM_PROMPT}]
    for item in history:
        msg = upstream_message_from_record(item)
        if msg:
            upstream_messages.append(msg)

    async def stream():
        content_parts = []
        raw_usage = None
        yield upstream.sse_event({"type": "meta", "conversation": conversation})
        try:
            async with httpx.AsyncClient(timeout=config.AI_REQUEST_TIMEOUT) as client:
                async with client.stream(
                    "POST",
                    f"{chat_base}/chat/completions",
                    headers=chat_hdrs,
                    json={"model": model, "messages": upstream_messages, "stream": True},
                ) as response:
                    if response.status_code >= 400:
                        detail = await response.aread()
                        yield upstream.sse_event({"type": "error", "detail": f"上游接口错误：{detail.decode('utf-8', errors='ignore')}"})
                        return
                    async for line in response.aiter_lines():
                        if not line:
                            continue
                        if line.startswith("data:"):
                            line = line[5:].strip()
                        if line == "[DONE]":
                            break
                        try:
                            chunk = json.loads(line)
                        except json.JSONDecodeError:
                            continue
                        if isinstance(chunk, dict) and chunk.get("usage"):
                            raw_usage = chunk.get("usage")
                        delta = upstream.text_delta_from_chat_chunk(chunk)
                        if delta:
                            content_parts.append(delta)
                            yield upstream.sse_event({"type": "delta", "delta": delta})
        except httpx.HTTPError as exc:
            yield upstream.sse_event({"type": "error", "detail": f"请求上游接口失败：{exc}"})
            return

        assistant_message = {
            "id": uuid.uuid4().hex,
            "role": "assistant",
            "content": "".join(content_parts).strip() or "接口返回了空回复。",
            "created_at": store.now_ms(),
            "model": model,
            "raw_usage": raw_usage,
        }
        conversation["messages"].append(assistant_message)
        conversation["updated_at"] = store.now_ms()
        store.save_conversation(user_id, conversation)
        yield upstream.sse_event({"type": "done", "conversation": conversation, "message": assistant_message})

    return StreamingResponse(stream(), media_type="text/event-stream")
