# 04 — Build and Ship

## Single-exe via `embed.FS`

The frontend, default workflows, and any other static assets are
compiled into the binary with the standard `embed` package.

### `internal/server/static.go`

```go
package server

import (
    "embed"
    "io/fs"
    "net/http"

    "github.com/gin-gonic/gin"
)

//go:embed web/static/*
var staticFS embed.FS

//go:embed web/workflows/*.json
var workflowFS embed.FS

func mountStatic(r *gin.Engine) {
    sub, _ := fs.Sub(staticFS, "web/static")
    r.StaticFS("/static", http.FS(sub))
    r.GET("/", func(c *gin.Context) {
        c.FileFromFS("web/static/index.html", http.FS(staticFS))
    })
}

// BuiltinWorkflows returns embedded workflow JSONs.
func BuiltinWorkflows() fs.FS {
    sub, _ := fs.Sub(workflowFS, "web/workflows")
    return sub
}
```

Workflows are served at runtime by overlaying the embedded defaults
with whatever the user has saved to disk (`workflows/` directory next
to the exe). User-saved workflows always win.

## Makefile

```makefile
BIN := infinite-canvas
GO_VERSION := $(shell go version)
VERSION ?= dev-$(shell git rev-parse --short HEAD)
LDFLAGS := -s -w -X github.com/<you>/infinite-canvas/internal/version.Version=$(VERSION)

.PHONY: build run tidy test cross

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN).exe ./cmd/server

run:
	go run ./cmd/server

tidy:
	go mod tidy

test:
	go test ./...

cross:
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BIN)-windows-amd64.exe ./cmd/server
	GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BIN)-darwin-arm64       ./cmd/server
	GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BIN)-linux-amd64        ./cmd/server
```

Flags:

- `-trimpath`: strips local paths from the binary (smaller, reproducible).
- `-s -w`: strips DWARF debug + symbol table (saves ~20% size).
- `-X version.Version=...`: injects build version printed at startup.

Expected binary size: **~15-20 MB** for the Windows amd64 build,
including the embedded frontend (~2 MB) and the three.js library (~1.3 MB).

## Cross-compile

Because no CGO is used, cross-compilation is one-shot:

```
make cross
```

Produces:

```
dist/infinite-canvas-windows-amd64.exe
dist/infinite-canvas-darwin-arm64
dist/infinite-canvas-linux-amd64
```

No toolchain juggling, no Docker. This is the single biggest
ergonomic win over the current Python distribution story.

## Runtime layout on a user's machine

The exe expects (and creates if missing) these directories next to itself:

```
infinite-canvas.exe        ← the binary
output/                    ← generated images (created on first run)
assets/
  input/                   ← uploaded references
  output/                  ← processed outputs
data/
  conversations/
  canvases/
  api_providers.json
workflows/                 ← user-saved workflows (defaults are embedded)
history.json
global_config.json
API/
  .env                     ← API keys
```

Same layout as the current Python project, intentionally — so users
upgrading from the Python version keep their data.

## CI build (GitHub Actions sketch)

`.github/workflows/build.yml`:

```yaml
name: build
on:
  push:
    tags: ["v*"]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: make cross
      - uses: actions/upload-artifact@v4
        with:
          name: binaries
          path: dist/*
```

Tag a release → CI produces three binaries → attach to GitHub release.
Users download one file and run it.

## Comparison vs current Python distribution

| | Python (current) | Go (target) |
|---|---|---|
| First-run install | `python -m venv .venv` + `pip install` | none — just run the exe |
| User needs Python installed | yes (3.12+) | no |
| Total bytes on disk | ~150 MB (venv) | ~20 MB (exe) |
| Cold start | ~1 s | <50 ms |
| Cross-platform build | per-platform venv | `make cross`, three files |
| Updating | re-clone + reinstall | drop new exe in place |

Updated 2026-06-10: the current Nuitka release path is better than a
loose Python venv for end users, but it still has a high developer-time
cost. A small Python source change can invalidate the compiled module
graph and make Nuitka walk thousands of generated C / extension files
again. The packaging script can reuse copied runtime files and skip
unchanged zips, but it cannot turn Nuitka into a cheap incremental
compiler for arbitrary Python edits. A Go target avoids this class of
problem because the runtime, server code, and static frontend build into
one normal executable with `go build`.

For the Go release artifact, keep the distribution shape deliberately
boring:

```
dist/
  Infinite-Canvas.exe
  Start.bat
  README.txt
```

`Start.bat` should only launch the exe next to it. It should not create
or activate Python environments.
