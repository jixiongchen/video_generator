param([switch]$ConfirmCost)
$ErrorActionPreference = "Stop"
if (-not $ConfirmCost) { throw "This calls the real text API (up to two requests). Re-run with -ConfirmCost to authorize the cost." }
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
. (Join-Path $PSScriptRoot "import-env.ps1")
Import-ProjectEnv -Path (Join-Path $projectRoot ".env")
if ([string]::IsNullOrWhiteSpace($env:TEXT_API_KEY)) { throw "Configure TEXT_API_KEY in the root .env file first." }
$env:PYTHONPATH = Join-Path $projectRoot "services\worker"
python -m worker.agents.novel.smoke --confirm-cost
if ($LASTEXITCODE -ne 0) { throw "Text API smoke test failed. Do not keep retrying if billing is uncertain." }
