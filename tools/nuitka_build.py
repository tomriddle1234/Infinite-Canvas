"""Nuitka build helper for Infinite-Canvas.

The batch file only activates the project environment. This script owns the
incremental decisions and final package layout:

  dist/Infinite-Canvas/
    Start.bat
    README.txt
    static/
    workflows/
    runtime/Infinite-Canvas.exe + Nuitka dependencies

Use --onefile when you specifically want a single root exe. Static assets and
workflows remain external so frontend-only edits do not force a Python rebuild.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
ENTRY = ROOT / "main_refactored.py"
APP_NAME = "Infinite-Canvas"
BUILD_DIR = ROOT / "build" / "nuitka"
FINAL_DIR = ROOT / "dist" / APP_NAME
RUNTIME_DIR = FINAL_DIR / "runtime"
STANDALONE_STAGING = BUILD_DIR / "main_refactored.dist"
ONEFILE_STAGING = BUILD_DIR / f"{APP_NAME}.exe"
MANIFEST = BUILD_DIR / "manifest.json"

PYTHON_SOURCE_PATTERNS = (
    "main_refactored.py",
    "app/**/*.py",
    "requirements.txt",
    "run_nuitka.bat",
    "tools/nuitka_build.py",
)

STATIC_DIRS = ("static", "workflows")
RUNTIME_NAMES = {"API", "assets", "data", "output", "history.json", "global_config.json"}
ROOT_CLUTTER_SUFFIXES = (".pyd", ".dll", ".exe", ".lib", ".cat", ".manifest")
PRESERVE_FINAL_NAMES = RUNTIME_NAMES | {"runtime", "static", "workflows", "Start.bat", "README.txt"}


def rel(path: Path) -> str:
    return path.relative_to(ROOT).as_posix()


def iter_source_files() -> list[Path]:
    seen: set[Path] = set()
    files: list[Path] = []
    for pattern in PYTHON_SOURCE_PATTERNS:
        for path in ROOT.glob(pattern):
            if path.is_file() and path not in seen:
                seen.add(path)
                files.append(path)
    return sorted(files)


def hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def source_signature(mode: str) -> dict[str, object]:
    return {
        "mode": mode,
        "entry": rel(ENTRY),
        "python": {rel(path): hash_file(path) for path in iter_source_files()},
        "nuitka_args_version": 2,
    }


def load_manifest() -> dict[str, object]:
    if not MANIFEST.exists():
        return {}
    try:
        return json.loads(MANIFEST.read_text(encoding="utf-8"))
    except Exception:
        return {}


def save_manifest(signature: dict[str, object]) -> None:
    BUILD_DIR.mkdir(parents=True, exist_ok=True)
    MANIFEST.write_text(json.dumps(signature, indent=2, sort_keys=True), encoding="utf-8")


def ensure_nuitka(skip_pip: bool) -> None:
    try:
        import nuitka  # noqa: F401
    except Exception:
        if skip_pip:
            raise SystemExit("[ERROR] Nuitka is not installed in this environment.")
        print("Nuitka not found in OFX_dev; installing nuitka + ordered-set + zstandard ...")
        subprocess.check_call(
            [sys.executable, "-m", "pip", "install", "-U", "nuitka", "ordered-set", "zstandard"],
            cwd=ROOT,
        )


def remove_path(path: Path) -> None:
    if not path.exists():
        return
    if path.is_dir():
        shutil.rmtree(path)
    else:
        path.unlink()


def copy_tree(src: Path, dst: Path) -> None:
    if not src.exists():
        return
    if dst.exists():
        shutil.rmtree(dst)
    shutil.copytree(src, dst, ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))


def sync_external_assets() -> None:
    FINAL_DIR.mkdir(parents=True, exist_ok=True)
    for name in STATIC_DIRS:
        copy_tree(ROOT / name, FINAL_DIR / name)

    api_env = ROOT / "API" / ".env"
    if api_env.exists():
        api_dir = FINAL_DIR / "API"
        api_dir.mkdir(parents=True, exist_ok=True)
        shutil.copy2(api_env, api_dir / ".env")
        print("Copied local API/.env into the build.")


def clean_old_flat_runtime_files() -> None:
    if not FINAL_DIR.exists():
        return
    for item in FINAL_DIR.iterdir():
        if item.name in PRESERVE_FINAL_NAMES:
            continue
        if item.is_dir():
            shutil.rmtree(item)
        elif item.is_file() and (item.suffix.lower() in ROOT_CLUTTER_SUFFIXES or item.name.startswith(APP_NAME)):
            item.unlink()


def write_launcher(mode: str) -> None:
    exe_path = f"%~dp0{APP_NAME}.exe" if mode == "onefile" else f"%~dp0runtime\\{APP_NAME}.exe"
    lines = [
        "@echo off",
        "setlocal",
        'cd /d "%~dp0"',
        f"set INFINITE_CANVAS_BASE_DIR=%~dp0",
        f"echo Starting {APP_NAME} ...",
        "echo Visit http://127.0.0.1:3000/ (browser will open automatically)",
        "echo Press Ctrl+C to stop.",
        'start "" powershell -WindowStyle Hidden -Command "Start-Sleep -Seconds 3; Start-Process \'http://127.0.0.1:3000/\'"',
        f'"{exe_path}"',
    ]
    (FINAL_DIR / "Start.bat").write_text("\r\n".join(lines) + "\r\n", encoding="ascii")


def write_readme(mode: str) -> None:
    layout = "Infinite-Canvas.exe is in this folder." if mode == "onefile" else "Compiled runtime files live under runtime/ to keep this folder tidy."
    text = f"""{APP_NAME} portable build
