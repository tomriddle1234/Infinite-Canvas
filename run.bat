@echo off
cd /d "%~dp0"
echo Starting ComfyUI-API-Modelscope...
echo Visit: http://127.0.0.1:3000/
echo Press Ctrl+C to stop.
echo.
start /b cmd /c "timeout /t 3 /nobreak >nul && start http://127.0.0.1:3000/"
python main.py
echo.
echo Server stopped.
pause