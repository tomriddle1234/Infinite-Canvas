@echo off
setlocal
chcp 65001 > nul
cd /d "%~dp0"

if not exist "C:\src\miniforge\Scripts\activate.bat" (
  echo [ERROR] Missing C:\src\miniforge\Scripts\activate.bat
  exit /b 1
)

echo [go-build] Activating OFX_dev...
call C:\src\miniforge\Scripts\activate.bat OFX_dev
if errorlevel 1 exit /b %errorlevel%

echo [go-build] Preparing app-go\bin...
pushd app-go
if not exist bin mkdir bin
set CGO_ENABLED=0
echo [go-build] Running go test ./...
go test ./...
if errorlevel 1 (
  set EXIT_CODE=%errorlevel%
  popd
  echo [go-build] Tests failed with exit code %EXIT_CODE%.
  exit /b %EXIT_CODE%
)
echo [go-build] Building app-go\bin\Infinite-Canvas-Go.exe...
go build -trimpath -ldflags "-s -w" -o bin\Infinite-Canvas-Go.exe .\cmd\server
set EXIT_CODE=%errorlevel%
popd
if %EXIT_CODE% EQU 0 (
  echo [go-build] Build OK: app-go\bin\Infinite-Canvas-Go.exe
) else (
  echo [go-build] Build failed with exit code %EXIT_CODE%.
)
exit /b %EXIT_CODE%
