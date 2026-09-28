#!/usr/bin/env bash
# check-test-floor.sh — anti-vacuity floor for the Go test tree.
#
# Every Go module found in the repo must have a ledgered test floor, and must
# keep at least that many packages carrying real tests. "Someone deleted the
# tests", "the package moved and its tests vanished", or "a new module
# appeared without a floor" all turn CI red — the suite may not silently
# shrink. (The enforcing runtime gate remains CI's `go test ./...` per
# module; this guard protects the STRUCTURE of the test tree.)
#
# Floor lives in quality-baseline.d/ (section go_floors, key per module dir).
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"

# count_test_pkgs <module-dir> — number of distinct dirs holding tests with real cases.
count_test_pkgs() {
	find "$1" -name "*_test.go" -not -path '*node_modules*' 2>/dev/null |
		xargs grep -l "func Test" 2>/dev/null |
		xargs -n1 dirname 2>/dev/null | LC_ALL=C sort -u | grep -c . || true
}

run_checks() { # run_checks <root>
	local rc=0 mod floor have
	local ledger="$1/quality-baseline.d"
	local mods rel
	mods=$(find "$1" -name go.mod -not -path '*node_modules*' -exec dirname {} \; 2>/dev/null |
		sed "s|^$1/||" | LC_ALL=C sort)
	[ -n "$mods" ] || { echo "test-floor: no Go modules found under $1"; return 2; }
	while IFS= read -r mod; do
		[ -n "$mod" ] || continue
		floor=$(fw_resolve "$ledger" go_floors "$mod" 2>/dev/null) || {
			echo "test-floor: module $mod has no go_floors floor in the ledger — add a NEW ledger entry measuring it"
			rc=2
			continue
		}
		have=$(count_test_pkgs "$1/$mod")
		if [ "$have" -lt "$floor" ]; then
			echo "test-floor: $mod has $have test packages < floor $floor — tests must not silently shrink"
			rc=1
		else
			echo "test-floor: $mod ok ($have >= $floor)"
		fi
	done < <(printf '%s\n' "$mods")
	return "$rc"
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/quality-baseline.d" "$td/moda/pkg1" "$td/moda/pkg2" "$td/modb"
	: > "$td/moda/go.mod"
	: > "$td/modb/go.mod"
	printf '{"go_floors":{"moda":2,"modb":1}}\n' > "$td/quality-baseline.d/0001-seed.json"
	printf 'package t\nimport "testing"\nfunc TestA(t *testing.T) {}\n' > "$td/moda/pkg1/a_test.go"
	printf 'package t\nimport "testing"\nfunc TestB(t *testing.T) {}\n' > "$td/moda/pkg2/b_test.go"
	printf 'package t\nimport "testing"\nfunc TestC(t *testing.T) {}\n' > "$td/modb/c_test.go"
	fail=0
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 || fail=1
	# Shrink moda below its floor:
	rm "$td/moda/pkg2/b_test.go"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 && { echo "self-test: shrunken suite passed"; fail=1; }
	# A module without a ledgered floor = could-not-measure (2):
	printf '{"go_floors":{"moda":2}}\n' > "$td/quality-baseline.d/0002-nofloor.json"
	r=0
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 || r=$?
	[ "$r" -eq 2 ] || { echo "self-test: missing floor must exit 2, got $r"; fail=1; }
	# Ledger resolution: latest entry wins.
	v=$(fw_resolve "$td/quality-baseline.d" go_floors moda)
	[ "$v" = "2" ] || { echo "self-test: fw_resolve got '$v' want 2"; fail=1; }
	fw_selftest_result "test-floor" "$fail"
fi

run_checks "$root"
