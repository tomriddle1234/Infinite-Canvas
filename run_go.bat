@echo off
setlocal
chcp 65001 > nul
cd /d "%~dp0"

set RUN_GO_BUILD_ONLY=
if /I "%~1"=="--build-only" set RUN_GO_BUILD_ONLY=1

if not exist "C:\src\miniforge\Scripts\activate.bat" (
  echo [ERROR] Missing C:\src\miniforge\Scripts\activate.bat
  exit /b 1
)

if "%INFINITE_CANVAS_GO_HOST%"=="" set INFINITE_CANVAS_GO_HOST=127.0.0.1
if "%INFINITE_CANVAS_GO_PORT%"=="" set INFINITE_CANVAS_GO_PORT=3000

echo [go-run] Activating OFX_dev...
call C:\src\miniforge\Scripts\activate.bat OFX_dev
if errorlevel 1 exit /b %errorlevel%

echo [go-run] Building app-go\bin\Infinite-Canvas-Go.exe...
pushd app-go
if not exist bin mkdir bin
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w" -o bin\Infinite-Canvas-Go.exe .\cmd\server
set EXIT_CODE=%errorlevel%
popd
if not "%EXIT_CODE%"=="0" (
  echo [go-run] Build failed with exit code %EXIT_CODE%.
  exit /b %EXIT_CODE%
)

echo [go-run] Build OK: app-go\bin\Infinite-Canvas-Go.exe
if "%RUN_GO_BUILD_ONLY%"=="1" (
  echo [go-run] Build-only requested; server was not started.
  exit /b 0
)

echo [go-run] Starting server on %INFINITE_CANVAS_GO_HOST%:%INFINITE_CANVAS_GO_PORT%...
echo [go-run] Opening http://127.0.0.1:%INFINITE_CANVAS_GO_PORT%/
start "" "http://127.0.0.1:%INFINITE_CANVAS_GO_PORT%/"
"%~dp0app-go\bin\Infinite-Canvas-Go.exe"
set EXIT_CODE=%errorlevel%
echo [go-run] Server exited with code %EXIT_CODE%.
exit /b %EXIT_CODE%
