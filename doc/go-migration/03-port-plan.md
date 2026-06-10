# 03 — Phased Port Plan

5 phases. Each ends in a runnable Go binary that does strictly more
than the previous. Python and Go can run side-by-side (Python on 3000,
Go on 8080) for the whole migration.

## Phase 0 — Scaffold (½ day)

Goal: empty Gin server on :8080 that serves the existing
`static/index.html`. Verify embed works end to end.

Deliverables:

- `go.mod` with module name, Go 1.22, deps pinned.
- `cmd/server/main.go`: ~50 lines, calls `server.New(cfg).Run()`.
- `internal/server/server.go`: builds Gin engine, applies CORS,
  registers routes, starts.
- `internal/server/static.go`: `//go:embed web/static/*` and serves
  `/static/*` + `/` (= index.html).
- `web/static/`: copy of current `static/` (one-time).
- `Makefile`: `make build`, `make run`, `make tidy`.

Acceptance test: `make run`, open http://localhost:8080/, see the
current UI render (it won't talk to backend yet — every fetch will 404,
that's expected).

## Phase 1 — Read-only endpoints (1 day)

Port the GET endpoints that don't talk to any upstream API. These are
small and exercise the model + store layers.

Endpoints (10 total):

| Endpoint | Maps to |
|---|---|
| `GET /api/canvases` | `handler.ListCanvases` → `store.Canvas` |
| `GET /api/canvases/trash` | `handler.ListTrashedCanvases` |
| `GET /api/canvases/:id` | `handler.GetCanvas` |
| `GET /api/conversations` | `handler.ListConversations` |
| `GET /api/conversations/:id` | `handler.GetConversation` |
| `GET /api/workflows` | `handler.ListWorkflows` |
| `GET /api/workflows/:name` | `handler.GetWorkflow` |
| `GET /api/config` | `handler.GetConfig` |
| `GET /api/models` | `handler.ListModels` |
| `GET /api/providers` | `handler.ListProviders` |

Side work:

- Implement `model/canvas.go`, `model/conversation.go`,
  `model/workflow.go`, `model/provider.go`.
- Implement `store/canvas.go`, `store/conversation.go`,
  `store/workflows.go`, `store/providers.go`.
- Implement `config/env.go` (port of `load_env_file`).
- Implement `config/config.go` (paths + defaults).

Acceptance test: every endpoint returns byte-equivalent JSON to the
Python version when fed the same on-disk data. Write a tiny shell
script that hits both servers and diffs the JSON.

## Phase 2 — Mutation endpoints + WebSocket (1 day)

Port the POST/PUT/DELETE endpoints for canvases, conversations,
providers, and configuration. Plus the WebSocket.

Endpoints (~15):

- `POST /api/canvases` (create), `POST /api/canvases/:id` (save),
  `DELETE /api/canvases/:id`, restore, hard-delete
- `POST /api/conversations`, conversation save
- `POST /api/providers`, provider update, delete
- `POST /api/config/token`
- `WS /ws/stats`

Side work:

- Implement `ws/manager.go` (port of `ConnectionManager`). Use
  `gorilla/websocket`. Track connections in a `sync.Map`, broadcast
  via a fan-out goroutine.
- Wire `App.WS` into `server/server.go` so handlers can call
  `app.WS.BroadcastNewImage(...)`.
- Implement `store/lockset.go` for per-resource locking
  (replaces module-level `QUEUE_LOCK` / `HISTORY_LOCK` / etc.).

Acceptance test: open the canvas UI against the Go server, create /
edit / delete a canvas, refresh, verify state persists and matches
Python.

## Phase 3 — Image processing + output endpoints (1 day)

Port the image pipeline and the endpoints that produce / serve output
files.

Endpoints:

- `GET /api/view` (serve a generated image)
- `GET /api/download-output` (download with optional JPG conversion)
- `GET /api/history`
- `GET /api/queue_status`
- `POST /api/online-image` (fetch external URL, save locally)

Side work:

- Implement `imageproc/` per the interface in
  [`02-image-engine.md`](02-image-engine.md): `convert.go`,
  `resize.go`, `dataurl.go`, `compose.go`, `format.go`.
- Implement `store/history.go`.

Acceptance test: trigger any existing image-producing flow on the
Python server, then hit `/api/download-output?to_jpg=true` on the
Go server with the same filename. Compare output JPEGs visually
(should be perceptually identical; bytes will differ due to encoder).

## Phase 4 — Upstream generation (2 days)

The bulk of the work. Port the calls to Apimart / OpenAI / ModelScope
/ ComfyUI and the orchestration around them.

Endpoints:

- `POST /generate` (ComfyUI workflow submit + poll)
- `POST /api/cloud/generate` + `/api/cloud/poll` (Apimart async)
- `POST /api/ms/generate`, `POST /api/ms/generate/canvas-llm`
- `POST /api/canvas/llm` (LLM chat, possibly streaming)
- `POST /api/canvas/video`
- `POST /api/comfyui/instances` (CRUD)
- `POST /api/providers/:id/test-connection`
- `POST /api/providers/:id/fetch-models`

Side work — `internal/upstream/`:

- `apimart.go`: async submit → poll loop with backoff.
- `openai.go`: chat completion, image generation, SSE streaming for
  chat replies.
- `volcengine.go`: Ark Seedream image generation and Seedance video
  content-generation tasks. Use the official Go SDK package
  `github.com/volcengine/volcengine-go-sdk/service/arkruntime` for
  `GenerateImages`, `GenerateImagesStreaming`,
  `CreateContentGenerationTask`, `GetContentGenerationTask`, and
  `ListContentGenerationTasks`.
- `volcengine_assets.go`: port the existing Ark asset-library calls
  (`ListAssetGroups`, `ListAssets`, `GetAsset`, `ListMediaAssetGroup`).
  These are hand-signed AK/SK HTTP calls in Python today, so a Go
  implementation can either reuse the official SDK if a generated method
  is available or keep a small Volcengine Signature V4 helper.
- `modelscope.go`: generation + polling.
- `comfyui.go`: workflow JSON modification, queue prompt, poll
  history, download outputs.
- `extract.go`: helpers that probe many possible JSON paths
  (port of `extract_image`, `extract_task_id`,
  `unwrap_apimart_response`, `text_from_chat_response`).

Updated 2026-06-10: Volcengine Ark is not a blocker for the Go port.
The current Python implementation uses `volcengine-python-sdk[ark]` in
`app/upstream_volcengine.py` for Seedream / Seedance, and hand-signed
HTTP in `app/upstream_volcengine_assets.py` for asset browsing and
preview caching. The Go SDK exposes the runtime methods needed for the
generation path; see [`07-volcengine-ark.md`](07-volcengine-ark.md) for
the mapping and links.

Acceptance test: hit each generate endpoint with the canvas UI,
verify images / videos land in `output/` and are pushed via WS.

## Phase 5 — Cutover (½ day)

- Verify all 47 endpoints pass parity tests.
- Add the route the Python README mentions but the table above
  missed: re-check `main.py` for any leftover endpoint.
- Move `static/` and `workflows/` directories under `web/`.
- Update `run.bat` to launch the Go binary instead of `python main.py`.
- Move `main.py` to `legacy/main.py` so it's still recoverable in git
  for one release, then delete in a follow-up commit.
- Update README to describe the Go binary.

## Total: ~5 working days

| Phase | Days |
|---|---|
| 0 — Scaffold | 0.5 |
| 1 — Read-only | 1.0 |
| 2 — Mutations + WS | 1.0 |
| 3 — Image + output | 1.0 |
| 4 — Upstream gen | 2.0 |
| 5 — Cutover | 0.5 |
| **Total** | **6.0** |

Buffer 1-2 extra days for the upstream protocol quirks discovered
along the way.
