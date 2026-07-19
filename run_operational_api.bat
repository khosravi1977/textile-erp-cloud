@echo off
chcp 65001 >nul
title ERP Operational Go - 8091
set "OPERATIONAL_DB_DRIVER=postgres"
if not defined DB_HOST set "DB_HOST=localhost"
if not defined DB_PORT set "DB_PORT=5433"
if not defined DB_USER set "DB_USER=erp_user"
if not defined DB_PASSWORD set "DB_PASSWORD=change_me"
if not defined DB_NAME set "DB_NAME=textile_erp"
if not defined DB_SSLMODE set "DB_SSLMODE=disable"
set "PORT=8091"
cd /d "%~dp0operational_cycle_go"

echo Starting operational Go server on http://127.0.0.1:8091 ...
echo Operational DB: PostgreSQL %DB_HOST%:%DB_PORT%/%DB_NAME%
call go run ./cmd/server
if errorlevel 1 (
  echo.
  echo Operational Go server stopped with an error.
  pause
  exit /b 1
)
