# Start PostgreSQL & Redis
Write-Host "Starting databases..." -ForegroundColor Yellow
docker-compose up -d

Write-Host "Waiting for PostgreSQL to be ready..." -ForegroundColor Yellow
Start-Sleep -Seconds 5

Write-Host "Checking PostgreSQL..." -ForegroundColor Yellow
docker exec textile-erp-db pg_isready -U erp_user -d textile_erp

if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ PostgreSQL is ready!" -ForegroundColor Green
} else {
    Write-Host "⚠️ PostgreSQL may not be ready yet" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "Now start the server: .\start-server.ps1" -ForegroundColor Green
