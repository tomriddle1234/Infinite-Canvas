"""Route aggregation for the application package."""

from fastapi import FastAPI

from .. import ws
from . import canvas, chat, conversation, generate, provider, public, volcengine_assets, workflow


def register_routers(app: FastAPI) -> None:
    for router in (
        ws.router,
        public.router,
        provider.router,
        canvas.router,
        conversation.router,
        chat.router,
        generate.router,
        volcengine_assets.router,
        workflow.router,
    ):
        app.include_router(router)
