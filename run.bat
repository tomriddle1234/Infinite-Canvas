@echo off
setlocal
cd /d "%~dp0"

set "CONDA_ENV=OFX_dev"

REM Locate miniforge (system-wide first, then per-user fallbacks)
set "CONDA_BAT="
if exist "C:\ProgramData\miniforge3\condabin\conda.bat" set "CONDA_BAT=C:\ProgramData\miniforge3\condabin\conda.bat"
if not defined CONDA_BAT if exist "%USERPROFILE%\miniforge3\condabin\conda.bat" set "CONDA_BAT=%USERPROFILE%\miniforge3\condabin\conda.bat"
if not defined CONDA_BAT if exist "%LOCALAPPDATA%\miniforge3\condabin\conda.bat" set "CONDA_BAT=%LOCALAPPDATA%\miniforge3\condabin\conda.bat"

if not defined CONDA_BAT (
    echo [ERROR] miniforge3 not found.
    echo Looked in: C:\ProgramData\miniforge3, %%USERPROFILE%%\miniforge3, %%LOCALAPPDATA%%\miniforge3
    pause
    exit /b 1
)

call "%CONDA_BAT%" activate %CONDA_ENV%
if errorlevel 1 (
    echo [ERROR] Failed to activate conda env: %CONDA_ENV%
    echo Create it with: conda create -n %CONDA_ENV% python=3.12
    pause
    exit /b 1
)

echo Starting Infinite-Canvas (env: %CONDA_ENV%)...
echo Visit: http://127.0.0.1:3000/
echo Press Ctrl+C to stop.
echo.

start /b cmd /c "timeout /t 3 /nobreak >nul && start http://127.0.0.1:3000/"
python main.py

echo.
echo Server stopped.
pause
