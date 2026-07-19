Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
  npm run build
} finally {
  Pop-Location
}