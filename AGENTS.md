# Infinite-Canvas Agent Guide

This file defines the default project instructions for any new agent conversation opened in this workspace.

## Startup

- At the start of every new conversation, first read [.agents/rules/cmdrule.md](.agents/rules/cmdrule.md).
- Treat [.agents/rules/cmdrule.md](.agents/rules/cmdrule.md) as the source of truth for command execution on this project.
- Do not ask the user to remind you to load rules if they are already present in this repository.

## Windows Command Rule

- Before running Python, pip, or server-launch commands on Windows, use the environment activation flow defined in `cmdrule.md`.
- The required command wrapper is:

```bat
cmd /c "chcp 65001 > nul && C:\ProgramData\miniforge3\condabin\conda.bat activate OFX_dev && <command>"
```

~~`cmd /c "chcp 65001 > nul && C:\src\miniforge\Scripts\activate.bat OFX_dev && <command>"`~~ (Updated 2026-05-16: miniforge path corrected.)

- Do not construct ad-hoc Visual Studio / vcvarsall environment commands — this is a pure Python project.
- For one-off Python calls, `C:/Users/DAN/.conda/envs/OFX_dev/python.exe <args>` is also acceptable (~~`C:/src/miniforge/envs/OFX_dev/python.exe <args>`~~, Updated 2026-05-16).

## Run Rule

- ~~`run.bat` is the default Windows end-user entrypoint (uses the embedded `python\python.exe`).~~
- `run.bat` is the default Windows entrypoint — it activates the `OFX_dev` conda env and launches `main.py`.
- For development / verification runs, prefer launching `main.py` under the activated `OFX_dev` env:

```bat
cmd /c "chcp 65001 > nul && C:\ProgramData\miniforge3\condabin\conda.bat activate OFX_dev && python main.py"
```

~~`cmd /c "chcp 65001 > nul && C:\src\miniforge\Scripts\activate.bat OFX_dev && python main.py"`~~ (Updated 2026-05-16.)

- Do not replace these flows with hand-written uvicorn invocations unless the user explicitly asks for that.

> Updated 2026-05-16: the embedded Python distribution at `python\`
> was removed. `run.bat` and the manual activation form now point at
> the same conda env, so they are functionally equivalent.

## Editing Rule

- ~~Preserve the existing project layout (`main.py`, `static/`, `workflows/`, `packages/`, `python/`).~~
- Preserve the existing project layout (`main.py`, `static/`, `static/vendor/`, `workflows/`, `doc/`).
- Make focused source changes; avoid unrelated cleanup of the FastAPI app shell, CORS / logging setup, or static mounts.
- ~~Never modify files inside the embedded `python\` directory — that is a vendored runtime, not source.~~

> Updated 2026-05-16: `packages/` and `python/` were removed from the
> repo. Frontend assets that used to be CDN-loaded now live under
> `static/vendor/`. `doc/` holds the Go migration plan.

## Dependency Rule

- Declared dependencies live in `requirements.txt`.
- Install or inspect dependencies inside `OFX_dev`, never against system Python.

## Verification Rule

- After meaningful code changes, run at minimum a Python import check via the `OFX_dev` interpreter and, when the change touches request handling, boot the FastAPI app to confirm it starts.
- Report clearly whether:
  - import / boot succeeded,
  - a runtime error remains and where it is raised,
  - or the change is UI-only and was not browser-tested (say so explicitly rather than claiming success).

## Skills

- If a task clearly matches an installed Claude Code skill, use it.
- For project-specific work in this repository, apply this `AGENTS.md` and the files under `.agents/rules/` before inventing a custom workflow.
