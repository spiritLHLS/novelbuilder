$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root

$Python = if ($env:PYTHON_BIN) { $env:PYTHON_BIN } else { "python" }
$Version = if ($env:VERSION) { $env:VERSION } else { "local" }

Write-Host "==> Preparing Python sidecar"
if (-not (Test-Path "python-sidecar/.venv")) {
  & $Python -m venv "python-sidecar/.venv"
}
$VenvPython = Join-Path $Root "python-sidecar/.venv/Scripts/python.exe"
& $VenvPython -m pip install -q --upgrade pip
& $VenvPython -m pip install -q -r "python-sidecar/requirements.txt"
if (Test-Path "python-sidecar/novel-downloader/pyproject.toml") {
  & $VenvPython -m pip install -q "./python-sidecar/novel-downloader"
}

Write-Host "==> Building frontend"
Push-Location "frontend"
& npm ci --legacy-peer-deps
& npm run build
Pop-Location

Write-Host "==> Building backend"
Push-Location "backend"
& go build -trimpath -ldflags "-s -w -buildid= -X main.version=${Version}" -o (Join-Path $Root "novelbuilder.exe") "./cmd/server"
Pop-Location

Write-Host ""
Write-Host "Install complete."
Write-Host "Default run mode uses local SQLite at .\data\novelbuilder.db."
Write-Host "Set DB_DRIVER=postgres plus DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME for PostgreSQL."
Write-Host "Start: powershell -ExecutionPolicy Bypass -File .\scripts\run-local.ps1"
