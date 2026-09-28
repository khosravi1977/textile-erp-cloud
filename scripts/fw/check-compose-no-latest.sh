#!/usr/bin/env bash
# check-compose-no-latest.sh — no moving image tags in deployment manifests.
#
# Deploys must be immutable and rollback-able: every image reference in the
# compose/k8s deployment YAMLs must be pinned to an explicit tag or digest —
# ":latest" is a silent upgrade you cannot roll back. Legacy occurrences are
# ratcheted through the ledger (compose_latest.max_occurrences): adding a new
# one is a regression; the ceiling only moves down.
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"
ledger="$root/quality-baseline.d"

run_checks() { # run_checks <root>
	local max total=0 f
	local ledger="$1/quality-baseline.d"
	max=$(fw_resolve "$ledger" compose_latest max_occurrences 2>/dev/null) || {
		echo "compose-no-latest: no compose_latest.max_occurrences in ledger"
		return 2
	}
	local report=""
	while IFS= read -r f; do
		local hits n
		hits=$(grep -nE 'image:[[:space:]]*"?[^[:space:]"]+:latest"?([[:space:]]|$)' "$f" || true)
		n=$(printf '%s\n' "$hits" | grep -c . 2>/dev/null || true)
		[ -n "$hits" ] || n=0
		if [ "$n" -gt 0 ]; then
			total=$((total + n))
			report="$report
compose-no-latest: $f ($n):$hits"
		fi
	done < <(find "$1" \( -name '*.yml' -o -name '*.yaml' \) \
		\( -path '*deploy*' -o -name 'docker-compose*.yml' -o -name 'docker-compose*.yaml' \) \
		-not -path '*node_modules*' 2>/dev/null | sort)
	if [ "$total" -gt "$max" ]; then
		echo "compose-no-latest: $total ':latest' image refs > ledger ceiling $max:"
		echo "$report"
		return 1
	fi
	echo "compose-no-latest: $total ':latest' refs (<= ceiling $max)"
	return 0
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/quality-baseline.d" "$td/deploy/production"
	printf '{"compose_latest":{"max_occurrences":1}}\n' > "$td/quality-baseline.d/0001-seed.json"
	{
		echo "services:"
		echo "  app:"
		echo "    image: ghcr.io/khosravi1977/textile-portal:9f31c2a"
	} > "$td/deploy/production/compose.yml"
	fail=0
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 || fail=1
	echo "    image: postgres:latest" >> "$td/deploy/production/compose.yml"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 || { echo "self-test: 1 :latest ref refused (within ceiling)"; fail=1; }
	echo "    image: redis:latest" >> "$td/deploy/production/compose.yml"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 && { echo "self-test: 2 :latest refs passed ceiling 1"; fail=1; }
	fw_selftest_result "compose-no-latest" "$fail"
fi

run_checks "$root"
