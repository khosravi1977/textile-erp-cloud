@echo off
chcp 65001 >nul
title Textile ERP Launcher
setlocal EnableExtensions

set "ROOT=%~dp0"
set "FINANCIAL_DIR=%ROOT%financial"
set "LOCAL_COMPOSE_FILE=%FINANCIAL_DIR%\docker-compose.yml"
set "PROD_COMPOSE_FILE=%FINANCIAL_DIR%\docker-compose.prod.yml"
set "COMPOSE_FILE=%LOCAL_COMPOSE_FILE%"
if not exist "%COMPOSE_FILE%" set "COMPOSE_FILE=%PROD_COMPOSE_FILE%"

if not defined DB_HOST set "DB_HOST=localhost"
if not defined DB_PORT set "DB_PORT=5433"
if not defined DB_USER set "DB_USER=erp_user"
if not defined DB_PASSWORD set "DB_PASSWORD=change_me"
if not defined DB_NAME set "DB_NAME=textile_erp"
if not defined DB_SSLMODE set "DB_SSLMODE=disable"
if not defined JWT_SECRET set "JWT_SECRET=textile-erp-local-secret-change-in-production"
if not defined FINANCIAL_ADMIN_PASSWORD set "FINANCIAL_ADMIN_PASSWORD=admin123"
if not defined APP_ENV set "APP_ENV=production"

set "APP_PORT=8081"
set "PORT=8091"

if /I "%~1"=="prod" goto production

echo Starting Textile ERP stack...
call :ensure_docker_ready
if errorlevel 1 exit /b 1
call :stop_legacy_containers

pushd "%FINANCIAL_DIR%"
echo Bringing up infrastructure...
call :ensure_docker_service "textile-erp-db" "postgres"
if errorlevel 1 (
  popd
  echo Failed to start Docker infrastructure.
  pause
  exit /b 1
)
call :ensure_docker_service "textile-erp-redis" "redis"
if errorlevel 1 (
  popd
  echo Failed to start Docker infrastructure.
  pause
  exit /b 1
)
call :ensure_docker_service "textile-erp-db-replica" "postgres-replica" optional
if errorlevel 1 (
  popd
  echo Failed to start Docker infrastructure.
  pause
  exit /b 1
)
call :ensure_docker_service "textile-erp-prometheus" "prometheus" optional
if errorlevel 1 (
  popd
  echo Failed to start Prometheus.
  pause
  exit /b 1
)
call :ensure_docker_service "textile-erp-grafana" "grafana" optional
if errorlevel 1 (
  popd
  echo Failed to start Grafana.
  pause
  exit /b 1
)
popd

echo Waiting for PostgreSQL...
powershell -NoProfile -ExecutionPolicy Bypass -Command "$deadline=(Get-Date).AddSeconds(90); do { $ready=Test-NetConnection -ComputerName '%DB_HOST%' -Port %DB_PORT% -InformationLevel Quiet; if(-not $ready){ Start-Sleep -Seconds 2 } } until($ready -or (Get-Date) -gt $deadline); if($ready){ exit 0 } else { exit 1 }"
if errorlevel 1 (
  echo PostgreSQL was not reachable.
  pause
  exit /b 1
)

call :ensure_port 8091 "Operational Go" "%ROOT%run_operational_api.bat"

pushd "%FINANCIAL_DIR%"
echo Running financial migrations...
call go run ./cmd/migrate
if errorlevel 1 (
  popd
  echo Financial migrations failed.
  pause
  exit /b 1
)
popd

call :ensure_port 8081 "Financial Go API" "%ROOT%run_financial_api.bat"
call :ensure_port 5173 "Financial React" "%ROOT%run_financial_frontend.bat"
call :ensure_port 8080 "ERP Portal" "%ROOT%run_portal.bat"
call :reset_grafana_password

echo Waiting for services...
powershell -NoProfile -ExecutionPolicy Bypass -Command "$ports=@(8080,5173,8081,8091); $deadline=(Get-Date).AddSeconds(60); do { $ready=$true; foreach($p in $ports){ if(-not (Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue)){ $ready=$false } }; if(-not $ready){ Start-Sleep -Milliseconds 700 } } until($ready -or (Get-Date) -gt $deadline)"

