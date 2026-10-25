#!/usr/bin/env bash
# check-no-direct-financial-put.sh — financial state arrays are written only
# by allowlisted files.
#
# MASTER-RESCUE-10 F1: the workspace state becomes a DISPLAY PROJECTION; the
# canonical write path is the typed core. Until that refactor lands, the
# blast radius is contained: files that may write the financial arrays
# (expenses/movements/accounts/mobileTransactions) are ledgered
# (no_direct_financial_put.allow_files with per-file max counts). A NEW file
# writing them, or a known file growing its write sites, is a regression —
# exactly the shape that produced the HesabYar duplicate rows and the
# balance drift. Self-test plants a violation in a synthetic tree.
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"

write_re='state\[.(expenses|movements|accounts|mobileTransactions).\][ ]*='

# fw_allow <ledger-dir> — echoes "<file>|<max>" lines from the latest entry.
fw_allow() {
	python - "$1" << 'PYEOF'
import json, sys, os
d = sys.argv[1]
try:
    winner = ''
    for f in sorted(os.listdir(d)):
        p = os.path.join(d, f)
        if f.endswith('.json'):
            try:
                data = json.load(open(p, encoding='utf-8'))
            except Exception:
                continue
            if 'no_direct_financial_put' in data:
                winner = p
    data = json.load(open(winner, encoding='utf-8')) if winner else {}
    for item in data.get('no_direct_financial_put', {}).get('allow_files', []):
        print(item['file'] + '|' + str(item['max']))
except Exception:
    pass
PYEOF
}

run_checks() { # run_checks <root>
	local ledger="$1/quality-baseline.d" fail=0 f n rel max
	# Allowlist via temp file + awk: portable (no assoc arrays, no process
	# substitution quirks on MSYS).
	local allow_file
	allow_file=$(mktemp)
	trap 'rm -f "$allow_file"' RETURN
	fw_allow "$ledger" | tr -d '' > "$allow_file"
	if [ ! -s "$allow_file" ]; then
		echo "no-direct-financial-put: no allow_files in ledger"
		return 2
	fi
	while IFS= read -r f; do
		rel="${f#"$1"/}"
		n=$(grep -cE "$write_re" "$f" 2>/dev/null || true)
		n=${n:-0}
		[ "$n" -gt 0 ] || continue
		max=$(awk -F'|' -v k="$rel" '$1==k{print $2}' "$allow_file")
		if [ -z "$max" ]; then
			echo "no-direct-financial-put: NEW writer of financial state: $rel ($n sites) — route through the typed core instead"
			fail=1
		elif [ "$n" -gt "$max" ]; then
			echo "no-direct-financial-put: $rel grew to $n write sites > ledger $max"
			fail=1
		fi
	done < <(find "$1/financial/internal" -name '*.go' -not -name '*_test.go' 2>/dev/null | sort)
	if [ "$fail" -eq 0 ]; then
		echo "no-direct-financial-put: financial state writers all within ledger"
	fi
	return "$fail"
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/quality-baseline.d" "$td/financial/internal/presentation/handler"
	printf '{"no_direct_financial_put":{"allow_files":[{"file":"financial/internal/presentation/handler/known.go","max":1}]}}\n' > "$td/quality-baseline.d/0001-seed.json"
	echo 'state["expenses"] = rows' > "$td/financial/internal/presentation/handler/known.go"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 || {
		echo "self-test: allowlisted writer failed"
		fw_selftest_result "no-direct-financial-put" 1
	}
	echo 'state["movements"] = rows' >> "$td/financial/internal/presentation/handler/known.go"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 && {
		echo "self-test: growth past per-file max passed"
		fw_selftest_result "no-direct-financial-put" 1
	}
	echo 'state["accounts"] = rows' > "$td/financial/internal/presentation/handler/newcomer.go"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 && {
		echo "self-test: new writer passed"
		fw_selftest_result "no-direct-financial-put" 1
	}
	fw_selftest_result "no-direct-financial-put" 0
fi

run_checks "$root"
