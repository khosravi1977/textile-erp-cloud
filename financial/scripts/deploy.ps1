# Deploy ERP Textile to Production
Write-Host "🚀 Deploying ERP Textile to Production..." -ForegroundColor Green

# Build and start all services
docker-compose -f docker-compose.prod.yml up -d --build

# Wait for services
Write-Host "⏳ Waiting for services to be ready..." -ForegroundColor Yellow
Start-Sleep -Seconds 10

# Health check
$health = try { Invoke-RestMethod -Uri "http://localhost:8081/health" } catch { $null }
if ($health.status -eq "ok") {
    Write-Host "✅ Deploy SUCCESS!" -ForegroundColor Green
    Write-Host "🌐 http://localhost:8081/health" -ForegroundColor Cyan
} else {
    Write-Host "❌ Deploy failed - check logs: docker-compose logs" -ForegroundColor Red
}
