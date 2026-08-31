param(
  [string]$ApiBaseUrl = "http://127.0.0.1:8080",
  [Parameter(Mandatory = $true)][string]$Model,
  [int]$TimeoutSeconds = 900
)

$ErrorActionPreference = "Stop"

$body = @{
  model = $Model
  prompt = "生成一个红色的奥迪R8跑车，在山地公路上飙车的激情视频。"
  generationMode = "t2v"
  resolutionTier = "480p"
  orientation = "landscape"
  seconds = 5
  seed = 42
} | ConvertTo-Json

$task = Invoke-RestMethod `
  -Uri "$ApiBaseUrl/api/v1/generations" `
  -Method Post `
  -ContentType "application/json" `
  -Body $body

$deadline = (Get-Date).AddSeconds($TimeoutSeconds)
do {
  Start-Sleep -Milliseconds 400
  $current = Invoke-RestMethod -Uri "$ApiBaseUrl/api/v1/generations/$($task.id)"
  Write-Host "$($current.status) $($current.progress)%"
} while ($current.status -in @("queued", "running", "waiting_provider") -and (Get-Date) -lt $deadline)

if ($current.status -ne "succeeded") {
  throw "Smoke test failed: $($current | ConvertTo-Json -Depth 6)"
}

Write-Host "Smoke test passed: $($current.id)" -ForegroundColor Green