echo.
call :open_menu
exit /b 0

:production
echo Starting Textile ERP production Docker stack...
set "COMPOSE_FILE=%PROD_COMPOSE_FILE%"
if not exist "%COMPOSE_FILE%" set "COMPOSE_FILE=%LOCAL_COMPOSE_FILE%"
call :ensure_docker_ready
if errorlevel 1 exit /b 1
call :stop_legacy_containers
pushd "%FINANCIAL_DIR%"
call :ensure_docker_service "textile-erp-db" "postgres"
if errorlevel 1 (
  popd
  echo Production database failed to start.
  pause
  exit /b 1
)
call :ensure_docker_service "textile-erp-redis" "redis"
if errorlevel 1 (
  popd
  echo Production Redis failed to start.
  pause
  exit /b 1
)
call :ensure_docker_service "textile-erp-db-replica" "postgres-replica" optional
if errorlevel 1 (
  popd
  echo Production read replica failed to start.
  pause
  exit /b 1
)
docker compose -f "%COMPOSE_FILE%" up -d --build --no-deps api nginx prometheus grafana
if errorlevel 1 (
  popd
  echo Production stack failed to start.
  pause
  exit /b 1
)
call :reset_grafana_password
popd
echo.
call :open_menu
exit /b 0

:open_menu
cls
set "HAS_GRAFANA="
set "HAS_PROMETHEUS="
powershell -NoProfile -ExecutionPolicy Bypass -Command "if (Get-NetTCPConnection -LocalPort 3000 -State Listen -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }"
if not errorlevel 1 set "HAS_GRAFANA=1"
powershell -NoProfile -ExecutionPolicy Bypass -Command "if (Get-NetTCPConnection -LocalPort 9090 -State Listen -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }"
if not errorlevel 1 set "HAS_PROMETHEUS=1"
echo Textile ERP is ready.
echo.
echo Select what you want to open:
echo.
echo   1. Portal        - http://127.0.0.1:8080
echo   2. Financial API - http://127.0.0.1:8081/health
echo   3. Metrics       - http://127.0.0.1:8081/metrics
if defined HAS_GRAFANA echo   4. Grafana       - http://127.0.0.1:3000
if defined HAS_PROMETHEUS echo   5. Prometheus    - http://127.0.0.1:9090
echo   0. Exit
echo.
set /p "MENU_CHOICE=Enter choice: "
if not defined MENU_CHOICE exit /b 0
if "%MENU_CHOICE%"=="1" start "" "http://127.0.0.1:8080" & goto open_menu
if "%MENU_CHOICE%"=="2" start "" "http://127.0.0.1:8081/health" & goto open_menu
if "%MENU_CHOICE%"=="3" start "" "http://127.0.0.1:8081/metrics" & goto open_menu
if "%MENU_CHOICE%"=="4" if defined HAS_GRAFANA start "" "http://127.0.0.1:3000" & goto open_menu
if "%MENU_CHOICE%"=="5" if defined HAS_PROMETHEUS start "" "http://127.0.0.1:9090" & goto open_menu
if "%MENU_CHOICE%"=="0" exit /b 0
echo Invalid choice.
pause
goto open_menu

:ensure_docker_ready
where docker >nul 2>nul
if errorlevel 1 (
  echo Docker is not installed or not on PATH.
  pause
  exit /b 1
)
docker version >nul 2>nul
if not errorlevel 1 exit /b 0
echo Docker daemon is not running. Trying to start Docker Desktop...
call :start_docker_desktop
powershell -NoProfile -ExecutionPolicy Bypass -Command "$deadline=(Get-Date).AddMinutes(2); do { docker version >$null 2>$null; if($LASTEXITCODE -eq 0){ exit 0 }; Start-Sleep -Seconds 3 } until((Get-Date) -gt $deadline); exit 1"
if errorlevel 1 (
  echo Docker Desktop did not become ready. Start Docker Desktop manually, wait until it says 'Engine running', then run this file again.
  pause
  exit /b 1
)
exit /b 0

:start_docker_desktop
for %%P in (
  "%ProgramFiles%\Docker\Docker\Docker Desktop.exe"
  "%ProgramFiles(x86)%\Docker\Docker\Docker Desktop.exe"
  "%LocalAppData%\Docker\Docker\Docker Desktop.exe"
) do (
  if exist %%~P (
    start "" "%%~P"
    exit /b 0
  )
)
exit /b 0

