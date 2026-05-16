"""路由聚合：``register_routers(app)`` 把所有子路由挂到 FastAPI 实例上。"""

from fastapi import FastAPI

from . import canvas, chat, conversation, generate, misc, provider, workflow


def register_routers(app: FastAPI) -> None:
    # 顺序仅影响 OpenAPI 文档显示，对路由匹配无影响
    app.include_router(misc.router)
    app.include_router(provider.router)
    app.include_router(canvas.router)
    app.include_router(conversation.router)
    app.include_router(chat.router)
    app.include_router(generate.router)
    app.include_router(workflow.router)
