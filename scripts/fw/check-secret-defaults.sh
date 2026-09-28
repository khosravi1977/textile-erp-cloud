#!/usr/bin/env bash
# check-secret-defaults.sh — no silent default credentials.
#
# Counts credential-looking literals across the WHOLE repo (repo-total
# ratchet, not per-file):
#   - Go / deploy YAML: literals in env-fallback positions
#     (getEnv("KEY", "change_me")) — validation code that COMPARES against
#     those literals (fail-fast guards) is not a default and does not match;
#     three sanctioned portal env-defaults are allowlisted by exact shape
#     (validatePortalProductionConfig rejects them in production).
#   - .env* files: KEY=<literal> dotenv lines.
# Adding a NEW occurrence past the ledgered total (secret_defaults.
# max_occurrences) is a regression; the ceiling only moves down. A line can
# self-declare "fw:allow-default".
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"

# secret_re — credential-looking literals (kept loose on purpose).
secret_re='(change_this_|change_me|admin123|local-secret|secret-change|password123)'
# default_position_re — the literal must sit in an env-fallback position.
default_position_re='(getEnv|env)\("[A-Za-z0-9_]+",[^)]*"'
# dotenv_re — .env files: KEY=<literal>
dotenv_re='^[A-Za-z0-9_]+='
# allowline_re — sanctioned portal env-defaults (rejected in production).
allowline_re='env\("(PORTAL_ADMIN_PASSWORD|PORTAL_SESSION_SECRET|OPERATIONAL_ADMIN_PASSWORD)",'

file_hits() { # file_hits <file> — count of violations in one file
	local file="$1" n=0
	case "$file" in
	*.env*)
		n=$(grep -icE "${dotenv_re}${secret_re}" "$file" 2>/dev/null || true) ;;
	*)
		n=$(grep -inE "${default_position_re}${secret_re}" "$file" 2>/dev/null |
			grep -v "fw:allow-default" | grep -cvE "$allowline_re" || true) ;;
	esac
	printf '%s\n' "${n:-0}"
}

run_checks() { # run_checks <root>
	local max total=0 f n
	local ledger="$1/quality-baseline.d"
	max=$(fw_resolve "$ledger" secret_defaults max_occurrences 2>/dev/null) || {
		echo "secret-defaults: no secret_defaults.max_occurrences in ledger"
		return 2
	}
	local report=""
	while IFS= read -r f; do
		case "$f" in *_test.go | *node_modules*) continue ;; esac
		n=$(file_hits "$f")
		[ "$n" -gt 0 ] || continue
		total=$((total + n))
		report="$report
secret-defaults: $f: $n"
	done < <(find "$1" \( -name '*.go' -o \( -name '*.yml' -path '*deploy*' \) -o -name '.env*' \) -not -path '*node_modules*' 2>/dev/null | sort)
	if [ "$total" -gt "$max" ]; then
		echo "secret-defaults: $total default-credential occurrences > ledger ceiling $max — require explicit env, no silent default:$report"
		return 1
	fi
	echo "secret-defaults: $total occurrences (<= ceiling $max)"
	return 0
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/quality-baseline.d" "$td/portal_server"
	printf '{"secret_defaults":{"max_occurrences":2}}\n' > "$td/quality-baseline.d/0001-seed.json"
	{
		# Two legacy fallbacks (within ceiling):
		echo 'password := getEnv("DB_PASSWORD", "change_me")'
		echo 'tok := getEnv("JWT_SECRET", "secret-change-me-please")'
		# Validation comparing against a literal — NOT a default, must not match:
		echo 'if strings.Contains(strings.ToLower(p), "admin123") || p == "change_this_x" { reject() }'
		# Marker escape hatch:
		echo 'const legacy = getEnv("LEGACY_PW", "change_me") // fw:allow-default'
	} > "$td/portal_server/main.go"
	fail=0
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 || fail=1
	# A third raw default pushes past the ceiling:
	echo 'opw := getEnv("OPW", "admin123")' >> "$td/portal_server/main.go"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 && { echo "self-test: extra default passed"; fail=1; }
	sed -i '$d' "$td/portal_server/main.go" # revert to the within-ceiling tree
	# dotenv path counts too: 2 legacy + 1 dotenv = 3 > ceiling 2
	printf 'DB_PASSWORD=change_me\n' > "$td/portal_server/.env.example"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 && { echo "self-test: dotenv default passed"; fail=1; }
	fw_selftest_result "secret-defaults" "$fail"
fi

run_checks "$root"
