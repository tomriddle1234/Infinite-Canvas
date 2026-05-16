# Infinite-Canvas

A web-based infinite canvas for image and video generation. Three backends
supported, mix and match in the same canvas:

- **ComfyUI** — local workflows (import any `.json`, expose params as nodes)
- **OpenAI-compatible API** — any provider speaking the OpenAI / async protocol
- **ModelScope** — free generation tier, LoRA supported

## Requirements

- Python 3.12+
- A modern browser (Chrome / Edge / Firefox)
- Optional: a local ComfyUI install for the ComfyUI backend

All frontend dependencies (Tailwind, Lucide, Three.js, fonts) are bundled
under `static/vendor/`. The server runs fully offline.

## Run

```
python -m venv .venv
.venv\Scripts\python -m pip install -r requirements.txt
.venv\Scripts\python main.py
```

Then open http://127.0.0.1:3000/

On Windows you can double-click `run.bat` instead (it activates the
miniforge env `OFX_dev` and starts the server).

## Configure

In the lower-left of the web UI:

- **API settings** — request URL, protocol (OpenAI / async), API key. Models
  are auto-fetched and selectable.
- **ComfyUI settings** — point at your local ComfyUI instance, optionally
  import custom workflows.
- **ModelScope** — set a ModelScope key to use the free generation tier.

## Layout

```
main.py                FastAPI backend (single file)
requirements.txt
run.bat                Windows launcher (activates miniforge env)
static/
  *.html               Pages (canvas, settings, login, ...)
  vendor/              Self-hosted JS / CSS / fonts
workflows/             ComfyUI workflow templates
```
