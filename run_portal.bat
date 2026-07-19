@echo off
chcp 65001 >nul
title ERP Portal - 8080
cd /d "%~dp0portal_server"

echo Starting ERP portal on http://127.0.0.1:8080 ...
call go run . -addr :8080 -financial http://127.0.0.1:5173 -operational http://127.0.0.1:8091 -financial-api http://127.0.0.1:8081 -operational-api http://127.0.0.1:8091
if errorlevel 1 (
  echo.
  echo ERP portal stopped with an error.
  pause
  exit /b 1
)
