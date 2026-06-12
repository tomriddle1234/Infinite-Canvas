"""Unit tests for Seedream size normalization, Seedance helpers, and the
Seedance result cache. Runs as a plain script (no pytest dependency).

    python tests/unit_seedream_seedance.py

Exits 0 on success, 1 on first failure. Prints a JSON summary either way.
"""

from __future__ import annotations

import json
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


FAILURES: list[str] = []


def check(label: str, condition: bool, detail: str = "") -> None:
    if condition:
        return
    FAILURES.append(f"{label}: {detail}" if detail else label)


def parse_size(text: str) -> tuple[int, int]:
    width, height = text.split("x")
    return int(width), int(height)


# ---------------- normalize_seedream_size ----------------

def test_seedream_size() -> None:
    from app.imageproc import (
        SEEDREAM_MAX_PIXELS_V4,
        SEEDREAM_MAX_PIXELS_V5,
        SEEDREAM_MIN_PIXELS_V4,
        SEEDREAM_MIN_PIXELS_V4_5,
        SEEDREAM_MIN_PIXELS_V5,
        normalize_seedream_size,
    )

    # Invalid input → default 2048x2048
    check("seedream:invalid-input", normalize_seedream_size("garbage", "doubao-seedream-4-0-250828") == "2048x2048")
    check("seedream:empty", normalize_seedream_size("", "") == "2048x2048")

    # V4: 2048x2048 already inside range → unchanged
    out = normalize_seedream_size("2048x2048", "doubao-seedream-4-0-250828")
    w, h = parse_size(out)
    check("seedream:v4-2048-stable", out == "2048x2048", f"got {out}")
    check("seedream:v4-2048-in-range", SEEDREAM_MIN_PIXELS_V4 <= w * h <= SEEDREAM_MAX_PIXELS_V4)

    # V4: tiny size → grows to >= V4 min pixels
    out = normalize_seedream_size("256x256", "doubao-seedream-4-0-250828")
    w, h = parse_size(out)
    check("seedream:v4-tiny-grows", w * h >= SEEDREAM_MIN_PIXELS_V4, f"got {out}")
    check("seedream:v4-tiny-multiple-of-16", w % 16 == 0 and h % 16 == 0, f"got {out}")

    # V4: huge size → shrinks to <= V4 max pixels
    out = normalize_seedream_size("8192x8192", "doubao-seedream-4-0-250828")
    w, h = parse_size(out)
    check("seedream:v4-huge-shrinks", w * h <= SEEDREAM_MAX_PIXELS_V4, f"got {out}")
    check("seedream:v4-huge-not-tiny", w * h >= SEEDREAM_MIN_PIXELS_V4, f"got {out}")

    # V4: extreme aspect ratio clamped to 1:16
    out = normalize_seedream_size("64x4096", "doubao-seedream-4-0-250828")
    w, h = parse_size(out)
    ratio = max(w, h) / max(1, min(w, h))
    check("seedream:v4-aspect-clamped", ratio <= 16 + 1e-6, f"got {out} ratio={ratio:.3f}")

    # V4.5: 1024x1024 below V4.5 min → grows to >= V4.5 min
    out = normalize_seedream_size("1024x1024", "doubao-seedream-4-5-251128")
    w, h = parse_size(out)
    check("seedream:v4.5-grows", w * h >= SEEDREAM_MIN_PIXELS_V4_5, f"got {out}")

    # V5: pixel limits use V5 min/max
    out = normalize_seedream_size("1024x1024", "doubao-seedream-5-0-260128")
    w, h = parse_size(out)
    check("seedream:v5-grows", w * h >= SEEDREAM_MIN_PIXELS_V5, f"got {out}")
    out = normalize_seedream_size("8192x8192", "doubao-seedream-5-0-260128")
    w, h = parse_size(out)
    check("seedream:v5-cap", w * h <= SEEDREAM_MAX_PIXELS_V5, f"got {out}")

    # Decimal-style version strings should match too
    out_dash = normalize_seedream_size("1024x1024", "seedream-4-5")
    out_dot = normalize_seedream_size("1024x1024", "seedream-4.5")
    check("seedream:version-equivalence", out_dash == out_dot, f"dash={out_dash} dot={out_dot}")


# ---------------- Seedance helpers ----------------

