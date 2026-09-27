#!/usr/bin/env bash
# check-golden-contracts.sh — the "tabs vanished" guard.
#
# Verifies three frozen structural contracts of the ERP against golden files
# under contracts/golden/:
#   1. portal-routes.txt      — every mux route registered in portal_server/main.go
#   2. portal-menus.txt       — every menu_key in the portal menu tables
#   3. financial-web-tabs.txt — every sidebar tab id in financial/web/src/App.jsx
#
# A route/menu/tab disappearing (the "sidebar tabs vanished" regression class)
# turns CI red. A conscious change regenerates goldens via `bash fw.sh golden-update`
# and reviews the diff — the golden diff IS the review surface.
# Extraction strips \r so CRLF checkouts on Windows never fake a drift.
#
# Exit codes: 0 ok · 1 drift · 2 could-not-measure (golden file missing).
# Self-test: --self-test (plants drift in a synthetic tree and asserts detection).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"

# extract_routes <main.go-path> — sorted mux route paths.
extract_routes() {
	grep -oE 'mux\.(HandleFunc|Handle)\("[^"]+"' "$1" |
		sed -E 's/^mux\.(HandleFunc|Handle)\("//; s/"$//' | tr -d '\r' | LC_ALL=C sort
}

# extract_menu_keys <main.go-path> — sorted menu_key values.
extract_menu_keys() {
	grep -oE '"menu_key":[[:space:]]*"[^"]+"' "$1" |
		sed -E 's/"menu_key":[[:space:]]*"//; s/"$//' | tr -d '\r' | LC_ALL=C sort
}

# extract_tab_ids <App.jsx-path> — sorted sidebar tab ids (single or double quotes).
extract_tab_ids() {
	grep -oE "\{[[:space:]]*id:[[:space:]]*['\"][^'\"]+['\"]" "$1" |
		sed -E "s/\{[[:space:]]*id:[[:space:]]*['\"]//; s/['\"]\$//" | tr -d '\r' | LC_ALL=C sort
}

# check_pair <label> <extracted-file> <golden-file> — 0 ok · 1 drift · 2 missing.
check_pair() {
	local label="$1" got="$2" golden="$3"
	if [ ! -f "$golden" ]; then
		echo "golden-contracts: missing $label golden: $golden"
		return 2
	fi
	if diff -u "$golden" "$got" > /tmp/fw_golden_diff.$$ 2>&1; then
		rm -f /tmp/fw_golden_diff.$$
		echo "golden-contracts: $label ok"
		return 0
	fi
	echo "golden-contracts: $label DRIFTED from its golden contract:"
	cat /tmp/fw_golden_diff.$$
	rm -f /tmp/fw_golden_diff.$$
	echo "golden-contracts: if intentional, run: bash fw.sh golden-update (and review the diff)"
	return 1
}

run_checks() {
	local rc=0 r
	local main_go="$1/portal_server/main.go" app_jsx="$1/financial/web/src/App.jsx"
	local g="$1/contracts/golden"
	[ -f "$main_go" ] || { echo "golden-contracts: missing $main_go"; return 2; }
	[ -f "$app_jsx" ] || { echo "golden-contracts: missing $app_jsx"; return 2; }

	tmp="$(mktemp -d)" # global-scoped; removed inline below (never trap inside a function)
	extract_routes "$main_go" > "$tmp/routes"
	extract_menu_keys "$main_go" > "$tmp/menus"
	extract_tab_ids "$app_jsx" > "$tmp/tabs"

	# severity: could-not-measure (2) dominates drift (1)
	r=0; check_pair "portal routes" "$tmp/routes" "$g/portal-routes.txt" || r=$?
	[ "$r" -gt "$rc" ] && rc=$r
	r=0; check_pair "portal menu keys" "$tmp/menus" "$g/portal-menus.txt" || r=$?
	[ "$r" -gt "$rc" ] && rc=$r
	r=0; check_pair "financial web tabs" "$tmp/tabs" "$g/financial-web-tabs.txt" || r=$?
	[ "$r" -gt "$rc" ] && rc=$r

	rm -rf "$tmp"
	return "$rc"
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/portal_server" "$td/financial/web/src" "$td/contracts/golden"
	{
		echo 'mux.HandleFunc("/login", app.customerLogin)'
		echo 'mux.HandleFunc("/admin", app.adminPanel)'
		echo '"menu_key": "dashboard"'
		echo '"menu_key": "reports"'
	} > "$td/portal_server/main.go"
	printf "const tabs = [\n  { id: 'dashboard', label: 'x' },\n  { id: 'reports', label: 'y' },\n];\n" > "$td/financial/web/src/App.jsx"
	extract_routes "$td/portal_server/main.go" > "$td/contracts/golden/portal-routes.txt"
	extract_menu_keys "$td/portal_server/main.go" > "$td/contracts/golden/portal-menus.txt"
	extract_tab_ids "$td/financial/web/src/App.jsx" > "$td/contracts/golden/financial-web-tabs.txt"
	fail=0
	FW_ROOT="$td" run_checks "$td" || fail=1
	# Drift: shrink the route golden to one line -> guard must catch.
	cp "$td/contracts/golden/portal-routes.txt" "$td/contracts/golden/portal-routes.bak"
	head -1 "$td/contracts/golden/portal-routes.txt" > "$td/contracts/golden/portal-routes.txt"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 && { echo "self-test: drifted route passed"; fail=1; }
	cp "$td/contracts/golden/portal-routes.bak" "$td/contracts/golden/portal-routes.txt"
	# Missing golden → exit 2.
	rm "$td/contracts/golden/financial-web-tabs.txt"
	r=0
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 || r=$?
	[ "$r" -eq 2 ] || { echo "self-test: missing golden must exit 2, got $r"; fail=1; }
	fw_selftest_result "golden-contracts" "$fail"
fi

run_checks "$root"
