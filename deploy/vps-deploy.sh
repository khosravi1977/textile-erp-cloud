#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/textile-erp}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/deploy/docker-compose.vps.yml}"
PUBLIC_COMPOSE_FILE="${PUBLIC_COMPOSE_FILE:-$APP_DIR/deploy/docker-compose.vps.public.yml}"
DOMAIN="${1:-${APP_DOMAIN:-}}"

if [ ! -d "$APP_DIR" ]; then
  echo "App directory not found: $APP_DIR" >&2
  exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "Environment file not found: $ENV_FILE" >&2
  echo "Copy deploy/.env.vps.example to .env and set production secrets first." >&2
  exit 1
fi

cd "$APP_DIR/deploy"

COMPOSE_ARGS=(-f "$COMPOSE_FILE")
if [ -f "$PUBLIC_COMPOSE_FILE" ]; then
  COMPOSE_ARGS+=(-f "$PUBLIC_COMPOSE_FILE")
fi

docker compose "${COMPOSE_ARGS[@]}" --env-file "$ENV_FILE" up -d --build

if [ -n "$DOMAIN" ]; then
  APP_DOMAIN="$DOMAIN" APP_DIR="$APP_DIR" bash "$APP_DIR/deploy/vps-nginx.sh"
fi

echo "Deployment complete."
echo "Portal health:"
curl -fsS http://127.0.0.1:8180/health || true
