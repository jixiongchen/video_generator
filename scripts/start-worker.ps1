$ErrorActionPreference = "Stop"
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$envFile = Join-Path $projectRoot ".env"

. (Join-Path $PSScriptRoot "import-env.ps1")

if (-not (Test-Path -LiteralPath $envFile)) {
  throw "Copy .env.example to .env, then configure the key for the feature you use."
}
Import-ProjectEnv -Path $envFile

# Credentials are checked per feature inside the worker. Text-only projects do
# not need VIDEO_API_KEY; importing a novel does not require either API key.

$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) {
  throw "Python was not found on PATH. Python 3.12+ is required."
}

$env:PYTHONPATH = Join-Path $projectRoot "services\worker"
Set-Location $projectRoot
& $python.Source -m worker.app