--------------------------------

Double-click Start.bat to launch (auto-opens browser at http://127.0.0.1:3000/).
{layout}

Static frontend files and workflows are external on purpose:
  static\\
  workflows\\

That lets frontend-only changes be copied into this folder without rebuilding
the Python executable.

Runtime data is created next to Start.bat on first run:
  output\\
  assets\\
  data\\
  history.json
  global_config.json

API keys: edit API\\.env in this folder.
"""
    (FINAL_DIR / "README.txt").write_text(text, encoding="utf-8")


def nuitka_common_args() -> list[str]:
    jobs = max(1, (os.cpu_count() or 2) - 1)
    return [
        sys.executable,
        "-m",
        "nuitka",
        str(ENTRY),
        f"--output-filename={APP_NAME}.exe",
        f"--output-dir={BUILD_DIR}",
        f"--jobs={jobs}",
        "--include-package=app",
        "--include-package=uvicorn",
        "--include-package=websockets",
        "--include-package=pydantic",
        "--nofollow-import-to=tkinter",
        "--nofollow-import-to=matplotlib",
        "--nofollow-import-to=numpy",
        "--nofollow-import-to=scipy",
        "--nofollow-import-to=pandas",
        "--nofollow-import-to=IPython",
        "--nofollow-import-to=pytest",
        "--assume-yes-for-downloads",
        "--windows-console-mode=force",
        f"--company-name={APP_NAME}",
        f"--product-name={APP_NAME}",
        "--file-version=1.0.0",
    ]


def run_nuitka(mode: str) -> None:
    BUILD_DIR.mkdir(parents=True, exist_ok=True)
    if mode == "onefile":
        remove_path(ONEFILE_STAGING)
        command = nuitka_common_args() + ["--onefile"]
    else:
        remove_path(STANDALONE_STAGING)
        command = nuitka_common_args() + ["--standalone"]

    print()
    print("=" * 60)
    print(f"Building {APP_NAME} with Nuitka ({mode})")
    print(f"Entry     : {rel(ENTRY)}")
    print(f"Cache dir : {rel(BUILD_DIR)}")
    print(f"Output    : {rel(FINAL_DIR)}")
    print("Static/workflows are copied after compilation and do not trigger Nuitka.")
    print("=" * 60)
    print()
    subprocess.check_call(command, cwd=ROOT)


def promote_build(mode: str) -> None:
    FINAL_DIR.mkdir(parents=True, exist_ok=True)
    clean_old_flat_runtime_files()
    if mode == "onefile":
        if not ONEFILE_STAGING.exists():
            raise SystemExit(f"[ERROR] Expected Nuitka output not found: {ONEFILE_STAGING}")
        remove_path(FINAL_DIR / f"{APP_NAME}.exe")
        shutil.move(str(ONEFILE_STAGING), str(FINAL_DIR / f"{APP_NAME}.exe"))
        remove_path(RUNTIME_DIR)
    else:
        if not STANDALONE_STAGING.exists():
            raise SystemExit(f"[ERROR] Expected Nuitka output not found: {STANDALONE_STAGING}")
        remove_path(RUNTIME_DIR)
        shutil.move(str(STANDALONE_STAGING), str(RUNTIME_DIR))
        remove_path(FINAL_DIR / f"{APP_NAME}.exe")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Incremental Nuitka build for Infinite-Canvas.")
    parser.add_argument("--mode", choices=("standalone", "onefile"), default="standalone")
    parser.add_argument("--onefile", action="store_true", help="Shortcut for --mode onefile.")
    parser.add_argument("--force", action="store_true", help="Force Nuitka even if Python sources are unchanged.")
    parser.add_argument("--clean", action="store_true", help="Remove build/nuitka and dist/Infinite-Canvas before building.")
    parser.add_argument("--skip-pip", action="store_true", help="Do not auto-install Nuitka if it is missing.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    mode = "onefile" if args.onefile else args.mode

    if args.clean:
        remove_path(BUILD_DIR)
        remove_path(FINAL_DIR)

    ensure_nuitka(skip_pip=args.skip_pip)

    signature = source_signature(mode)
    manifest = load_manifest()
    expected_exe = FINAL_DIR / f"{APP_NAME}.exe" if mode == "onefile" else RUNTIME_DIR / f"{APP_NAME}.exe"
    needs_compile = args.force or manifest != signature or not expected_exe.exists()

    if needs_compile:
        run_nuitka(mode)
        promote_build(mode)
        save_manifest(signature)
    else:
        print(f"Nuitka skipped: Python sources unchanged for {mode} build.")

    sync_external_assets()
    write_launcher(mode)
    write_readme(mode)

    print()
    print("=" * 60)
    print("BUILD DONE")
    print(f"Folder : {FINAL_DIR}")
    print(f"Launch : {FINAL_DIR / 'Start.bat'}")
    if not needs_compile:
        print("Only static/workflow/API files were refreshed.")
    print("=" * 60)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
