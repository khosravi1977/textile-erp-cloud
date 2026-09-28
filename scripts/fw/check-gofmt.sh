#!/usr/bin/env bash
# check-gofmt.sh — ratchet guard for gofmt cleanliness.
#
# Legacy dirt is ledger-capped: the repo carries many pre-existing unformatted
# Go files, so this guard does NOT hard-fail on the baseline. It reads the
# gofmt_dirty.max_files ceiling from quality-baseline.d/ and only fails when
# the count of unformatted files EXCEEDS that ceiling — i.e. when NEW dirty
# files push past the historical baseline. The ceiling may only move DOWN
# (fixing files lowers it via a NEW ledger entry with a cited note).
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"
ledger="$root/quality-baseline.d"

run_checks() { # run_checks <root>
	local max total=0 report="" d out n
	local ledger="$1/quality-baseline.d"
	command -v gofmt > /dev/null 2>&1 || { echo "gofmt: gofmt not found on PATH"; return 2; }
	max=$(fw_resolve "$ledger" gofmt_dirty max_files 2>/dev/null) || {
		echo "gofmt: no gofmt_dirty.max_files in ledger"
		return 2
	}
	while IFS= read -r d; do
		out=$(gofmt -l "$d" 2>/dev/null || true)
		n=$(printf '%s\n' "$out" | grep -c . || true)
		total=$((total + n))
		if [ "$n" -gt 0 ]; then
			report="$report
gofmt: $d: $n dirty"
		fi
	done < <(find "$1" -name go.mod -not -path '*node_modules*' -exec dirname {} \; | sort)
	if [ "$total" -gt "$max" ]; then
		echo "gofmt: $total unformatted Go files > ledger ceiling $max — fix files to move the ceiling down:"
		echo "$report"
		return 1
	fi
	echo "gofmt: $total unformatted files (<= ceiling $max)"
	return 0
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/quality-baseline.d" "$td/moda" "$td/modbad"
	: > "$td/moda/go.mod"
	: > "$td/modbad/go.mod"
	printf '{"gofmt_dirty":{"max_files":1}}\n' > "$td/quality-baseline.d/0001-seed.json"
	printf 'package b\nfunc Bad( ) {\nx:=1\n_ = x\n}\n' > "$td/moda/bad.go"
	fail=0
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 || { echo "self-test: within-ceiling tree refused"; fail=1; }
	printf 'package c\nfunc Worse( ) {\ny:=2\n_ = y\n}\n' > "$td/modbad/worse.go"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 && { echo "self-test: over-ceiling tree passed"; fail=1; }
	fw_selftest_result "gofmt" "$fail"
fi

run_checks "$root"
