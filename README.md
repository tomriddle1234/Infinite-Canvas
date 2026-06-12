# Infinite-Canvas

A web-based infinite canvas for image and video generation. Three backends
supported, mix and match in the same canvas:

- **ComfyUI** — local workflows (import any `.json`, expose params as nodes)
- **OpenAI-compatible API** — any provider speaking the OpenAI / async protocol
- **ModelScope** — free generation tier, LoRA supported

## Current Status

Updated 2026-06-12: the active app is Go-first. The legacy Python backend,
root static tree, Python run scripts, and Python tests were moved to
`deprecated/python/` for reference.

Use the root scripts:

```bat
run_go.bat
build_go.bat
package_go.bat
```

`run_go.bat` opens http://127.0.0.1:3000/.

## Requirements

- ~~Python 3.12+~~
- Go from the `OFX_dev` environment
- A modern browser (Chrome / Edge / Firefox)
- Optional: a local ComfyUI install for the ComfyUI backend

~~All frontend dependencies (Tailwind, Lucide, Three.js, fonts) are bundled
under `static/vendor/`. The server runs fully offline.~~

Updated 2026-06-12: active frontend dependencies are bundled under
`app-go/web/static/vendor/`. The Go server embeds these assets.

## Run

Updated 2026-06-12:

```bat
run_go.bat
```

Then open http://127.0.0.1:3000/ if the browser did not open automatically.

Legacy Python flow:

```
python -m venv .venv
.venv\Scripts\python -m pip install -r requirements.txt
.venv\Scripts\python main.py
```

Then open http://127.0.0.1:3000/

~~On Windows you can double-click `run.bat` instead (it activates the
miniforge env `OFX_dev` and starts the server).~~

Updated 2026-06-12: the Python launcher lives at
`deprecated/python/run_refactored.bat` and is archival.

## Configure

In the lower-left of the web UI:

- **API settings** — request URL, protocol (OpenAI / async), API key. Models
  are auto-fetched and selectable.
- **ComfyUI settings** — point at your local ComfyUI instance, optionally
  import custom workflows.
- **ModelScope** — set a ModelScope key to use the free generation tier.

## Layout

```
app-go/
  cmd/server/          Go entrypoint
  internal/            Go backend
  web/static/          Embedded frontend
  web/workflows/       Embedded workflow templates
run_go.bat             Windows Go launcher
build_go.bat           Go test/build helper
package_go.bat         Standalone package helper
deprecated/python/     Legacy Python backend, static tree, scripts, tests
```

Legacy layout before 2026-06-12:

```
main.py                FastAPI backend (single file)
requirements.txt
run.bat                Windows launcher (activates miniforge env)
static/
  *.html               Pages (canvas, settings, login, ...)
  vendor/              Self-hosted JS / CSS / fonts
workflows/             ComfyUI workflow templates
```
