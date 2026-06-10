param(
  [switch]$IncludeUserData,
  [switch]$NoZip
)

$ErrorActionPreference = "Stop"

$GoDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$Root = Resolve-Path (Join-Path $GoDir "..")
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

$EmbedStatic = Join-Path $GoDir "web\static"
$EmbedWorkflows = Join-Path $GoDir "web\workflows"
Reset-Directory $EmbedStatic
Copy-Dir (Join-Path $Root "static") $EmbedStatic
$localPreset = Join-Path $EmbedStatic "system-prompts\templates\core-presets.local.json"
if (Test-Path $localPreset) {
  Remove-Item -LiteralPath $localPreset -Force
}
Reset-Directory $EmbedWorkflows
Copy-Dir (Join-Path $Root "workflows") $EmbedWorkflows

Push-Location $GoDir
try {
  New-Item -ItemType Directory -Force "bin" | Out-Null
  $env:CGO_ENABLED = "0"
  go mod tidy
  go test ./...
  go build -trimpath -ldflags "-s -w" -o $BuildExe .\cmd\server
} finally {
  Pop-Location
}

Reset-Directory $FinalDir
Copy-Item -LiteralPath $BuildExe -Destination $FinalExe -Force

foreach ($name in @("API", "assets", "assets\input", "assets\output", "assets\cache", "assets\cache\volcengine_assets", "data", "data\canvases", "data\conversations", "output", "workflows", "workflows\custom")) {
  New-Item -ItemType Directory -Force (Join-Path $FinalDir $name) | Out-Null
}

Copy-Dir (Join-Path $Root "workflows") (Join-Path $FinalDir "workflows")

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
"%~dp0Infinite-Canvas-Go.exe"
'@
Write-Utf8NoBom (Join-Path $FinalDir "Start-Go.bat") $startBat

$readme = @'
Infinite Canvas Go package

Run:
  Start-Go.bat

Runtime data is intentionally stored in the same file and directory layout as
the Python backend:
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
  Compress-Archive -Path (Join-Path $FinalDir "*") -DestinationPath $PackageZip -Force
  Write-Host "[go-package] ZIP: $PackageZip"
}

Write-Host "[go-package] Package: $FinalDir"
