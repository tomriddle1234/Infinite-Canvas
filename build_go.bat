@echo off
setlocal
chcp 65001 > nul
cd /d "%~dp0"

if not exist "C:\src\miniforge\Scripts\activate.bat" (
  echo [ERROR] Missing C:\src\miniforge\Scripts\activate.bat
  exit /b 1
)

call C:\src\miniforge\Scripts\activate.bat OFX_dev
if errorlevel 1 exit /b %errorlevel%

pushd app-go
if not exist bin mkdir bin
set CGO_ENABLED=0
go test ./...
if errorlevel 1 (
  set EXIT_CODE=%errorlevel%
  popd
  exit /b %EXIT_CODE%
)
go build -trimpath -ldflags "-s -w" -o bin\Infinite-Canvas-Go.exe .\cmd\server
set EXIT_CODE=%errorlevel%
popd
if %EXIT_CODE% EQU 0 echo [go-build] Built app-go\bin\Infinite-Canvas-Go.exe
exit /b %EXIT_CODE%
