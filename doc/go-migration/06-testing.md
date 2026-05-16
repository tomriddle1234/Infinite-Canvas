# 06 — Testing Strategy

Unit, integration, and parity tests are first-class deliverables of
every port phase. A phase is "done" only when its tests pass and its
package meets the coverage target in
[`01-architecture.md`](01-architecture.md#testing-policy).

## Toolchain

Standard library only, plus two well-known helpers. No exotic
test frameworks.

| Tool | Purpose |
|---|---|
| `testing` (stdlib) | All test bodies. Table-driven where possible. |
| `net/http/httptest` (stdlib) | Spin Gin handlers without a real listener. |
| `github.com/stretchr/testify/require` | Terse assertions. `require.NoError`, `require.JSONEq`, `require.Equal`. |
| `github.com/google/go-cmp/cmp` | Deep diff for structs (better failure messages than `reflect.DeepEqual`). |
| `go test -race` | Always on in CI. WebSocket and store code must be race-clean. |
| `go test -cover` | Coverage report per package. |

Tests must run with `go test ./...` from a clean clone — no network,
no env vars, no docker, no Python on the box.

## Test types and where they live

```
internal/imageproc/convert_test.go     ← unit, white-box, package imageproc
internal/handler/canvas_test.go        ← unit, white-box, package handler
test/e2e/server_test.go                ← integration, package e2e
test/e2e/parity_test.go                ← parity vs Python, opt-in
```

### Unit tests

Beside each source file. Use table-driven style:

```go
func TestEncodeJPEG_flattensAlpha(t *testing.T) {
    cases := []struct {
        name     string
        input    string  // path under testdata/
        quality  int
        wantDims image.Point
    }{
        {"plain-rgb", "photo.jpg", 88, image.Pt(1024, 768)},
        {"rgba-flattened", "alpha.png", 88, image.Pt(512, 512)},
        {"small-bypass", "tiny.png", 88, image.Pt(64, 64)},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            img := mustLoad(t, tc.input)
            out, err := EncodeJPEG(img, tc.quality)
            require.NoError(t, err)
            got, _, err := image.Decode(bytes.NewReader(out))
            require.NoError(t, err)
            require.Equal(t, tc.wantDims, got.Bounds().Size())
        })
    }
}
```

### Integration tests (handlers)

Build the full Gin engine, hit it with `httptest`:

```go
func TestListCanvases_emptyDir(t *testing.T) {
    app := newTestApp(t)  // wires real store against t.TempDir()
    req := httptest.NewRequest("GET", "/api/canvases", nil)
    w := httptest.NewRecorder()
    app.Engine().ServeHTTP(w, req)
    require.Equal(t, 200, w.Code)
    require.JSONEq(t, `{"canvases":[]}`, w.Body.String())
}
```

`newTestApp(t)` is the only test helper. It wires a fresh `App` with:

- store paths under `t.TempDir()` (cleaned up automatically by `testing.T`)
- a no-op upstream client (or a stub registered per-test)
- a recording WebSocket manager (collects broadcasts in memory)

### E2E tests

`test/e2e/server_test.go` spins the real server, no mocks:

```go
func TestE2E_canvasLifecycle(t *testing.T) {
    srv := startTestServer(t)
    defer srv.Close()

    // create
    res := httpPost(t, srv, "/api/canvases", `{"title":"hello"}`)
    var created struct{ ID string `json:"id"` }
    require.NoError(t, json.Unmarshal(res, &created))

    // list shows it
    list := httpGet(t, srv, "/api/canvases")
    require.Contains(t, string(list), created.ID)

    // delete + trash + restore + hard-delete
    // ...
}
```

E2E tests exercise full request cycles including WebSocket broadcasts.

### Parity tests (port-time only)

`test/e2e/parity_test.go` is gated behind an env var:

```go
func TestParity_canvasesEndpoint(t *testing.T) {
    if os.Getenv("PARITY_PY_URL") == "" {
        t.Skip("set PARITY_PY_URL to enable parity tests")
    }
    pyResp := httpGet(t, os.Getenv("PARITY_PY_URL"), "/api/canvases")
    goResp := httpGet(t, startTestServer(t).URL, "/api/canvases")
    require.JSONEq(t, string(pyResp), string(goResp))
}
```

Run during the port to catch JSON drift:

```
PARITY_PY_URL=http://localhost:3000 go test ./test/e2e -run TestParity
```

Once the port is complete and the Python server is retired, these
tests stay in the repo but always skip. They're cheap insurance if
someone resurrects the Python server later.

### Upstream protocol tests

`internal/upstream/extract.go` is the highest-risk surface (see
[`05-risks.md`](05-risks.md#2-probe-many-json-paths-extraction-code)).
Capture real responses once and check them in:

```
internal/upstream/testdata/
├── apimart/
│   ├── async_submit_ok.json
│   ├── poll_pending.json
│   ├── poll_finished.json
│   └── poll_failed.json
├── openai/
│   ├── chat_completion.json
│   ├── chat_stream_chunk.json
│   └── image_generation.json
├── modelscope/
│   └── ...
└── comfyui/
    └── history_with_outputs.json
```

Each file gets one or more table rows asserting what `extract.go`
should return. Adding a new provider quirk = add a JSON fixture + a
test row, never modify the helper without a regression test.

## Makefile targets

```makefile
test:
	go test -race -count=1 ./...

test-short:
	go test -race -short ./...

cover:
	go test -race -coverprofile=cover.out ./...
	go tool cover -html=cover.out -o cover.html

cover-check: cover
	@go run ./scripts/covercheck cover.out
```

`scripts/covercheck/main.go` reads the per-package thresholds from
[`01-architecture.md`](01-architecture.md#testing-policy) and exits
non-zero if any package is below target. Wire it into pre-push or CI.

## CI

`.github/workflows/test.yml`:

```yaml
name: test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - run: make cover-check
```

Runs on every push. Build job (see
[`04-build-and-ship.md`](04-build-and-ship.md#ci-build-github-actions-sketch))
depends on the test job passing.

## Phase-by-phase test deliverables

Augment [`03-port-plan.md`](03-port-plan.md) phases with:

| Phase | New tests required |
|---|---|
| 0 — Scaffold | `internal/server/static_test.go`: embed.FS serves `index.html`. |
| 1 — Read-only | Per-handler unit tests + one E2E per endpoint. |
| 2 — Mutations + WS | Store round-trip tests; WS broadcast under load (`-race`). |
| 3 — Image + output | `imageproc/` table-driven tests with checked-in fixtures. |
| 4 — Upstream | `upstream/extract_test.go` with captured response JSON. |
| 5 — Cutover | Full E2E suite green + parity suite green against Python. |

## Test data hygiene

- All fixtures under `testdata/` (Go convention: `go test` ignores it
  by default, so it doesn't pollute coverage).
- No real API keys in fixtures. Scrub upstream responses before
  checking in: search for `sk-`, `Bearer `, email addresses.
- No PNGs over 64 KB in `testdata/`. Generate larger images
  on the fly in the test if needed.

## What we explicitly do not test

- Image encoder byte-equality. JPEG output bytes are encoder-specific;
  we check decoded dimensions / mode instead.
- LLM response content. Stub the upstream client; never call real
  providers from tests.
- The bundled frontend. Out of scope for backend tests. (A separate
  Playwright suite could be added later if needed; not part of this
  migration.)
