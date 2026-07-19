#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "This script must be run as root." >&2
  exit 1
fi

APP_DIR="${APP_DIR:-/opt/textile-erp}"
DOMAIN="${APP_DOMAIN:-${1:-}}"

if [ -z "$DOMAIN" ]; then
  echo "Usage: APP_DOMAIN=example.com bash deploy/vps-nginx.sh" >&2
  exit 1
fi

SRC="$APP_DIR/deploy/nginx.textile-erp.conf"
DST="/etc/nginx/sites-available/textile-erp.conf"

if [ ! -f "$SRC" ]; then
  echo "Nginx template not found: $SRC" >&2
  exit 1
fi

sed "s/example.com/${DOMAIN}/g" "$SRC" >"$DST"
ln -sfn "$DST" /etc/nginx/sites-enabled/textile-erp.conf

nginx -t
systemctl reload nginx

echo "Nginx configured for $DOMAIN"
