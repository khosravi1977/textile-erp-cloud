#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT

export TEXTILE_FINANCIAL_API_IMAGE=example.invalid/textile-financial-api:test
export TEXTILE_FINANCIAL_WEB_IMAGE=example.invalid/textile-financial-web:test
export TEXTILE_OPERATIONAL_IMAGE=example.invalid/textile-operational:test
export TEXTILE_PORTAL_IMAGE=example.invalid/textile-portal:test

docker compose \
  --profile monitoring \
  --project-directory "$root" \
  --env-file "$root/deploy/.env.vps.example" \
  --file "$root/deploy/docker-compose.vps.yml" \
  --file "$root/deploy/docker-compose.vps.public.yml" \
  --file "$root/deploy/production/compose.images.yaml" \
  config > "$rendered"

for path in \
  "$root/financial/deploy/prometheus/prometheus.yml" \
  "$root/financial/deploy/grafana/provisioning" \
  "$root/financial/deploy/grafana/dashboards"; do
  test -e "$path"
  grep -Fq "source: $path" "$rendered"
done

echo "production compose paths verified"
