---
trigger: always_on
---

## Documentation Update Rules
- **NEVER** delete existing content when updating documentation such as `.md` files.
- **Incremental Updates only**: append or insert new information instead of removing historical context.
- **Handling Obsolete Info**: if information is outdated, do **NOT** delete it.
  - Use strikethrough, for example `~~old info~~`.
  - Add a timestamp note, for example `Updated 2026-05-16: ...`.
- **Redundancy Over Loss**: preserving technical rationale is more important than avoiding repetition.

# Project Rules and Environment

This document defines the command and run conventions for the `Infinite-Canvas` project.

## Runtime Environment
- Preferred Conda environment: `OFX_dev`
- ~~Windows activation script: `C:\src\miniforge\Scripts\activate.bat`~~
- ~~macOS activation script: `/opt/homebrew/Caskroom/miniforge/base/bin/activate`~~

> Updated 2026-05-16: miniforge3 on this machine lives at
> `C:\ProgramData\miniforge3\`. Activation:
> `C:\ProgramData\miniforge3\condabin\conda.bat activate OFX_dev`
> Direct interpreter: `C:\Users\DAN\.conda\envs\OFX_dev\python.exe`
> macOS path is unchanged in spirit; verify on the target Mac before
> using the path above.

~~The repository also ships an embedded interpreter at `python\python.exe` used by `run.bat`. That embedded interpreter is for end-user runtime only — for development, debugging, dependency inspection, and any command an agent runs, use the `OFX_dev` miniforge environment.~~

> Updated 2026-05-16: the embedded `python\` interpreter and
> `packages\` wheel cache were removed from the repo. `run.bat` now
> activates the `OFX_dev` conda env directly. Both end-user runs and
> dev runs use the same env — the historical split no longer applies.

## Command Execution Strategy

### Windows
When running project commands from PowerShell on Windows, use this form:

```cmd
cmd /c "chcp 65001 > nul && C:\ProgramData\miniforge3\condabin\conda.bat activate OFX_dev && <your_command>"
```

~~`cmd /c "chcp 65001 > nul && C:\src\miniforge\Scripts\activate.bat OFX_dev && <your_command>"`~~ (Updated 2026-05-16: miniforge path corrected.)

- Use the quoted `cmd /c "..."` form so PowerShell passes the `&&` chain to `cmd` instead of trying to parse it itself.
- `chcp 65001` ensures UTF-8 output so Chinese log messages and file paths render correctly.
- If a trailing no-op is useful for a specific command, it may be appended inside the quoted command string.
- Do not construct ad-hoc Visual Studio / vcvarsall environment commands — this is a pure Python project and does not need them.

### Direct interpreter form
For one-off Python invocations where activation overhead is undesirable, calling the env interpreter directly is also acceptable:

```cmd
C:/Users/DAN/.conda/envs/OFX_dev/python.exe <args>
```

~~`C:/src/miniforge/envs/OFX_dev/python.exe <args>`~~ (Updated 2026-05-16: env path corrected.)

Prefer the `cmd /c "...activate.bat OFX_dev && ..."` wrapper when the command needs PATH-resolved tools (`pip`, `uvicorn`, etc.) or when it spawns subprocesses that expect the env to be active.

### macOS
- Run commands directly in the shell.
- ~~Use the `OFX_dev` environment when dependencies are required (`source /opt/homebrew/Caskroom/miniforge/base/bin/activate OFX_dev`).~~

> Updated 2026-05-16: macOS activation path depends on the install
> location of miniforge. Verify with `which conda` on the Mac before
> hardcoding a path.

## Run / Launch Rule
- ~~The default Windows entrypoint for end users is `run.bat`, which launches `main.py` against the embedded `python\python.exe` and opens `http://127.0.0.1:3000/`.~~
- For development verification, prefer running `main.py` under the `OFX_dev` env:

```cmd
cmd /c "chcp 65001 > nul && C:\ProgramData\miniforge3\condabin\conda.bat activate OFX_dev && python main.py"
```

~~`cmd /c "chcp 65001 > nul && C:\src\miniforge\Scripts\activate.bat OFX_dev && python main.py"`~~

- Do not invent ad-hoc launch scripts when `run.bat` or the env-activated `python main.py` already covers the workflow.

> Updated 2026-05-16: `run.bat` no longer uses an embedded interpreter
> — it now calls `conda activate OFX_dev` itself. So `run.bat` and the
> manual `cmd /c "...activate OFX_dev && python main.py"` form are
> functionally equivalent.

## Dependency Rule
- Project Python dependencies are declared in `requirements.txt`.
- Install / inspect dependencies inside `OFX_dev`, not against system Python:

```cmd
cmd /c "chcp 65001 > nul && C:\ProgramData\miniforge3\condabin\conda.bat activate OFX_dev && pip install -r requirements.txt"
```

~~`cmd /c "chcp 65001 > nul && C:\src\miniforge\Scripts\activate.bat OFX_dev && pip install -r requirements.txt"`~~

- ~~Do not modify the embedded `python\` directory's site-packages directly.~~

> Updated 2026-05-16: the embedded `python\` directory no longer
> exists. All dependency installs land in the `OFX_dev` conda env's
> site-packages, which is the only Python the project uses.

## Editing Rule
- ~~Preserve the existing project structure (`main.py`, `static/`, `workflows/`, `packages/`).~~
- Preserve the existing project structure (`main.py`, `static/`, `workflows/`, `doc/`, `static/vendor/`).
- Make focused source changes; avoid unrelated refactors of the FastAPI app shell, logging setup, or static asset layout unless the task requires it.
- When adding routes or workflow handlers, follow the existing patterns in `main.py` rather than introducing a new framework/abstraction layer.

> Updated 2026-05-16: `packages/` was removed (no more bundled wheels);
> `static/vendor/` was added (locally hosted Tailwind / Lucide /
> three.js / fonts); `doc/` contains the Go migration plan.

## Verification Rule
- After meaningful code changes, verify by:
  1. Python syntax / import check via the env interpreter, e.g. `C:/Users/DAN/.conda/envs/OFX_dev/python.exe -c "import main"` (~~`C:/src/miniforge/envs/OFX_dev/python.exe -c "import main"`~~, Updated 2026-05-16).
  2. Booting the FastAPI app and hitting the relevant endpoint when the change is server-side behavior.
- Report clearly whether:
  - the import / boot succeeded,
  - a runtime error remains and where it is raised,
  - or the change is UI-only and was not browser-tested (say so explicitly rather than claiming success).

## Skills
- If a task clearly matches an installed Claude Code skill, use it.
- For project-specific work in this repository, apply this `cmdrule.md` and the top-level `AGENTS.md` before inventing a custom workflow.
