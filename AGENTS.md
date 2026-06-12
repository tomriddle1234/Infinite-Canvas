# Infinite-Canvas Agent Guide

This file defines the default project instructions for any new agent conversation opened in this workspace.

## Startup

- At the start of every new conversation, first read [.agents/rules/cmdrule.md](.agents/rules/cmdrule.md).
- Treat [.agents/rules/cmdrule.md](.agents/rules/cmdrule.md) as the source of truth for command execution on this project.
- Do not ask the user to remind you to load rules if they are already present in this repository.

## Upstream Sync Rule

- This repository is a heavily-customized fork of `hero8152/Infinite-Canvas` with **no shared git history** with upstream.
- When the user asks to "merge / sync / pull in upstream / 拉原作者的更新", **read [doc/upstream-sync-playbook.md](doc/upstream-sync-playbook.md) before doing anything else**.
- Do **not** run `git merge upstream/main`, `git rebase upstream/main`, or `git cherry-pick <upstream-sha>` — they will fail or produce garbage. The playbook explains the content-based diff workflow that actually works and lists the user customizations that must not be reverted.

## Windows Command Rule

- Before running Python, pip, or server-launch commands on Windows, use the environment activation flow defined in `cmdrule.md`.
- Updated 2026-06-12: the active runtime is Go-first. Use the root Go scripts (`run_go.bat`, `build_go.bat`, `package_go.bat`) for run/build/package unless the user explicitly asks to inspect or revive deprecated Python code.
- The required command wrapper is:

```bat
cmd /c "chcp 65001 > nul && C:\src\miniforge\Scripts\activate.bat OFX_dev && <command>"
```

~~`cmd /c "chcp 65001 > nul && C:\ProgramData\miniforge3\condabin\conda.bat activate OFX_dev && <command>"`~~ (Corrected 2026-06-03: follow `.agents/rules/cmdrule.md`; the active project env path is `C:\src\miniforge\Scripts\activate.bat`.)

- ~~Do not construct ad-hoc Visual Studio / vcvarsall environment commands — this is a pure Python project.~~
- Updated 2026-06-12: do not construct ad-hoc Visual Studio / vcvarsall environment commands; Go is provided by the `OFX_dev` environment for this project.
- For one-off Python calls, `C:/src/miniforge/envs/OFX_dev/python.exe <args>` is also acceptable (~~`C:/Users/DAN/.conda/envs/OFX_dev/python.exe <args>`~~, Corrected 2026-06-03: follow `.agents/rules/cmdrule.md`).

## Run Rule

- ~~`run.bat` is the default Windows end-user entrypoint (uses the embedded `python\python.exe`).~~
- ~~`run.bat` is the default Windows entrypoint — it activates the `OFX_dev` conda env and launches `main.py`.~~
- ~~`run_refactored.bat` is the default Windows entrypoint — it launches `main_refactored.py` and opens `http://127.0.0.1:3000/`.~~
- Updated 2026-06-12: `run_go.bat` is the default Windows entrypoint. It activates `OFX_dev`, runs `app-go/cmd/server`, and opens `http://127.0.0.1:3000/`.
- Updated 2026-06-12: use `build_go.bat` for a compile/test build and `package_go.bat` for the standalone package.
- ~~For development / verification runs in the current checkout, launch `main_refactored.py` under the activated `OFX_dev` env:~~

```bat
cmd /c "chcp 65001 > nul && C:\src\miniforge\Scripts\activate.bat OFX_dev && python main_refactored.py"
```

~~`cmd /c "chcp 65001 > nul && C:\ProgramData\miniforge3\condabin\conda.bat activate OFX_dev && python main.py"`~~ (Corrected 2026-06-03: follow `.agents/rules/cmdrule.md`; the active project env path is `C:\src\miniforge\Scripts\activate.bat`.)
~~`cmd /c "chcp 65001 > nul && C:\src\miniforge\Scripts\activate.bat OFX_dev && python main.py"`~~ (Corrected 2026-06-03: current checkout uses `main_refactored.py`; `main.py` is not present.)

- Do not replace these flows with hand-written uvicorn invocations unless the user explicitly asks for that.
- Updated 2026-06-12: do not replace the Go script flow with hand-written `go run` / `go build` invocations unless the user explicitly asks for that.

> Updated 2026-05-16: the embedded Python distribution at `python\`
> was removed. `run.bat` and the manual activation form now point at
> the same conda env, so they are functionally equivalent.

## Editing Rule

- ~~Preserve the existing project layout (`main.py`, `static/`, `workflows/`, `packages/`, `python/`).~~
- ~~Preserve the existing project layout (`main.py`, `static/`, `static/vendor/`, `workflows/`, `doc/`).~~
- Updated 2026-06-12: preserve the Go-first layout (`app-go/internal/`, `app-go/web/static/`, `app-go/web/workflows/`, root `run_go.bat` / `build_go.bat` / `package_go.bat`, `deprecated/python/`).
- ~~Make focused source changes; avoid unrelated cleanup of the FastAPI app shell, CORS / logging setup, or static mounts.~~
- Updated 2026-06-12: make active backend changes in `app-go/internal/` and active frontend changes in `app-go/web/static/`; treat `deprecated/python/` as archival unless explicitly requested.
- ~~Never modify files inside the embedded `python\` directory — that is a vendored runtime, not source.~~

> Updated 2026-05-16: `packages/` and `python/` were removed from the
> repo. Frontend assets that used to be CDN-loaded now live under
> `static/vendor/`. `doc/` holds the Go migration plan.

## Dependency Rule

- ~~Declared dependencies live in `requirements.txt`.~~
- Updated 2026-06-12: active Go dependencies live in `app-go/go.mod`; legacy Python dependencies live in `deprecated/python/requirements.txt`.
- Install or inspect dependencies inside `OFX_dev`, never against system Python.

## Verification Rule

- ~~After meaningful code changes, run at minimum a Python import check via the `OFX_dev` interpreter and, when the change touches request handling, boot the FastAPI app to confirm it starts.~~
- Updated 2026-06-12: after meaningful Go changes, run `build_go.bat` or at minimum `go test ./...` from `app-go` under activated `OFX_dev`.
- Report clearly whether:
  - Go test/build or boot succeeded,
  - a runtime error remains and where it is raised,
  - or the change is UI-only and was not browser-tested (say so explicitly rather than claiming success).

## Skills

- If a task clearly matches an installed Claude Code skill, use it.
- For project-specific work in this repository, apply this `AGENTS.md` and the files under `.agents/rules/` before inventing a custom workflow.
