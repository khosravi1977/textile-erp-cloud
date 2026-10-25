#!/usr/bin/env bash
# check-anchor-consistency.sh — every bank-opening anchor uses the shared
# balance formula.
#
# The 2026-10-25 dashboard-negative incident: three recalibration sites each
# rolled their own movement-net loop that ignored transfer counter-legs,
# while the financial UI counted them. The fix collapsed all sites onto
# mobileAccountNet. This guard keeps it that way:
#   - mobile_handler.go must call mobileAccountNet
#   - mobile_handler.go must NOT contain the old inline accumulation pattern
#     (`movementNet := int64(0)` loops) — reintroducing one is exactly the
#     drift that produced the negatives
#   - the web accountBalance and mobileAccountNet signatures must both carry
#     the transfer counter-leg marker (grep for the counterAccountId branch
#     on the web side, "transactionType.*transfer" near counter leg on Go)
# Violations past ledger allowlist are regressions. Self-test plants the
# historical bug shape in a synthetic tree.
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"

inline_anchor_re='movementNet := int64\(0\)'
go_counter_re='transactionType"\]\) == "transfer"'

run_checks() { # run_checks <root>
	local fail=0 mh web
	mh="$1/financial/internal/presentation/handler/mobile_handler.go"
	web="$1/financial/web/src/App.jsx"
	for f in "$mh" "$web"; do
		if [ ! -f "$f" ]; then
			echo "anchor-consistency: required file missing: $f"
			return 2
		fi
	done
	if ! grep -q "mobileAccountNet" "$mh"; then
		echo "anchor-consistency: mobile_handler.go no longer calls the shared mobileAccountNet"
		fail=1
	fi
	if grep -nE "$inline_anchor_re" "$mh" | grep -qv "fw:allow-inline-anchor"; then
		echo "anchor-consistency: inline movementNet loop returned in mobile_handler.go (must use mobileAccountNet):"
		grep -nE "$inline_anchor_re" "$mh" | grep -v "fw:allow-inline-anchor"
		fail=1
	fi
	if ! grep -qE "$go_counter_re" "$mh"; then
		echo "anchor-consistency: mobileAccountNet lost the transfer counter-leg branch"
		fail=1
	fi
	if ! grep -q "counterAccountId === account.id" "$web"; then
		echo "anchor-consistency: web accountBalance lost the transfer counter-leg branch"
		fail=1
	fi
	if [ "$fail" -eq 0 ]; then
		echo "anchor-consistency: single shared balance formula (Go anchor sites + web display)"
	fi
	return "$fail"
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/financial/internal/presentation/handler" "$td/financial/web/src"
	printf 'func mobileAccountNet(m []map[string]any, id string) int64 {\n if stringValue(m[0]["transactionType"]) == "transfer" { }\n return 0 }\n' > "$td/financial/internal/presentation/handler/mobile_handler.go"
	printf 'function accountBalance(a, m) { if (m.transactionType === "transfer" && m.counterAccountId === account.id) sum += 1; return 0; }\n' > "$td/financial/web/src/App.jsx"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 || {
		echo "self-test: healthy tree failed"
		fw_selftest_result "anchor-consistency" 1
	}
	{
		echo 'func oldSite() {'
		echo '	movementNet := int64(0)'
		echo '	_ = movementNet'
		echo '}'
	} >> "$td/financial/internal/presentation/handler/mobile_handler.go"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 && {
		echo "self-test: reintroduced inline anchor passed"
		fw_selftest_result "anchor-consistency" 1
	}
	fw_selftest_result "anchor-consistency" 0
fi

run_checks "$root"
