#!/usr/bin/env bash
# check-file-size.sh — monolith ratchet: source files may not grow.
#
# Ledger section file_size holds per-pattern ceilings in LINES:
#   {"file_size":{"ceilings":{"financial/web/src/App.jsx":6704,...},"default_max":800}}
# Rationale: F3 of MASTER-RESCUE-10 splits the monoliths; until the split
# lands, ANY growth of the big five files is a regression (the number only
# ratchets down). Files matching no ceiling may not exceed default_max.
# "fw:allow-size" never excuses a file — size is structural; to raise a
# ceiling write a NEW ledger entry with a cited note (review-grade).
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"

# fw_size_ceiling <ledger-dir> <relpath> — echoes ceiling or empty.
fw_size_ceiling() {
	local ledger="$1" rel="$2"
	jsonq() { python - "$1" "$2" << 'PYEOF'
import json, sys
try:
    data = json.load(open(sys.argv[1], encoding='utf-8'))
    print(data.get('file_size', {}).get('ceilings', {}).get(sys.argv[2], ''))
except Exception:
    print('')
PYEOF
	}
	local f
	f=$(fw_latest "$ledger" file_size 2>/dev/null) || return 0
	# fw_latest already returns the full path to the winning entry.
	# Windows python writes CRLF; strip CR so [ -gt ] stays numeric.
	jsonq "$f" "$rel" | tr -d ''
}

run_checks() { # run_checks <root>
	local ledger="$1/quality-baseline.d" f lines rel ceiling default_max
	default_max=$(fw_resolve "$ledger" file_size default_max 2>/dev/null) || {
		echo "file-size: no file_size.default_max in ledger"
		return 2
	}
	local fail=0
	while IFS= read -r f; do
		rel="${f#"$1"/}"
		lines=$(wc -l < "$f")
		ceiling=$(fw_size_ceiling "$ledger" "$rel")
		if [ -z "$ceiling" ]; then
			ceiling="$default_max"
		fi
		if [ "$lines" -gt "$ceiling" ]; then
			echo "file-size: $rel = $lines lines > ceiling $ceiling — split it or lower the ledger with a cited note"
			fail=1
		fi
	done < <(find "$1" \( -name '*.go' -o -name '*.jsx' -o -name '*.js' -o -name '*.kt' \) \
		-not -path '*node_modules*' -not -path '*/dist/*' -not -path '*/.git/*' -not -name '*_test.go' \
		-not -path '*test*' 2>/dev/null | sort)
	if [ "$fail" -eq 0 ]; then
		echo "file-size: all source files within ledger ceilings (default $default_max)"
	fi
	return "$fail"
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/quality-baseline.d" "$td/app"
	printf '{"file_size":{"default_max":800,"ceilings":{"app/big.kt":801}}}\n' > "$td/quality-baseline.d/0001-seed.json"
	{
		for i in $(seq 1 801); do echo "line $i"; done
	} > "$td/app/big.kt"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 || {
		echo "self-test: ledgered ceiling not honored"
		fw_selftest_result "file-size" 1
	}
	{
		for i in $(seq 1 802); do echo "line $i"; done
	} > "$td/app/big.kt"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 && {
		echo "self-test: growth past ceiling passed"
		fw_selftest_result "file-size" 1
	}
	echo 'x' > "$td/app/huge.js"
	for i in $(seq 1 801); do echo "line $i" >> "$td/app/huge.js"; done
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 && {
		echo "self-test: unledgered oversized file passed"
		fw_selftest_result "file-size" 1
	}
	fw_selftest_result "file-size" 0
fi

run_checks "$root"
