#!/usr/bin/env bash
# check-mojibake.sh — no double-encoded Persian text in source.
#
# Scans user-facing source (Go, JS/JSX, Kotlin-style literals that live in
# this repo, web/src) for classic mojibake byte-mangling sequences
# («ط§ظ», «ع†ع©», «ÛŒ», «Â») that have bitten this codebase before
# (standardTransactionType shipped with mojibake group names for months).
# New occurrences past the ledger ceiling (mojibake.max_occurrences) are a
# regression. A line can self-declare "fw:allow-mojibake" (e.g. fixtures
# that MUST contain the broken bytes).
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"

mojibake_re='(ط§ظ|ع†ع©|ط¯ط§|ÛŒÙ‡|Â§|Ø¨Ø±)'

run_checks() { # run_checks <root>
	local max total=0
	local ledger="$1/quality-baseline.d"
	max=$(fw_resolve "$ledger" mojibake max_occurrences 2>/dev/null) || {
		echo "mojibake: no mojibake.max_occurrences in ledger"
		return 2
	}
	local report="" f n
	while IFS= read -r f; do
		n=$(grep -nE "$mojibake_re" "$f" 2>/dev/null | grep -v "fw:allow-mojibake" | wc -l || true)
		n=${n:-0}
		[ "$n" -gt 0 ] || continue
		total=$((total + n))
		report="$report
mojibake: $f: $n"
	done < <(find "$1" \( -name '*.go' -o -name '*.jsx' -o -name '*.js' -o -name '*.kt' -o -name '*.html' \) \
		-not -path '*node_modules*' -not -path '*/dist/*' -not -path '*/.git/*' 2>/dev/null | sort)
	if [ "$total" -gt "$max" ]; then
		echo "mojibake: $total double-encoded occurrences > ledger ceiling $max — fix the encoding, do not band-aid:$report"
		return 1
	fi
	echo "mojibake: $total occurrences (<= ceiling $max)"
	return 0
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/quality-baseline.d" "$td/web/src"
	printf '{"mojibake":{"max_occurrences":0}}\n' > "$td/quality-baseline.d/0001-seed.json"
	{
		echo 'if (group.contains("ط§ظ†طھظ‚ط§ظ„")) return "X"; // fw:allow-mojibake — legacy fixture, F3 will remove'
	} > "$td/web/src/legacy.jsx"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 || {
		echo "self-test: allowed occurrence wrongly failed"
		fw_selftest_result "mojibake" 1
	}
	echo 'if (subgroup.contains("ع†ع©")) return "Y";' >> "$td/web/src/legacy.jsx"
	FW_ROOT="$td" run_checks "$td" >/dev/null 2>&1 && {
		echo "self-test: new mojibake passed"
		fw_selftest_result "mojibake" 1
	}
	fw_selftest_result "mojibake" 0
fi

run_checks "$root"
