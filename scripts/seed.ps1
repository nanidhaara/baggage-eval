# Windows version of seed.sh. Run from the project folder:
#   powershell -ExecutionPolicy Bypass -File scripts\seed.ps1
$ErrorActionPreference = "Stop"
$Api = if ($env:API) { $env:API } else { "http://localhost:8080" }

# The API only starts listening after it has connected to Postgres and
# RabbitMQ, so wait for /healthz instead of failing on the first request.
Write-Host "==> Waiting for the API at $Api ..."
$ready = $false
for ($i = 0; $i -lt 60; $i++) {
    try { Invoke-RestMethod "$Api/healthz" -TimeoutSec 2 | Out-Null; $ready = $true; break }
    catch { Start-Sleep -Seconds 1 }
}
if (-not $ready) { Write-Error "API did not become ready in 60 seconds. Check: docker compose logs api" }

Write-Host "==> Baggage records"
Invoke-RestMethod -Method Post -Uri "$Api/api/v1/baggage-records" -ContentType "application/json" `
    -InFile "testdata\baggage_records.json" | ConvertTo-Json -Depth 5

Write-Host "==> Predictions"
Invoke-RestMethod -Method Post -Uri "$Api/api/v1/predictions" -ContentType "application/json" `
    -InFile "testdata\predictions.json" | ConvertTo-Json -Depth 5

Write-Host "==> Waiting for the workers..."
Start-Sleep -Seconds 3

Write-Host "==> Quality report"
Invoke-RestMethod "$Api/api/v1/reports/quality" | ConvertTo-Json -Depth 5
