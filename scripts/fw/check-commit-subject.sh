#!/usr/bin/env bash
# check-commit-subject.sh — commit subjects stay machine-checkable.
#
# Accepted shapes (matches the repo's existing convention):
#   "area: summary"            e.g.  financial-accounts: rebind placeholders
#   "type(area): summary"      e.g.  fix(portal): keep sidebar menus on SSO
#   "type: summary"            e.g.  chore: bump Go to 1.21
# where type ∈ feat|fix|chore|docs|test|refactor|deploy|hotfix|security|audit
# and area is lowercase letters/digits/dash. GitHub's merge commits pass.
#
# Exit codes: 0 accepted · 1 rejected · 2 usage.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

TYPES='feat|fix|chore|docs|test|refactor|deploy|hotfix|security|audit'

accept() { # accept <subject>
	case "$1" in
		"Merge "*) return 0 ;; # GitHub's PR test-merge commit
	esac
	if grep -qE "^($TYPES)\([a-z0-9-]+\): .+" <<<"$1"; then return 0; fi
	if grep -qE "^($TYPES): .+" <<<"$1"; then return 0; fi
	if grep -qE "^[a-z][a-z0-9-]*: .+" <<<"$1"; then return 0; fi
	return 1
}

if [ "${1:-}" = "--self-test" ]; then
	fail=0
	check() { # check <want-rc> <subject>
		if accept "$2"; then got=0; else got=1; fi
		if [ "$got" -ne "$1" ]; then
			echo "self-test FAIL (want rc=$1): $2"
			fail=1
		fi
	}
	check 0 "financial-accounts: rebind placeholders"
	check 0 "fix(portal): keep sidebar menus on SSO"
	check 0 "chore: bump Go to 1.21"
	check 0 "audit: add firewall guards"
	check 1 "updated stuff"
	check 1 "Capital: starts with uppercase area"
	check 1 "no colon here"
	check 0 "Merge 84833c52523faaa13de8d2a815eac4b92b5dc820 into 96f6bcf04b1e4651a2c5ffaac25ebc824e420961"
	fw_selftest_result "commit-subject" "$fail"
fi

subject="${1:-}"
[ -n "$subject" ] || { echo "usage: check-commit-subject.sh \"<subject>\" | --self-test"; exit 2; }
if accept "$subject"; then
	exit 0
fi
echo "commit subject rejected: '$subject'"
echo "required: 'type(area): summary' or 'area: summary' or 'type: summary'"
exit 1
