# 01 — Architecture

## Target directory layout

```
.
├── cmd/
│   └── server/
│       └── main.go                  # entry point, ~50 lines
├── internal/
│   ├── config/
│   │   ├── config.go                # global config struct, defaults
│   │   ├── env.go                   # API/.env loader
│   │   └── paths.go                 # all on-disk paths
│   ├── server/
│   │   ├── server.go                # Gin engine, middleware, lifecycle
│   │   ├── routes.go                # route registration only
│   │   ├── static.go                # embed.FS mounting
│   │   ├── logging.go               # access log filter (port of QuietAccessLogFilter)
│   │   └── errors.go                # validation error formatting
│   ├── handler/
│   │   ├── canvas.go                # /api/canvases/*
│   │   ├── conversation.go          # /api/conversations/*
│   │   ├── generate.go              # /generate, /api/cloud/*
│   │   ├── modelscope.go            # /api/ms/*
│   │   ├── comfyui.go               # /api/comfyui/*
│   │   ├── workflow.go              # /api/workflows/*
│   │   ├── provider.go              # /api/providers/*, /api/config
│   │   ├── chat.go                  # LLM chat + SSE streaming
│   │   ├── online.go                # /api/online-image
│   │   ├── output.go                # /api/view, /api/download-output
│   │   └── ws.go                    # /ws/stats websocket
│   ├── model/
│   │   ├── request.go               # request DTOs (was Pydantic input)
│   │   ├── canvas.go                # Canvas, CanvasNode
│   │   ├── conversation.go          # Conversation, Message
│   │   ├── provider.go              # ApiProvider, ApiProviderPayload
│   │   ├── workflow.go              # WorkflowConfig, WorkflowField
│   │   └── reference.go             # AIReference (image ref)
│   ├── upstream/
│   │   ├── client.go                # shared http.Client with timeouts
│   │   ├── apimart.go               # Apimart async protocol
│   │   ├── openai.go                # OpenAI-compatible chat + image
│   │   ├── modelscope.go            # ModelScope generation + poll
│   │   ├── comfyui.go               # ComfyUI HTTP + queue polling
│   │   └── extract.go               # JSON "probe many paths" helpers
│   ├── imageproc/
│   │   ├── convert.go               # PNG↔JPEG, alpha→white-bg
│   │   ├── resize.go                # thumbnail / max-side resize
│   │   ├── compose.go               # composition (gg-based)
│   │   ├── dataurl.go               # data: URL encode/decode
│   │   └── format.go                # MIME / extension detection
│   ├── store/
│   │   ├── canvas.go                # canvas JSON persistence + trash
│   │   ├── conversation.go          # conversation JSON persistence
│   │   ├── history.go               # history.json append/read
│   │   ├── providers.go             # api_providers.json
│   │   ├── workflows.go             # workflow file read/write
│   │   └── lockset.go               # named sync.Mutex map (ports QUEUE_LOCK etc.)
│   ├── ws/
│   │   └── manager.go               # ConnectionManager port
│   └── version/
│       └── version.go               # populated by -ldflags at build
├── web/
│   ├── static/                      # move current static/ here, embedded
│   └── workflows/                   # move current workflows/ here, embedded
├── test/
│   ├── e2e/
│   │   ├── server_test.go           # spin up app, hit endpoints, assert JSON
│   │   └── parity_test.go           # diff Go vs Python responses (golden files)
│   └── testdata/
│       ├── canvas/                  # sample canvas JSONs
│       ├── workflows/               # sample workflow JSONs
│       └── upstream/                # captured Apimart/OpenAI/MS/ComfyUI responses
├── go.mod
├── go.sum
├── Makefile                         # build / run / test / cover targets
└── README.md
```

## Testing policy

Every package under `internal/` has a `_test.go` file beside each
source file. Tests live in the same package (white-box) unless the
test needs to exercise the public surface only (then `_test` package
suffix).

