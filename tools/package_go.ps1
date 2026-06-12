param(
  [switch]$IncludeUserData,
  [switch]$NoZip
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$GoDir = Resolve-Path (Join-Path $Root "app-go")
$AppName = "Infinite-Canvas-Go"
$BuildExe = Join-Path $GoDir "bin\$AppName.exe"
$FinalDir = Join-Path $Root "dist\$AppName"
$FinalExe = Join-Path $FinalDir "$AppName.exe"
$PackageZip = Join-Path $Root "dist\$AppName.zip"

function Assert-UnderRoot([string]$Path) {
  $full = [System.IO.Path]::GetFullPath($Path)
  $rootFull = [System.IO.Path]::GetFullPath($Root)
  if (-not $full.StartsWith($rootFull, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to write outside workspace: $full"
  }
}

function Reset-Directory([string]$Path) {
  Assert-UnderRoot $Path
  if (Test-Path $Path) {
    Remove-Item -LiteralPath $Path -Recurse -Force
  }
  New-Item -ItemType Directory -Force $Path | Out-Null
}

function Copy-Dir([string]$Source, [string]$Destination) {
  if (-not (Test-Path $Source)) { return }
  New-Item -ItemType Directory -Force $Destination | Out-Null
  Copy-Item -Path (Join-Path $Source "*") -Destination $Destination -Recurse -Force
}

function Copy-FileIfExists([string]$Source, [string]$Destination) {
  if (-not (Test-Path $Source)) { return }
  New-Item -ItemType Directory -Force ([System.IO.Path]::GetDirectoryName($Destination)) | Out-Null
  Copy-Item -LiteralPath $Source -Destination $Destination -Force
}

function Write-Utf8NoBom([string]$Path, [string]$Text) {
  New-Item -ItemType Directory -Force ([System.IO.Path]::GetDirectoryName($Path)) | Out-Null
  [System.IO.File]::WriteAllText($Path, $Text, [System.Text.UTF8Encoding]::new($false))
}

Write-Host "[go-package] Root: $Root"
Write-Host "[go-package] Checking embedded assets..."

$EmbedStatic = Join-Path $GoDir "web\static"
$EmbedWorkflows = Join-Path $GoDir "web\workflows"
if (-not (Test-Path $EmbedStatic)) {
  throw "Missing embedded Go static directory: $EmbedStatic"
}
if (-not (Test-Path $EmbedWorkflows)) {
  throw "Missing embedded Go workflows directory: $EmbedWorkflows"
}

Push-Location $GoDir
try {
  Write-Host "[go-package] Preparing app-go\bin..."
  New-Item -ItemType Directory -Force "bin" | Out-Null
  $env:CGO_ENABLED = "0"
  Write-Host "[go-package] Running go mod tidy..."
  go mod tidy
  Write-Host "[go-package] Running go test ./..."
  go test ./...
  Write-Host "[go-package] Building $BuildExe..."
  go build -trimpath -ldflags "-s -w" -o $BuildExe .\cmd\server
} finally {
  Pop-Location
}

Write-Host "[go-package] Preparing package directory: $FinalDir"
Reset-Directory $FinalDir
Write-Host "[go-package] Copying executable..."
Copy-Item -LiteralPath $BuildExe -Destination $FinalExe -Force

Write-Host "[go-package] Creating runtime directories..."
foreach ($name in @("API", "assets", "assets\input", "assets\output", "assets\cache", "assets\cache\volcengine_assets", "data", "data\canvases", "data\conversations", "data\media_previews", "output", "workflows", "workflows\custom")) {
  New-Item -ItemType Directory -Force (Join-Path $FinalDir $name) | Out-Null
}

Write-Host "[go-package] Copying embedded workflows..."
Copy-Dir $EmbedWorkflows (Join-Path $FinalDir "workflows")

if ($IncludeUserData) {
  Write-Host "[go-package] Including local user runtime data. Check API/.env before sharing this package."
  Copy-FileIfExists (Join-Path $Root "API\.env") (Join-Path $FinalDir "API\.env")
  Copy-FileIfExists (Join-Path $Root "history.json") (Join-Path $FinalDir "history.json")
  Copy-FileIfExists (Join-Path $Root "global_config.json") (Join-Path $FinalDir "global_config.json")
  Copy-FileIfExists (Join-Path $Root "data\api_providers.json") (Join-Path $FinalDir "data\api_providers.json")
  Copy-FileIfExists (Join-Path $Root "data\seedance_tasks.json") (Join-Path $FinalDir "data\seedance_tasks.json")
  Copy-Dir (Join-Path $Root "data\canvases") (Join-Path $FinalDir "data\canvases")
  Copy-Dir (Join-Path $Root "data\conversations") (Join-Path $FinalDir "data\conversations")
  Copy-Dir (Join-Path $Root "assets") (Join-Path $FinalDir "assets")
  Copy-Dir (Join-Path $Root "output") (Join-Path $FinalDir "output")
}

$startBat = @'
@echo off
setlocal
chcp 65001 > nul
cd /d "%~dp0"

set INFINITE_CANVAS_BASE_DIR=%~dp0
set INFINITE_CANVAS_GO_HOST=127.0.0.1
if "%INFINITE_CANVAS_GO_PORT%"=="" set INFINITE_CANVAS_GO_PORT=3000

start "" "http://127.0.0.1:%INFINITE_CANVAS_GO_PORT%/"
echo [go-start] Starting Infinite-Canvas-Go.exe on 127.0.0.1:%INFINITE_CANVAS_GO_PORT%...
"%~dp0Infinite-Canvas-Go.exe"
set EXIT_CODE=%errorlevel%
echo [go-start] Server exited with code %EXIT_CODE%.
exit /b %EXIT_CODE%
'@
Write-Host "[go-package] Writing Start-Go.bat..."
Write-Utf8NoBom (Join-Path $FinalDir "Start-Go.bat") $startBat

$readme = @'
Infinite Canvas Go package

Run:
  Start-Go.bat

Runtime data is intentionally stored in the legacy-compatible file and
directory layout:
  API/.env
  data/api_providers.json
  data/canvases/
  data/conversations/
  data/seedance_tasks.json
  history.json
  global_config.json
  assets/
  output/
  workflows/

The default package does not copy local user data or API keys. Build with
package_go.bat -IncludeUserData only for a private migration package.
'@
Write-Utf8NoBom (Join-Path $FinalDir "README-GO.txt") $readme

if (-not $NoZip) {
  if (Test-Path $PackageZip) {
    Remove-Item -LiteralPath $PackageZip -Force
  }
  Write-Host "[go-package] Compressing ZIP..."
  Compress-Archive -Path (Join-Path $FinalDir "*") -DestinationPath $PackageZip -Force
  Write-Host "[go-package] ZIP: $PackageZip"
}

Write-Host "[go-package] Package: $FinalDir"
