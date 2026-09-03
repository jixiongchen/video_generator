$ErrorActionPreference = "Stop"
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

# The template is tracked by Git. Fail safely without printing any credential.
# Real keys belong only in the ignored .env file, never in .env.example.
foreach ($templateLine in Get-Content -LiteralPath (Join-Path $root ".env.example")) {
  if ($templateLine -match '^\s*(TEXT_API_KEY|VIDEO_API_KEY)\s*=\s*(.+)$') {
    if ($Matches[2].Trim() -notin @("", '""', "''")) {
      throw "A non-empty API key is present in .env.example. Move it to .env and clear the template before testing or committing."
    }
  }
}

$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) {
  throw "Python was not found on PATH. Python 3.12+ is required."
}

$go = Get-Command go -ErrorAction SilentlyContinue
$goExecutable = $null
if ($go) {
  $goExecutable = $go.Source
} else {
  $localGo = Join-Path $root ".tools\go\bin\go.exe"
  if (Test-Path -LiteralPath $localGo) {
    $goExecutable = (Resolve-Path -LiteralPath $localGo).Path
  } else {
    throw "Go was not found on PATH or in .tools/go. Go 1.23+ is required."
  }
}

$env:PYTHONPATH = Join-Path $root "services\worker"
& $python.Source -m unittest discover -s (Join-Path $root "services\worker\tests") -v
if ($LASTEXITCODE -ne 0) { throw "Python tests failed." }

$env:GOCACHE = Join-Path $root ".cache\go-build"
$env:GOPATH = Join-Path $root ".cache\gopath"
Push-Location (Join-Path $root "services\api")
try {
  & $goExecutable test ./...
  if ($LASTEXITCODE -ne 0) { throw "Go tests failed." }
} finally {
  Pop-Location
}

pnpm --dir (Join-Path $root "apps\web") run build
if ($LASTEXITCODE -ne 0) { throw "Frontend build failed." }