| Package | What its tests cover | Style |
|---|---|---|
| `internal/config/` | env loading, default merging, provider normalization | table-driven |
| `internal/model/` | JSON marshal/unmarshal round-trips for every DTO | golden files |
| `internal/store/` | round-trip canvas/conversation/history JSON to a tmpdir | `t.TempDir()` |
| `internal/imageproc/` | encode/decode/resize/compose with checked-in PNG fixtures | byte-exact for PNG, perceptual hash for JPEG |
| `internal/upstream/` | response extraction (`extract.go`) against captured upstream JSON | table-driven, fixtures in `testdata/` |
| `internal/handler/` | hit each route via `httptest.NewRecorder`, assert status + JSON | request → response golden |
| `internal/ws/` | broadcast under concurrent connect/disconnect | `t.Parallel()` + race detector |
| `internal/server/` | middleware ordering, error formatting | httptest |

Mirror layout — `xxx.go` is accompanied by `xxx_test.go` in the same
directory:

```
internal/imageproc/
├── convert.go
├── convert_test.go
├── resize.go
├── resize_test.go
├── compose.go
├── compose_test.go
└── testdata/
    ├── alpha.png
    ├── photo.jpg
    └── expected/
        ├── alpha_flattened.png
        └── photo_resized_512.jpg
```

End-to-end tests live under `test/e2e/` and start the full
`server.App` against a tmpdir filesystem. They double as parity tests
during the port: each E2E test can be run against the Python server
too (via an env var that switches the target) to confirm response
equality.

Coverage targets (enforced by `make cover-check`):

| Package | Min coverage |
|---|---|
| `model/`, `store/`, `imageproc/`, `upstream/extract.go` | 85% |
| `handler/`, `server/`, `ws/`, `config/` | 70% |
| `cmd/` | exempt |

## Layering rules

```
cmd  →  server  →  handler  →  upstream | store | imageproc | ws
                       ↓
                     model
                       ↓
                    config
```

- **Handlers never call `http.Client` directly.** They delegate to
  `upstream/` clients, which expose typed methods returning typed
  responses (no `map[string]any` leaking back).
- **Handlers never touch disk directly.** All file IO goes through
  `store/`.
- **`model/` has zero dependencies on other internal packages.** Pure
  data structs. Tag-based binding (json + binding tags for Gin).
- **`config/` is read-only after startup.** Mutations to API providers
  live in `store/providers.go`.
- **`imageproc/` is stateless and pure.** Takes bytes / `image.Image`
  in, returns bytes / `image.Image` out.

## Why this shape

| Pain in current `main.py` | Resolution |
|---|---|
| 3349 lines in one file | Each handler file ~150-300 lines |
| Upstream API logic interleaved with handler logic | `upstream/` isolates protocol details |
| Image ops scattered through generate/save paths | `imageproc/` is a small library |
| `dict()` and `map[string]any` everywhere | Typed `model/` structs at every boundary |
| Locks declared at module level (`QUEUE_LOCK`, `HISTORY_LOCK`, ...) | `store.LockSet` keyed by resource |
| Global state (`GLOBAL_LOOP`, `manager`, `CLIENT_ID`) | Wired into a top-level `App` struct in `server/` |

## Module name

```
module github.com/<you>/infinite-canvas

go 1.22
```

(`go 1.22` for routing improvements in `net/http`; we use Gin but
1.22's stdlib improvements help library upgrades.)

## Top-level dependencies

| Package | Purpose |
|---|---|
| `github.com/gin-gonic/gin` | HTTP framework |
| `github.com/gorilla/websocket` | WebSocket |
| `github.com/joho/godotenv` | API/.env loader |
| `github.com/disintegration/imaging` | Resize / crop / filter |
| `github.com/fogleman/gg` | 2D composition / drawing |
| `golang.org/x/image` | Pulled in transitively for WebP, TIFF |
| `github.com/google/uuid` | uuid.NewString |
| `github.com/go-playground/validator/v10` | Pulled in by Gin |

No CGO required. `go build` produces a static binary.