def test_seedance_duration() -> None:
    from app.upstream_volcengine import _is_seedance_1_5, _seedance_duration

    check("seedance:1.5-detected-dash", _is_seedance_1_5("doubao-seedance-1-5-pro-251215") is True)
    check("seedance:1.5-detected-dot", _is_seedance_1_5("doubao-seedance-1.5-pro") is True)
    check("seedance:2.0-not-1.5", _is_seedance_1_5("doubao-seedance-2-0-fast-260128") is False)
    check("seedance:other-model-not-1.5", _is_seedance_1_5("doubao-x-1-5-y") is False)

    # 1.5 family caps at 12s
    check("seedance:1.5-cap-12", _seedance_duration("doubao-seedance-1-5-pro-251215", 30) == 12)
    # 2.0 family caps at 15s
    check("seedance:2.0-cap-15", _seedance_duration("doubao-seedance-2-0-fast-260128", 30) == 15)
    # Floor at 4
    check("seedance:floor-4", _seedance_duration("doubao-seedance-2-0-fast-260128", 1) == 4)
    # Mid value passes through
    check("seedance:mid-pass", _seedance_duration("doubao-seedance-2-0-fast-260128", 8) == 8)
    # Falsy duration → default 5
    check("seedance:default-5", _seedance_duration("doubao-seedance-2-0-fast-260128", 0) == 5)


# ---------------- Seedance cache TTL/LRU ----------------

def test_seedance_cache() -> None:
    from app.routes import generate

    generate.SEEDANCE_RESULT_CACHE.clear()
    original_ttl = generate.SEEDANCE_CACHE_TTL_SECONDS
    original_max = generate.SEEDANCE_CACHE_MAX_ENTRIES
    try:
        # TTL eviction
        generate.SEEDANCE_CACHE_TTL_SECONDS = 0.05
        generate._seedance_cache_put(("task-a",), {"status": "succeeded", "videos": ["a.mp4"]})
        check("cache:hit-before-ttl", generate._seedance_cache_get(("task-a",)) is not None)
        time.sleep(0.1)
        check("cache:miss-after-ttl", generate._seedance_cache_get(("task-a",)) is None)
        check("cache:evicted-from-store", ("task-a",) not in generate.SEEDANCE_RESULT_CACHE)

        # LRU eviction
        generate.SEEDANCE_CACHE_TTL_SECONDS = 600
        generate.SEEDANCE_CACHE_MAX_ENTRIES = 3
        for i in range(5):
            generate._seedance_cache_put((f"t{i}",), {"status": "succeeded", "videos": []})
        check("cache:lru-size-bounded", len(generate.SEEDANCE_RESULT_CACHE) == 3,
              f"len={len(generate.SEEDANCE_RESULT_CACHE)}")
        check("cache:lru-keeps-latest-3", all((f"t{i}",) in generate.SEEDANCE_RESULT_CACHE for i in (2, 3, 4)))
        check("cache:lru-drops-oldest", all((f"t{i}",) not in generate.SEEDANCE_RESULT_CACHE for i in (0, 1)))

        # Access moves entry to MRU end
        generate._seedance_cache_get(("t2",))
        generate._seedance_cache_put(("t5",), {"status": "succeeded", "videos": []})
        check("cache:lru-touch-protects", ("t2",) in generate.SEEDANCE_RESULT_CACHE)
        check("cache:lru-touch-evicts-other", ("t3",) not in generate.SEEDANCE_RESULT_CACHE)
    finally:
        generate.SEEDANCE_CACHE_TTL_SECONDS = original_ttl
        generate.SEEDANCE_CACHE_MAX_ENTRIES = original_max
        generate.SEEDANCE_RESULT_CACHE.clear()


def main() -> int:
    suites = [
        ("normalize_seedream_size", test_seedream_size),
        ("seedance_duration", test_seedance_duration),
        ("seedance_cache", test_seedance_cache),
    ]
    for name, fn in suites:
        try:
            fn()
        except Exception as exc:
            FAILURES.append(f"{name}:exception:{exc!r}")
    payload = {"ok": not FAILURES, "failure_count": len(FAILURES), "failures": FAILURES}
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0 if not FAILURES else 1


if __name__ == "__main__":
    raise SystemExit(main())
