$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ((Test-Path (Join-Path $ScriptDir "python-sidecar")) -or (Test-Path (Join-Path $ScriptDir "frontend"))) {
  $Root = $ScriptDir
} else {
  $Root = Split-Path -Parent $ScriptDir
}
Set-Location $Root

$Python = if ($env:PYTHON_BIN) { $env:PYTHON_BIN } else { "python" }
$SidecarHost = if ($env:SIDECAR_HOST) { $env:SIDECAR_HOST } else { "127.0.0.1" }
$SidecarPort = if ($env:SIDECAR_PORT) { $env:SIDECAR_PORT } else { "8081" }
$ServerPort = if ($env:SERVER_PORT) { $env:SERVER_PORT } else { "8080" }

$env:APP_PROFILE = if ($env:APP_PROFILE) { $env:APP_PROFILE } else { "binary" }
$env:SERVER_HOST = if ($env:SERVER_HOST) { $env:SERVER_HOST } else { "0.0.0.0" }
$env:SERVER_PORT = $ServerPort
$env:SIDECAR_URL = if ($env:SIDECAR_URL) { $env:SIDECAR_URL } else { "http://${SidecarHost}:${SidecarPort}" }
$env:DB_DRIVER = if ($env:DB_DRIVER) { $env:DB_DRIVER } else { "sqlite" }
$env:DB_HOST = if ($env:DB_HOST) { $env:DB_HOST } else { "127.0.0.1" }
$env:DB_PORT = if ($env:DB_PORT) { $env:DB_PORT } else { "5432" }
$env:DB_USER = if ($env:DB_USER) { $env:DB_USER } else { "novelbuilder" }
$env:DB_PASSWORD = if ($env:DB_PASSWORD) { $env:DB_PASSWORD } else { "novelbuilder" }
$env:DB_NAME = if ($env:DB_NAME) { $env:DB_NAME } else { "novelbuilder" }
$env:DB_SSLMODE = if ($env:DB_SSLMODE) { $env:DB_SSLMODE } else { "disable" }
$env:SQLITE_PATH = if ($env:SQLITE_PATH) { $env:SQLITE_PATH } else { Join-Path $Root "data/novelbuilder.db" }
$env:REDIS_ENABLED = if ($env:REDIS_ENABLED) { $env:REDIS_ENABLED } else { "false" }
$env:REDIS_URL = if ($env:REDIS_URL) { $env:REDIS_URL } else { "" }
$env:NEO4J_URI = if ($env:NEO4J_URI) { $env:NEO4J_URI } else { "" }
$env:QDRANT_URL = if ($env:QDRANT_URL) { $env:QDRANT_URL } else { "" }

if ($env:DB_DRIVER -eq "sqlite" -or $env:DB_DRIVER -eq "sqlite3") {
  $SqliteDir = Split-Path -Parent $env:SQLITE_PATH
  if ($SqliteDir -and -not (Test-Path $SqliteDir)) {
    New-Item -ItemType Directory -Force -Path $SqliteDir | Out-Null
  }
  New-Item -ItemType Directory -Force -Path (Join-Path $Root "data/uploads") | Out-Null
} elseif ($env:DB_DRIVER -eq "postgres" -and $env:SKIP_DB_CHECK -ne "1") {
  $tcp = New-Object Net.Sockets.TcpClient
  try {
    $async = $tcp.BeginConnect($env:DB_HOST, [int]$env:DB_PORT, $null, $null)
    if (-not $async.AsyncWaitHandle.WaitOne(3000)) {
      throw "PostgreSQL is not reachable at $($env:DB_HOST):$($env:DB_PORT)."
    }
    $tcp.EndConnect($async)
  } finally {
    $tcp.Close()
  }
} elseif ($env:DB_DRIVER -ne "postgres") {
  throw "Unsupported DB_DRIVER=$($env:DB_DRIVER); use sqlite or postgres."
}

if (-not (Test-Path "python-sidecar/.venv")) {
  & $Python -m venv "python-sidecar/.venv"
}

$VenvPython = Join-Path $Root "python-sidecar/.venv/Scripts/python.exe"
& $VenvPython -m pip install -q --upgrade pip
& $VenvPython -m pip install -q -r "python-sidecar/requirements.txt"
if (Test-Path "python-sidecar/novel-downloader/pyproject.toml") {
  & $VenvPython -m pip install -q "./python-sidecar/novel-downloader"
}

if (Test-Path "python-sidecar/runtime_capabilities.py") {
  Push-Location "python-sidecar"
  & $VenvPython -c "from runtime_capabilities import detect_accelerators; c=detect_accelerators(); print('accelerator=' + c.get('selected_accelerator','cpu'))"
  Pop-Location
}

$Backend = if ($env:BACKEND_BIN) { $env:BACKEND_BIN } else { Join-Path $Root "novelbuilder.exe" }
if (-not (Test-Path $Backend)) {
  if (Test-Path "backend") {
    Push-Location "backend"
    $BuildVersion = if ($env:VERSION) { $env:VERSION } else { "local" }
    & go build -trimpath -ldflags "-s -w -buildid= -X main.version=$BuildVersion" -o $Backend "./cmd/server"
    Pop-Location
  } else {
    throw "backend binary not found: $Backend"
  }
}

if (-not (Test-Path "frontend/dist")) {
  if (Test-Path "frontend") {
    Push-Location "frontend"
    & npm ci --legacy-peer-deps
    & npm run build
    Pop-Location
  } else {
    throw "frontend/dist not found"
  }
}

$Sidecar = Start-Process -FilePath $VenvPython -ArgumentList @("-m", "uvicorn", "main:app", "--host", $SidecarHost, "--port", $SidecarPort) -WorkingDirectory (Join-Path $Root "python-sidecar") -PassThru
try {
  Write-Host "NovelBuilder setup page: http://127.0.0.1:${ServerPort}/setup"
  & $Backend
} finally {
  if ($Sidecar -and -not $Sidecar.HasExited) {
    Stop-Process -Id $Sidecar.Id -Force
  }
}
