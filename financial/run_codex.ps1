# run_codex.ps1
$APIKey = $env:GAPGPT_API_KEY
if ([string]::IsNullOrWhiteSpace($APIKey)) {
    throw 'Set GAPGPT_API_KEY in your environment before running this script.'
}
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  CODEX â†’ GapGPT" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Kill existing proxies
Write-Host "[*] Stopping existing proxies..." -ForegroundColor Yellow
Get-Process -Name "mitmdump" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 2

# Create redirect.py
$redirectScript = "$env:USERPROFILE\.mitmproxy\redirect.py"
$mitmDir = "$env:USERPROFILE\.mitmproxy"

if (-not (Test-Path $mitmDir)) {
    New-Item -ItemType Directory -Path $mitmDir -Force
}

Write-Host "[*] Creating redirect script..." -ForegroundColor Yellow

$redirectContent = @"
from mitmproxy import http
from datetime import datetime

API_KEY = "$APIKey"

class GapGPTRedirect:
    def __init__(self):
        self.target = "api.gapgpt.app"
        self.port = 443
    
    def request(self, flow: http.HTTPFlow) -> None:
        timestamp = datetime.now().strftime("%H:%M:%S")
        
        if "api.openai.com" in flow.request.pretty_host:
            original_path = flow.request.path
            flow.request.host = self.target
            flow.request.scheme = "https"
            flow.request.port = self.port
            
            if not flow.request.path.startswith("/v1"):
                flow.request.path = "/v1" + flow.request.path
            
            flow.request.headers["Authorization"] = f"Bearer {API_KEY}"
            flow.request.headers["Host"] = self.target
            flow.request.headers["Origin"] = "https://codex.openai.com"
            flow.request.headers["Referer"] = "https://codex.openai.com/"
            
            print(f"[{timestamp}] ًں”„ {original_path} â†’ https://{self.target}{flow.request.path}")
        
        elif "gapgpt.app" in flow.request.pretty_host:
            print(f"[{timestamp}] ًں“، {flow.request.path}")
    
    def response(self, flow: http.HTTPFlow) -> None:
        timestamp = datetime.now().strftime("%H:%M:%S")
        
        if "gapgpt.app" in flow.request.pretty_host:
            status = flow.response.status_code
            print(f"[{timestamp}] ًں“¨ {status} - {flow.request.path}")

addons = [GapGPTRedirect()]
"@

$redirectContent | Out-File -FilePath $redirectScript -Encoding UTF8
Write-Host "[âœ“] redirect.py created!" -ForegroundColor Green

# Start mitmproxy
Write-Host "[*] Starting mitmproxy..." -ForegroundColor Yellow

$mitmArgs = @(
    "-s", $redirectScript,
    "-p", "8888",
    "--set", "block_global=false",
    "--set", "ssl_insecure=true",
    "--set", "connection_strategy=lazy"
)

Start-Process -FilePath "mitmdump.exe" -ArgumentList $mitmArgs -WindowStyle Normal
Start-Sleep -Seconds 3

# Verify
$mitmProcess = Get-Process -Name "mitmdump" -ErrorAction SilentlyContinue
if (-not $mitmProcess) {
    Write-Host "[!] Failed to start mitmproxy!" -ForegroundColor Red
    exit 1
}

Write-Host "[âœ“] mitmproxy running (PID: $($mitmProcess.Id))" -ForegroundColor Green

# Set Windows Proxy
Write-Host "[*] Configuring Windows proxy..." -ForegroundColor Yellow
Set-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings" -Name "ProxyEnable" -Value 1
Set-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings" -Name "ProxyServer" -Value "127.0.0.1:8888"

# Launch Codex
Write-Host "[*] Launching Codex..." -ForegroundColor Yellow
Start-Process "explorer.exe" -ArgumentList 'shell:AppsFolder\OpenAI.Codex_2p2nqsd0c76g0!App'

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "âœ… Setup Complete!" -ForegroundColor Green
Write-Host "ًںں¢ Proxy: http://localhost:8888" -ForegroundColor Yellow
Write-Host "ًںژ¯ Target: https://api.gapgpt.app/v1" -ForegroundColor Yellow
Write-Host ""
Write-Host "ًں“ٹ Check the mitmdump window for logs" -ForegroundColor Cyan
Write-Host "Press ENTER to stop proxy..." -ForegroundColor Gray
Write-Host "========================================" -ForegroundColor Cyan

Read-Host

# Cleanup
Write-Host "[*] Cleaning up..." -ForegroundColor Yellow
Get-Process -Name "mitmdump" -ErrorAction SilentlyContinue | Stop-Process -Force
Set-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings" -Name "ProxyEnable" -Value 0
Write-Host "[âœ“] Done!" -ForegroundColor Green