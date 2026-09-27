#!/usr/bin/env bash
# check-ci-contract.sh — the meta-guard: stops every other guard from being
# quietly edited away (the hamneshin pattern, adapted).
#
# It does not measure quality; it verifies the CI contract itself:
#   R1 invocation:   every scripts/fw/check-*.sh guard is invoked by some
#                    workflow in .github/workflows/ (comment-only mentions
#                    don't count — full-line comments are stripped first).
#   R2 self-test:    every guard invoked in CI also runs with --self-test
#                    somewhere in CI (a guard whose self-test isn't run is one
#                    rot away from dead).
#   R3 no bypasses:  no continue-on-error, no `|| true`/`|| :`/`|| exit 0`,
#                    no set +e, no (SKIP|BYPASS|IGNORE)_* env — except legacy
#                    occurrences allowlisted in the ledger
#                    (ci_contract.bypass_allow_files / bypass_allow_max).
#   R4 pinned:       every `uses:` is name@<40-hex-sha> — no moving tags.
#   R5 self-wired:   this meta-guard runs for real in CI (a non-self-test
#                    invocation) — removing THAT line would otherwise be free.
#
# Exit codes: 0 contract holds · 1 violation.
# Self-test: --self-test (plants each violation class in a synthetic tree).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

# check_contract <repo-root> — the seam the self-test drives.
check_contract() {
	local root="$1"
	local workflows_dir="$root/.github/workflows"
	local guards_dir="$root/scripts/fw"
	local rc=0

	[ -d "$workflows_dir" ] || { echo "ci-contract: no .github/workflows in $root"; return 1; }
	local wf
	wf=$(ls "$workflows_dir"/*.yml "$workflows_dir"/*.yaml 2>/dev/null | LC_ALL=C sort || true)
	[ -n "$wf" ] || { echo "ci-contract: no workflow files"; return 1; }

	# Invocation surface: every line of every workflow with comments stripped
	# (full-line AND trailing `# ...`). A guard can only run from one of these
	# lines; a comment mentioning a guard proves nothing.
	local inv="" f
	while IFS= read -r f; do
		[ -n "$f" ] || continue
		inv="$inv$(sed -E 's/(^|[[:space:]])#.*$//' "$f" 2>/dev/null || true)
"
	done < <(printf '%s\n' "$wf")

	# R1 + R2: per-guard invocation and self-test coverage.
	local guard base
	while IFS= read -r guard; do
		base=$(basename "$guard")
		if ! printf '%s\n' "$inv" | grep -qF "$base"; then
			echo "ci-contract R1: guard $base is not invoked by any workflow"
			rc=1
			continue
		fi
		if ! printf '%s\n' "$inv" | grep -F "$base" | grep -qF -- "--self-test"; then
			echo "ci-contract R2: guard $base never runs with --self-test in CI"
			rc=1
		fi
	done < <(find "$guards_dir" -maxdepth 1 -name 'check-*.sh' ! -name 'check-ci-contract.sh' 2>/dev/null | LC_ALL=C sort)

	# R3: bypass constructs — legacy occurrences are ledger-allowlisted.
	local bypass_re='continue-on-error|\|[|][[:space:]]*(:|exit[[:space:]]+0|true)|(SKIP|BYPASS|IGNORE)_[A-Z_]+|set[[:space:]]+\+e'
	local entry allow_max allowlist base n line
	if entry=$(fw_latest "$root/quality-baseline.d" ci_contract 2>/dev/null); then
		allow_max=$(grep -oE '"bypass_allow_max":[[:space:]]*[0-9]+' "$entry" | grep -oE '[0-9]+' | head -n1 || true)
		[ -n "$allow_max" ] || allow_max=0
		allowlist=$(grep -oE '"bypass_allow_files":[^]]*\]' "$entry" | grep -oE '"[^"]+\.y(a)?ml"' | tr -d '"' || true)
	else
		allow_max=0
		allowlist=""
	fi
	while IFS= read -r f; do
		[ -n "$f" ] || continue
		n=$(grep -cE "$bypass_re" "$f" || true)
		[ "$n" -gt 0 ] || continue
		base=$(basename "$f")
		if printf '%s\n' "$allowlist" | grep -qxF "$base" && [ "$n" -le "$allow_max" ]; then
			echo "ci-contract R3: $base carries $n legacy bypass construct(s) (ledger-allowlisted)"
		else
			while IFS= read -r line; do
				echo "ci-contract R3: bypass construct in $f: $line"
				rc=1
			done < <(grep -nE "$bypass_re" "$f" || true)
		fi
	done < <(printf '%s\n' "$wf")

	# R4: actions pinned by commit SHA (local ./ actions allowed).
	while IFS= read -r f; do
		[ -n "$f" ] || continue
		while IFS= read -r line; do
			echo "ci-contract R4: action not pinned by 40-hex SHA in $f: $line"
			rc=1
		done < <(grep -E '^[[:space:]]*(-[[:space:]]*)?uses:' "$f" |
			grep -vE "uses:[[:space:]]*[^@[:space:]]+@[0-9a-f]{40}([[:space:]]|$)|uses:[[:space:]]*\./" || true)
	done < <(printf '%s\n' "$wf")

	# R5: the meta-guard must itself have a REAL (non-self-test) run in CI.
	if ! printf '%s\n' "$inv" | grep -F "check-ci-contract.sh" | grep -vqF -- "--self-test"; then
		echo "ci-contract R5: scripts/fw/check-ci-contract.sh has no real (non-self-test) invocation in any workflow"
		rc=1
	fi

	return "$rc"
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	fail=0

	mk_tree() { # mk_tree <name> <workflow-body>
		local t="$td/$1"
		mkdir -p "$t/.github/workflows" "$t/scripts/fw"
		printf '#!/usr/bin/env bash\nexit 0\n' > "$t/scripts/fw/check-alpha.sh"
		printf '#!/usr/bin/env bash\nexit 0\n' > "$t/scripts/fw/check-beta.sh"
		printf '%s\n' "$2" > "$t/.github/workflows/ci.yml"
	}

	clean='
name: ci
on: [push]
jobs:
  quality:
    steps:
      - uses: actions/checkout@8f4b7f84864484a7bf31766abe9204da3cbe65b3 # v4
      - run: bash scripts/fw/check-alpha.sh --self-test
      - run: bash scripts/fw/check-alpha.sh
      - run: bash scripts/fw/check-beta.sh --self-test
      - run: bash scripts/fw/check-beta.sh
      - run: bash scripts/fw/check-ci-contract.sh --self-test
      - run: bash scripts/fw/check-ci-contract.sh'

	mk_tree clean "$clean"
	check_contract "$td/clean" > /dev/null || { echo "self-test: clean tree refused"; fail=1; }

	mk_tree noinvoke "$clean"
	sed -i '/check-beta.sh/d' "$td/noinvoke/.github/workflows/ci.yml"
	check_contract "$td/noinvoke" > /dev/null 2>&1 && { echo "self-test: R1 unwired guard passed"; fail=1; }

	mk_tree noselftest "$clean"
	sed -i 's|check-beta.sh --self-test|check-beta.sh|' "$td/noselftest/.github/workflows/ci.yml"
	check_contract "$td/noselftest" > /dev/null 2>&1 && { echo "self-test: R2 self-test-less guard passed"; fail=1; }

	mk_tree commentonly "$clean"
	sed -i 's|^\(.*\)bash scripts/fw/check-alpha.sh|\1# bash scripts/fw/check-alpha.sh|' "$td/commentonly/.github/workflows/ci.yml"
	check_contract "$td/commentonly" > /dev/null 2>&1 && { echo "self-test: R1 comment-only reference passed"; fail=1; }

	mk_tree bypass "$clean
      - run: bash scripts/fw/check-alpha.sh || true"
	check_contract "$td/bypass" > /dev/null 2>&1 && { echo "self-test: R3 || true passed"; fail=1; }

	mk_tree coe "$clean"
	sed -i 's|- run: bash scripts/fw/check-alpha.sh$|- run: bash scripts/fw/check-alpha.sh\n        continue-on-error: true|' "$td/coe/.github/workflows/ci.yml"
	check_contract "$td/coe" > /dev/null 2>&1 && { echo "self-test: R3 continue-on-error passed"; fail=1; }

	mk_tree sete "$clean
      - run: set +e"
	check_contract "$td/sete" > /dev/null 2>&1 && { echo "self-test: R3 set +e passed"; fail=1; }

	mk_tree allowbypass "$clean
      - run: bash scripts/fw/check-alpha.sh || true"
	mkdir -p "$td/allowbypass/quality-baseline.d"
	printf '{"ci_contract":{"bypass_allow_files":["ci.yml"]},"bypass_allow_max":1}\n' > "$td/allowbypass/quality-baseline.d/0001-seed.json"
	check_contract "$td/allowbypass" > /dev/null 2>&1 || { echo "self-test: R3 ledger-allowlisted bypass failed"; fail=1; }

	mk_tree tagpin "$clean"
	sed -i 's|actions/checkout@8f4b7f84864484a7bf31766abe9204da3cbe65b3|actions/checkout@v4|' "$td/tagpin/.github/workflows/ci.yml"
	check_contract "$td/tagpin" > /dev/null 2>&1 && { echo "self-test: R4 tag-pinned action passed"; fail=1; }

	mk_tree nometaguard "$clean"
	sed -i '/check-ci-contract/d' "$td/nometaguard/.github/workflows/ci.yml"
	check_contract "$td/nometaguard" > /dev/null 2>&1 && { echo "self-test: R5 meta-guard unwired passed"; fail=1; }

	mk_tree metaonly "$clean"
	sed -i 's|- run: bash scripts/fw/check-ci-contract.sh$|- run: echo meta|' "$td/metaonly/.github/workflows/ci.yml"
	check_contract "$td/metaonly" > /dev/null 2>&1 && { echo "self-test: R5 self-test-only meta-guard passed"; fail=1; }

	fw_selftest_result "ci-contract" "$fail"
fi

check_contract "$(fw_root)"