:stop_legacy_containers
for %%C in (textile-backend textile-frontend textile-nginx textile-postgres textile-redis financial_postgres) do (
  docker container inspect "%%C" >nul 2>nul
  if not errorlevel 1 (
    echo Stopping old conflicting container %%C...
    docker stop "%%C" >nul 2>nul
  )
)
exit /b 0

:ensure_docker_service
set "CONTAINER_NAME=%~1"
set "COMPOSE_SERVICE=%~2"
set "SERVICE_MODE=%~3"
call :compose_service_exists "%COMPOSE_SERVICE%"
if errorlevel 1 (
  if /I "%SERVICE_MODE%"=="optional" (
    echo Skipping optional Docker service %COMPOSE_SERVICE% because it is not defined in "%COMPOSE_FILE%".
    exit /b 0
  )
  echo Docker service %COMPOSE_SERVICE% is not defined in "%COMPOSE_FILE%".
  exit /b 1
)
echo Applying Docker service %COMPOSE_SERVICE% from "%COMPOSE_FILE%"...
docker container inspect "%CONTAINER_NAME%" >nul 2>nul
if not errorlevel 1 (
  echo Removing conflicting container %CONTAINER_NAME% so Docker Compose can recreate it...
  docker rm -f "%CONTAINER_NAME%" >nul
  if errorlevel 1 exit /b %ERRORLEVEL%
)
docker compose -f "%COMPOSE_FILE%" up -d --no-deps --force-recreate "%COMPOSE_SERVICE%"
exit /b %ERRORLEVEL%

:compose_service_exists
powershell -NoProfile -ExecutionPolicy Bypass -Command "$services = docker compose -f '%COMPOSE_FILE%' config --services 2>$null; if($services -contains '%~1'){ exit 0 } else { exit 1 }"
exit /b %ERRORLEVEL%

:reset_grafana_password
docker container inspect "textile-erp-grafana" >nul 2>nul
if errorlevel 1 exit /b 0
docker start "textile-erp-grafana" >nul 2>nul
docker exec "textile-erp-grafana" grafana cli admin reset-admin-password admin123 >nul 2>nul
exit /b 0

:ensure_port
set "PORT=%~1"
set "TITLE=%~2"
set "SCRIPT=%~3"
if "%PORT%"=="8080" (
  powershell -NoProfile -ExecutionPolicy Bypass -Command "try { $r=Invoke-WebRequest -Uri 'http://127.0.0.1:8080/health' -UseBasicParsing -TimeoutSec 2; if($r.Content -like '*financialApi*'){ exit 0 } else { exit 1 } } catch { exit 1 }"
  if errorlevel 1 (
    echo Starting %TITLE% on port %PORT%...
    start "%TITLE%" "%SCRIPT%"
  ) else (
    echo %TITLE% already healthy on port %PORT%; skipping.
  )
  exit /b 0
)
if "%PORT%"=="8081" (
  powershell -NoProfile -ExecutionPolicy Bypass -Command "try { $r=Invoke-WebRequest -Uri 'http://127.0.0.1:8081/health' -UseBasicParsing -TimeoutSec 2; if($r.Content -like '*\"status\":\"ok\"*'){ exit 0 } else { exit 1 } } catch { $c=Get-NetTCPConnection -LocalPort 8081 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1; if($c){ Stop-Process -Id $c.OwningProcess -Force -ErrorAction SilentlyContinue }; exit 1 }"
  if errorlevel 1 (
    echo Starting %TITLE% on port %PORT%...
    start "%TITLE%" "%SCRIPT%"
  ) else (
    echo %TITLE% already healthy on port %PORT%; skipping.
  )
  exit /b 0
)
powershell -NoProfile -ExecutionPolicy Bypass -Command "if (Get-NetTCPConnection -LocalPort %PORT% -State Listen -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }"
if errorlevel 1 (
  echo Starting %TITLE% on port %PORT%...
  start "%TITLE%" "%SCRIPT%"
) else (
  echo %TITLE% already running on port %PORT%; skipping.
)
exit /b 0
