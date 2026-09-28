#!/usr/bin/env bash
# lib.sh — shared helpers for the fw guards (modeled on hamneshin's
# scripts/ci/lib/baseline.sh, adapted for textile-erp-cloud).
#
# Ledger rules (docs/FIREWALL.md):
#   - quality-baseline.d/ is append-only: never edit/delete an entry.
#   - entries are one-line JSON files named 0001-*.json or
#     YYYYMMDD-HHMMSSZ-<slug>.json (sorted = chronological).
#   - per section the LATEST entry carrying <section> wins WHOLESALE.
#   - "could not resolve" must become exit 2 (failure, never skip).

# fw_root — the repository root (FW_ROOT override for self-tests).
fw_root() {
	if [ -n "${FW_ROOT:-}" ]; then printf '%s\n' "$FW_ROOT"; return 0; fi
	local dir
	dir="$(cd "$(dirname "${BASH_SOURCE[1]}")/../.." && pwd)"
	printf '%s\n' "$dir"
}

# fw_ledger_files <dir> — existing *.json entries, sorted (sorted = chronological).
fw_ledger_files() {
	local dir="$1"
	[ -d "$dir" ] || return 0
	ls "$dir"/*.json 2>/dev/null | LC_ALL=C sort || true
}

# fw_latest <dir> <section> — filename of the latest entry carrying <section>.
fw_latest() {
	local dir="$1" section="$2" f winner=""
	while IFS= read -r f; do
		[ -n "$f" ] || continue
		if grep -q "\"$section\"" "$f"; then winner="$f"; fi
	done < <(fw_ledger_files "$dir")
	[ -n "$winner" ] || return 1
	printf '%s\n' "$winner"
}

# fw_resolve <dir> <section> <key> — numeric value; latest entry wins.
fw_resolve() {
	local dir="$1" section="$2" key="$3" f winner="" v
	while IFS= read -r f; do
		[ -n "$f" ] || continue
		if grep -q "\"$section\"" "$f"; then winner="$f"; fi
	done < <(fw_ledger_files "$dir")
	[ -n "$winner" ] || return 1
	v=$(grep -oE "\"$section\"[[:space:]]*:[[:space:]]*\{[^}]*\}" "$winner" |
		grep -oE "\"$key\"[[:space:]]*:[[:space:]]*[0-9]+" |
		grep -oE '[0-9]+$' | tail -1)
	[ -n "$v" ] || return 1
	printf '%s\n' "$v"
}

# fw_selftest_result <name> <fail(0/1)> — uniform self-test footer.
fw_selftest_result() {
	if [ "$2" -eq 0 ]; then
		echo "$1 self-test: PASS"
		exit 0
	fi
	echo "$1 self-test: FAIL"
	exit 1
}
