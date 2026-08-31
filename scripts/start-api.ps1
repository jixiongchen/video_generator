$ErrorActionPreference = "Stop"
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$envFile = Join-Path $projectRoot ".env"

. (Join-Path $PSScriptRoot "import-env.ps1")

if (Test-Path -LiteralPath $envFile) {
  Import-ProjectEnv -Path $envFile
}

$go = Get-Command go -ErrorAction SilentlyContinue
$goExecutable = $null
if ($go) {
  $goExecutable = $go.Source
} else {
  $localGo = Join-Path $projectRoot ".tools\go\bin\go.exe"
  if (Test-Path -LiteralPath $localGo) {
    $goExecutable = (Resolve-Path -LiteralPath $localGo).Path
  } else {
    throw "Go was not found on PATH or in .tools/go. Go 1.23+ is required."
  }
}

$env:API_ADDR = if ($env:API_ADDR) { $env:API_ADDR } else { "127.0.0.1:8080" }
$env:WORKER_URL = if ($env:WORKER_URL) { $env:WORKER_URL } else { "http://127.0.0.1:8090" }
$env:DATA_DIR = Join-Path $projectRoot "data"
$env:WEB_DIST = Join-Path $projectRoot "apps\web\dist"
$env:GOCACHE = Join-Path $projectRoot ".cache\go-build"
$env:GOPATH = Join-Path $projectRoot ".cache\gopath"

Push-Location (Join-Path $projectRoot "services\api")
try {
  & $goExecutable run .\cmd\server
} finally {
  Pop-Location
}
