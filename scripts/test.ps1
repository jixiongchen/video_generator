$ErrorActionPreference = "Stop"
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) {
  throw "Python was not found on PATH. Python 3.12+ is required."
}

$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
  $localGo = Join-Path $root ".tools\go\bin\go.exe"
  if (Test-Path -LiteralPath $localGo) {
    $go = Get-Item $localGo
  } else {
    throw "Go was not found on PATH or in .tools/go. Go 1.23+ is required."
  }
}

$env:PYTHONPATH = Join-Path $root "services\worker"
& $python.Source -m unittest discover -s (Join-Path $root "services\worker\tests") -v

$env:GOCACHE = Join-Path $root ".cache\go-build"
$env:GOPATH = Join-Path $root ".cache\gopath"
Push-Location (Join-Path $root "services\api")
try {
  & $go.Source test ./...
} finally {
  Pop-Location
}

pnpm --dir (Join-Path $root "apps\web") run build

