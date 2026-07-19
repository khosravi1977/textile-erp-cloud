@echo off
chcp 65001 >nul
title ERP Financial Go API - 8081
set "OPERATIONAL_DB_DRIVER=postgres"
if not defined DB_HOST set "DB_HOST=localhost"
if not defined DB_PORT set "DB_PORT=5433"
if not defined DB_USER set "DB_USER=erp_user"
if not defined DB_PASSWORD set "DB_PASSWORD=change_me"
if not defined DB_NAME set "DB_NAME=textile_erp"
if not defined DB_SSLMODE set "DB_SSLMODE=disable"
if not defined JWT_SECRET set "JWT_SECRET=textile-erp-local-secret-change-in-production"
set "APP_PORT=8081"
cd /d "%~dp0financial"

echo Starting financial API on http://127.0.0.1:8081 ...
echo Financial DB: PostgreSQL %DB_HOST%:%DB_PORT%/%DB_NAME%
echo Operational bridge: PostgreSQL %DB_HOST%:%DB_PORT%/%DB_NAME%
call go run ./cmd/api
if errorlevel 1 (
  echo.
  echo Financial API stopped with an error.
  pause
  exit /b 1
)
