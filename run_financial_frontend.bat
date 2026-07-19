@echo off
chcp 65001 >nul
title ERP Financial React - 5173
cd /d "%~dp0financial\web"

if not exist node_modules (
  echo Installing financial frontend dependencies...
  call npm install
  if errorlevel 1 (
    echo.
    echo Financial frontend dependencies could not be installed.
    pause
    exit /b 1
  )
)

echo Starting financial frontend on http://127.0.0.1:5173 ...
call npm run dev -- --host 127.0.0.1 --port 5173
if errorlevel 1 (
  echo.
  echo Financial frontend stopped with an error.
  pause
  exit /b 1
)
