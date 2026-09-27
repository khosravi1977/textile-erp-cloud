#!/usr/bin/env bash
# fw.sh — the code-firewall runner (no make on the dev boxes; bash is enough).
#
#   bash fw.sh self-tests      every guard's --self-test (falsification proof)
#   bash fw.sh guard           every guard, exactly as CI enforces it
#   bash fw.sh verify          self-tests + guard (run before every push)
#   bash fw.sh verify-full     verify + full Go test suite of the 3 modules
#   bash fw.sh golden-update   consciously regenerate the golden contracts
#   bash fw.sh commit-lint <range>   lint commit subjects in a git range
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure (never a skip).
set -uo pipefail
cd "$(dirname "$0")"

GUARDS="check-golden-contracts check-migration-safety check-secret-defaults check-compose-no-latest check-gofmt check-test-floor check-ci-contract"

fail=0
case "${1:-}" in
self-tests)
	for g in $GUARDS; do bash "scripts/fw/$g.sh" --self-test || fail=1; done
	;;
guard)
	for g in $GUARDS; do bash "scripts/fw/$g.sh" || fail=1; done
	;;
verify)
	bash "$0" self-tests || fail=1
	bash "$0" guard || fail=1
	;;
verify-full)
	bash "$0" verify || fail=1
	for m in financial portal_server operational_cycle_go; do
		echo "== go test ./$m =="
		(cd "$m" && go test ./...) || fail=1
	done
	;;
golden-update)
	mkdir -p contracts/golden
	grep -oE 'mux\.(HandleFunc|Handle)\("[^"]+"' portal_server/main.go |
		sed -E 's/^mux\.(HandleFunc|Handle)\("//; s/"$//' | LC_ALL=C sort > contracts/golden/portal-routes.txt
	grep -oE '"menu_key":[[:space:]]*"[^"]+"' portal_server/main.go |
		sed -E 's/"menu_key":[[:space:]]*"//; s/"$//' | LC_ALL=C sort > contracts/golden/portal-menus.txt
	grep -oE "\{[[:space:]]*id:[[:space:]]*'[^']+'" financial/web/src/App.jsx |
		sed -E "s/\{[[:space:]]*id:[[:space:]]*'//; s/'\$//" | LC_ALL=C sort > contracts/golden/financial-web-tabs.txt
	git --no-pager diff --stat -- contracts/golden
	echo "golden contracts regenerated — review the diff above and commit it consciously."
	;;
commit-lint)
	shift
	range="${1:-origin/main..HEAD}"
	git rev-list -1 "$range" > /dev/null 2>&1 || { echo "commit-lint: bad range '$range'"; exit 2; }
	rc=0
	while IFS= read -r s; do
		bash scripts/fw/check-commit-subject.sh "$s" || rc=1
	done < <(git log --format=%s "$range")
	exit "$rc"
	;;
*)
	echo "usage: bash fw.sh {self-tests|guard|verify|verify-full|golden-update|commit-lint [range]}"
	[ -n "${1:-}" ] && exit 2
	exit 0
	;;
esac
exit "$fail"
