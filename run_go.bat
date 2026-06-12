@echo off
setlocal
chcp 65001 > nul
cd /d "%~dp0"

if not exist "C:\src\miniforge\Scripts\activate.bat" (
  echo [ERROR] Missing C:\src\miniforge\Scripts\activate.bat
  exit /b 1
)

if "%INFINITE_CANVAS_GO_HOST%"=="" set INFINITE_CANVAS_GO_HOST=127.0.0.1
if "%INFINITE_CANVAS_GO_PORT%"=="" set INFINITE_CANVAS_GO_PORT=3000

call C:\src\miniforge\Scripts\activate.bat OFX_dev
if errorlevel 1 exit /b %errorlevel%

start "" "http://127.0.0.1:%INFINITE_CANVAS_GO_PORT%/"
pushd app-go
go run ./cmd/server
set EXIT_CODE=%errorlevel%
popd
exit /b %EXIT_CODE%
