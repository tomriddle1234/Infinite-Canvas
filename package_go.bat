@echo off
setlocal
chcp 65001 > nul
cd /d "%~dp0"

if not exist "C:\src\miniforge\Scripts\activate.bat" (
  echo [ERROR] Missing C:\src\miniforge\Scripts\activate.bat
  exit /b 1
)

echo [go-package] Activating OFX_dev...
call C:\src\miniforge\Scripts\activate.bat OFX_dev
if errorlevel 1 exit /b %errorlevel%

echo [go-package] Running tools\package_go.ps1 %*
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0tools\package_go.ps1" %*
exit /b %errorlevel%
