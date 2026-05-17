@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

REM ============================================================
REM  Infinite-Canvas Nuitka --standalone one-click build
REM    Entry        : main_refactored.py  (the app/ package)
REM    Build cache  : build\nuitka\
REM    Final output : dist\Infinite-Canvas\
REM  Source tree runtime data (output\, data\, history.json,
REM  assets\, API\.env) is NOT touched.
REM ============================================================

set "CONDA_ENV=OFX_dev"
set "ENTRY=main_refactored.py"
set "APP_NAME=Infinite-Canvas"
set "DIST_DIR=dist"
set "BUILD_DIR=build\nuitka"
set "FINAL_DIR=%DIST_DIR%\%APP_NAME%"
set "NUITKA_OUT=%BUILD_DIR%\main_refactored.dist"

REM --- Locate miniforge (same fallback chain as run.bat) ---
set "CONDA_BAT="
if exist "C:\ProgramData\miniforge3\condabin\conda.bat" set "CONDA_BAT=C:\ProgramData\miniforge3\condabin\conda.bat"
if not defined CONDA_BAT if exist "%USERPROFILE%\miniforge3\condabin\conda.bat" set "CONDA_BAT=%USERPROFILE%\miniforge3\condabin\conda.bat"
if not defined CONDA_BAT if exist "%LOCALAPPDATA%\miniforge3\condabin\conda.bat" set "CONDA_BAT=%LOCALAPPDATA%\miniforge3\condabin\conda.bat"
if not defined CONDA_BAT if exist "C:\src\miniforge\condabin\conda.bat" set "CONDA_BAT=C:\src\miniforge\condabin\conda.bat"

if not defined CONDA_BAT (
    echo [ERROR] miniforge3 not found.
    pause
    exit /b 1
)

call "%CONDA_BAT%" activate %CONDA_ENV%
if errorlevel 1 (
    echo [ERROR] Failed to activate conda env: %CONDA_ENV%
    pause
    exit /b 1
)

REM --- Ensure Nuitka + helpers are installed in OFX_dev ---
python -c "import nuitka" 2>nul
if errorlevel 1 (
    echo Nuitka not found in %CONDA_ENV% -- installing nuitka + ordered-set + zstandard ...
    python -m pip install -U nuitka ordered-set zstandard
    if errorlevel 1 (
        echo [ERROR] Failed to install Nuitka
        pause
        exit /b 1
    )
)

REM --- Clean previous build output (NEVER touch source-tree runtime data) ---
if exist "%FINAL_DIR%" rmdir /s /q "%FINAL_DIR%"
if exist "%NUITKA_OUT%" rmdir /s /q "%NUITKA_OUT%"
if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"
if not exist "%BUILD_DIR%" mkdir "%BUILD_DIR%"

echo.
echo ============================================================
echo Building %APP_NAME%   (Nuitka --standalone)
echo Entry      : %ENTRY%
echo Cache dir  : %BUILD_DIR%
echo Output     : %FINAL_DIR%
echo Source-tree runtime dirs (output\, data\, history.json) are NOT touched.
echo First build may take 5-15 minutes. Incremental builds are much faster.
echo ============================================================
echo.

python -m nuitka %ENTRY% ^
  --standalone ^
  --output-filename=%APP_NAME%.exe ^
  --output-dir=%BUILD_DIR% ^
  --include-data-dir=static=static ^
  --include-data-dir=workflows=workflows ^
  --include-package=app ^
  --include-package=uvicorn ^
  --include-package=websockets ^
  --include-package=pydantic ^
  --nofollow-import-to=tkinter ^
  --nofollow-import-to=matplotlib ^
  --nofollow-import-to=numpy ^
  --nofollow-import-to=scipy ^
  --nofollow-import-to=pandas ^
  --nofollow-import-to=IPython ^
  --nofollow-import-to=pytest ^
  --assume-yes-for-downloads ^
  --remove-output ^
  --windows-console-mode=force ^
  --company-name=%APP_NAME% ^
  --product-name=%APP_NAME% ^
  --file-version=1.0.0

if errorlevel 1 (
    echo.
    echo [ERROR] Nuitka build failed. See log above.
    pause
    exit /b 1
)

if not exist "%NUITKA_OUT%" (
    echo [ERROR] Expected Nuitka output not found: %NUITKA_OUT%
    pause
    exit /b 1
)

REM --- Promote Nuitka *.dist folder to dist\Infinite-Canvas\ ---
move "%NUITKA_OUT%" "%FINAL_DIR%" >nul

REM --- Copy current API\.env so the build is ready-to-run for you ---
if exist "API\.env" (
    if not exist "%FINAL_DIR%\API" mkdir "%FINAL_DIR%\API"
    copy /y "API\.env" "%FINAL_DIR%\API\.env" >nul
    echo Copied your local API\.env into the build.
)

REM --- Generate end-user launcher (auto-opens browser, mirrors run.bat) ---
> "%FINAL_DIR%\Start.bat" echo @echo off
>>"%FINAL_DIR%\Start.bat" echo setlocal
>>"%FINAL_DIR%\Start.bat" echo cd /d "%%~dp0"
>>"%FINAL_DIR%\Start.bat" echo echo Starting %APP_NAME% ...
>>"%FINAL_DIR%\Start.bat" echo echo Visit http://127.0.0.1:3000/ ^(browser will open automatically^)
>>"%FINAL_DIR%\Start.bat" echo echo Press Ctrl+C to stop.
>>"%FINAL_DIR%\Start.bat" echo start "" powershell -WindowStyle Hidden -Command "Start-Sleep -Seconds 3; Start-Process 'http://127.0.0.1:3000/'"
>>"%FINAL_DIR%\Start.bat" echo "%%~dp0%APP_NAME%.exe"

REM --- README ---
> "%FINAL_DIR%\README.txt" echo %APP_NAME% portable build
>>"%FINAL_DIR%\README.txt" echo --------------------------------
>>"%FINAL_DIR%\README.txt" echo.
>>"%FINAL_DIR%\README.txt" echo Double-click Start.bat to launch (auto-opens browser at http://127.0.0.1:3000/).
>>"%FINAL_DIR%\README.txt" echo Or run %APP_NAME%.exe directly and open the URL manually.
>>"%FINAL_DIR%\README.txt" echo.
>>"%FINAL_DIR%\README.txt" echo The whole folder is portable -- copy it anywhere.
>>"%FINAL_DIR%\README.txt" echo Runtime data is created next to the exe on first run:
>>"%FINAL_DIR%\README.txt" echo   output\           generated images / video
>>"%FINAL_DIR%\README.txt" echo   assets\           uploaded inputs
>>"%FINAL_DIR%\README.txt" echo   data\             conversations / canvases / providers
>>"%FINAL_DIR%\README.txt" echo   history.json      task history
>>"%FINAL_DIR%\README.txt" echo   global_config.json  global UI settings
>>"%FINAL_DIR%\README.txt" echo.
>>"%FINAL_DIR%\README.txt" echo API keys: edit API\.env in this folder.

echo.
echo ============================================================
echo  BUILD DONE
echo  Folder : %FINAL_DIR%
echo  Launch : "%FINAL_DIR%\Start.bat"
echo ============================================================
echo.
pause
endlocal
