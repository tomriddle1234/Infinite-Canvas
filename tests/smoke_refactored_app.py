"""Standalone smoke test for the refactored app package.

This script intentionally does not touch main.py. It validates whether the
split app package can assemble a FastAPI application with the expected
endpoints before we consider switching the real entrypoint.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Iterable


ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


EXPECTED_PATHS = {
    "/",
    "/ws/stats",
    "/api/view",
    "/api/download-output",
    "/api/upload",
    "/api/ai/upload",
    "/api/config",
    "/api/providers",
    "/api/online-image",
    "/api/canvas-video",
    "/api/canvas-llm",
    "/api/conversations",
    "/api/canvases",
    "/api/history",
    "/api/queue_status",
    "/api/history/delete",
    "/api/angle/poll_status",
    "/api/angle/generate",
    "/generate",
    "/api/ms/generate",
    "/api/generate",
    "/api/comfyui/instances",
    "/api/workflows",
}


def _collect_paths(routes: Iterable[object]) -> set[str]:
    paths = set()
    for route in routes:
        path = getattr(route, "path", None)
        if path:
            paths.add(path)
    return paths


def main() -> int:
    try:
        from app.factory import create_app
    except ModuleNotFoundError as exc:
        missing = getattr(exc, "name", "") or str(exc)
        print(
            json.dumps(
                {
                    "ok": False,
                    "stage": "import",
                    "reason": f"missing dependency or module: {missing}",
                },
                ensure_ascii=False,
                indent=2,
            )
        )
        return 2

    try:
        app = create_app()
    except Exception as exc:  # pragma: no cover - smoke-test reporting path
        print(
            json.dumps(
                {
                    "ok": False,
                    "stage": "create_app",
                    "reason": repr(exc),
                },
                ensure_ascii=False,
                indent=2,
            )
        )
        return 1

    actual_paths = _collect_paths(app.routes)
    missing_paths = sorted(path for path in EXPECTED_PATHS if path not in actual_paths)

    payload = {
        "ok": not missing_paths,
        "route_count": len(actual_paths),
        "missing_paths": missing_paths,
    }
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0 if not missing_paths else 1


if __name__ == "__main__":
    raise SystemExit(main())
