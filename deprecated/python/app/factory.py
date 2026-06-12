"""FastAPI application factory."""

import logging
import os

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles

from . import config, ws
from .routes import register_routers


def _install_access_log_filter() -> None:
    access_logger = logging.getLogger("uvicorn.access")
    if any(isinstance(item, ws.QuietAccessLogFilter) for item in access_logger.filters):
        return
    access_logger.addFilter(ws.QuietAccessLogFilter())


def _ensure_runtime_dirs() -> None:
    for path in (
        config.OUTPUT_DIR,
        config.ASSETS_DIR,
        config.OUTPUT_INPUT_DIR,
        config.OUTPUT_OUTPUT_DIR,
        config.DATA_DIR,
        config.CONVERSATION_DIR,
        config.CANVAS_DIR,
    ):
        os.makedirs(path, exist_ok=True)
    os.makedirs(os.path.dirname(config.API_ENV_FILE), exist_ok=True)


def create_app() -> FastAPI:
    _ensure_runtime_dirs()
    _install_access_log_filter()

    app = FastAPI(lifespan=ws.lifespan)
    app.add_middleware(
        CORSMiddleware,
        allow_origins=[
            "http://127.0.0.1:3000",
            "http://localhost:3000",
        ],
        allow_methods=["*"],
        allow_headers=["*"],
    )

    @app.exception_handler(RequestValidationError)
    async def request_validation_exception_handler(request: Request, exc: RequestValidationError):
        return JSONResponse(
            status_code=422,
            content={"detail": config.friendly_validation_error(exc.errors()), "errors": exc.errors()},
        )

    app.mount("/static", StaticFiles(directory=config.STATIC_DIR), name="static")
    app.mount("/output", StaticFiles(directory=config.OUTPUT_DIR), name="output")
    app.mount("/assets", StaticFiles(directory=config.ASSETS_DIR), name="assets")
    register_routers(app)
    return app

