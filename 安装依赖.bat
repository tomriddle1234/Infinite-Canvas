@echo off
cd /d "%~dp0"
echo ============================================
echo   Installing dependencies (offline)
echo ============================================
echo.

python --version >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Python not found. Please install Python 3.10+
    echo Download: https://www.python.org/downloads/
    pause
    exit /b 1
)

python --version

echo.
echo [1/2] Checking pip...
python -m pip --version >nul 2>&1
if errorlevel 1 (
    echo pip not found, bootstrapping...
    python -m ensurepip --upgrade
)

echo [2/2] Installing from local packages folder...
python -m pip install --no-index --find-links=packages -r requirements.txt

if errorlevel 1 (
    echo.
    echo [WARN] Offline install failed, trying online...
    python -m pip install -r requirements.txt
)

echo.
echo Done. Run "启动服务.bat" to start.
pause