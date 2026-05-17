@echo off
setlocal
cd /d "%~dp0"

REM ============================================================
REM  Infinite-Canvas Nuitka build
REM    Default      : incremental standalone build
REM    Build cache  : build\nuitka\
REM    Final output : dist\Infinite-Canvas\
REM
REM  Useful options:
REM    run_nuitka.bat --force      Force Python recompilation
REM    run_nuitka.bat --clean      Clean build/dist first
REM    run_nuitka.bat --onefile    Build a single root exe instead of runtime\
REM
REM  static\ and workflows\ are copied after compilation, so frontend-only
REM  edits do not trigger a Nuitka rebuild.
REM ============================================================

set "CONDA_ENV=OFX_dev"

REM Locate miniforge (same fallback chain as run.bat)
set "CONDA_BAT="
if exist "C:\ProgramData\miniforge3\condabin\conda.bat" set "CONDA_BAT=C:\ProgramData\miniforge3\condabin\conda.bat"
if not defined CONDA_BAT if exist "%USERPROFILE%\miniforge3\condabin\conda.bat" set "CONDA_BAT=%USERPROFILE%\miniforge3\condabin\conda.bat"
if not defined CONDA_BAT if exist "%LOCALAPPDATA%\miniforge3\condabin\conda.bat" set "CONDA_BAT=%LOCALAPPDATA%\miniforge3\condabin\conda.bat"
if not defined CONDA_BAT if exist "C:\src\miniforge\condabin\conda.bat" set "CONDA_BAT=C:\src\miniforge\condabin\conda.bat"

if not defined CONDA_BAT (
    echo [ERROR] miniforge3 not found.
    pause
    exit /b 1
)

call "%CONDA_BAT%" activate %CONDA_ENV%
if errorlevel 1 (
    echo [ERROR] Failed to activate conda env: %CONDA_ENV%
    pause
    exit /b 1
)

python tools\nuitka_build.py %*
set "BUILD_EXIT=%ERRORLEVEL%"
if not "%BUILD_EXIT%"=="0" (
    echo.
    echo [ERROR] Nuitka build failed.
    pause
    exit /b %BUILD_EXIT%
)

echo.
pause
endlocal
