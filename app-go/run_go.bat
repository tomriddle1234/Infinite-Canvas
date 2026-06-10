@echo off
setlocal
chcp 65001 > nul
cd /d "%~dp0"
set INFINITE_CANVAS_GO_PORT=8080
call C:\src\miniforge\Scripts\activate.bat OFX_dev
go run ./cmd/server
