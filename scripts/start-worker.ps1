$ErrorActionPreference = "Stop"
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$envFile = Join-Path $projectRoot ".env"

. (Join-Path $PSScriptRoot "import-env.ps1")

if (-not (Test-Path -LiteralPath $envFile)) {
  throw "Copy .env.example to .env and configure VIDEO_API_KEY first."
}
Import-ProjectEnv -Path $envFile

if ([string]::IsNullOrWhiteSpace($env:VIDEO_API_KEY)) {
  throw "VIDEO_API_KEY is empty. Configure it in $envFile."
}

$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) {
  throw "Python was not found on PATH. Python 3.12+ is required."
}

$env:PYTHONPATH = Join-Path $projectRoot "services\worker"
Set-Location $projectRoot
& $python.Source -m worker.app
