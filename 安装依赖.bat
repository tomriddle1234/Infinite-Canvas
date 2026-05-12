@echo off
cd /d "%~dp0"
echo ============================================
echo   Installing dependencies
echo ============================================
echo.

python --version >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Python not found.
    echo Please install Python 3.10 or newer from:
    echo https://www.python.org/downloads/
    echo IMPORTANT: Check "Add Python to PATH" during installation.
    pause
    exit /b 1
)

python --version
echo.

echo [1/3] Ensuring pip is available...
python -m pip --version >nul 2>&1
if errorlevel 1 (
    echo pip not found, bootstrapping with ensurepip...
    python -m ensurepip --upgrade
    if errorlevel 1 (
        echo [ERROR] Failed to bootstrap pip.
        pause
        exit /b 1
    )
)

echo.
echo [2/3] Trying offline install from packages folder...
python -m pip install --no-index --find-links=packages -r requirements.txt
if not errorlevel 1 (
    echo.
    echo [OK] Offline install succeeded.
    goto :done
)

echo.
echo [3/3] Offline failed - trying online install...
python -m pip install --upgrade pip
python -m pip install -r requirements.txt
if errorlevel 1 (
    echo.
    echo [ERROR] Both offline and online install failed.
    echo Possible causes:
    echo   1. Python version not supported (need 3.10+)
    echo   2. No internet connection
    echo   3. Network blocked by firewall/proxy
    pause
    exit /b 1
)

:done
echo.
echo ============================================
echo  Done. Run the start script.
echo ============================================
pause
