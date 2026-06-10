# Go Migration Plan

Plan to port the Python FastAPI backend (`main.py`, 3349 lines) to Go,
producing a single self-contained executable with the entire frontend
embedded.

## Goals

- **Single exe distribution.** `go build` → ~15-20 MB binary containing
  HTML, JS, CSS, fonts, and the default ComfyUI workflows. No Python,
  no venv, no install step on target machines.
- **Faster cold start and image processing.** 20x faster startup, 5-10x
  faster image conversion/resize.
- **Lower memory floor.** ~15-25 MB resident vs ~80-120 MB for Python.
- **Preserve the existing frontend verbatim.** Same `static/*.html`,
  no JS rewrite.
- **Readable file structure.** Code split by responsibility, no
  single-file monolith.

## Non-goals

- Rewriting the frontend.
- Changing the HTTP API surface. Every existing endpoint must respond
  with byte-equivalent JSON.
- Optimizing for >1000 RPS. This is a single-user / studio LAN tool.

## Documents

1. [`01-architecture.md`](01-architecture.md) — target directory layout
   and rationale.
2. [`02-image-engine.md`](02-image-engine.md) — image processing /
   composition stack selection.
3. [`03-port-plan.md`](03-port-plan.md) — phased port with concrete
   per-phase deliverables and checklists.
4. [`04-build-and-ship.md`](04-build-and-ship.md) — `embed.FS`,
   build commands, cross-compile, release artifact.
5. [`05-risks.md`](05-risks.md) — known gotchas and mitigation.
6. [`06-testing.md`](06-testing.md) — unit / integration / parity
   testing strategy, tooling, CI, coverage targets.

Updated 2026-06-10:

7. [`07-volcengine-ark.md`](07-volcengine-ark.md) — Volcengine Ark Go
   SDK research and the Seedream / Seedance / asset-library port map.

## Quick summary

- **Framework:** Gin (`github.com/gin-gonic/gin`)
- **Image stack:** `disintegration/imaging` (resize / filter) +
  `fogleman/gg` (composition / drawing). Both pure Go, no CGO.
- **WebSocket:** `gorilla/websocket`
- **Validation:** `go-playground/validator/v10` (Gin's default binder)
- **Volcengine Ark:** `github.com/volcengine/volcengine-go-sdk/service/arkruntime`
  for Seedream / Seedance; hand-signed AK/SK HTTP remains acceptable for
  asset-library APIs that are currently hand-signed in Python.
- **Static / workflows:** `embed.FS` at build time
- **Estimated effort:** ~5 working days for a Go-fluent developer.
- **Output:** `infinite-canvas.exe` (~15-20 MB), runs anywhere with no
  external dependencies.
