#!/usr/bin/env bash
# check-migration-safety.sh — destructive SQL guard.
#
# Scans *.up.sql (the forward path; *.down.sql rollbacks are out of scope by
# design) for destructive SQL (DROP, ALTER COLUMN TYPE, RENAME) unless the
# line carries an explicit "fw:allow-destructive" marker. Files already
# carrying unmarked destructive SQL at install time are allowlisted in the
# ledger (migration_safety.allow_files) AND ratcheted by hit count
# (migration_safety.allow_hits_max): adding destructive SQL even to an
# allowlisted file, or a new file with destructive SQL, is a regression.
#
# Exit codes: 0 ok · 1 regression · 2 could-not-measure.
# Self-test: --self-test
set -euo pipefail
. "$(dirname "$0")/lib.sh"

root="$(fw_root)"

patterns='DROP[[:space:]]+(TABLE|COLUMN|INDEX|CONSTRAINT|SCHEMA|DATABASE)|ALTER[[:space:]]+COLUMN[[:space:]]+[A-Za-z_"]+[[:space:]]+TYPE|RENAME[[:space:]]+(TABLE|COLUMN|TO)'

# allowlisted_files <ledger-dir> — basenames allowlisted in the latest migration_safety entry.
allowlisted_files() {
	local entry
	entry=$(fw_latest "$1" migration_safety) || { echo ""; return 0; }
	grep -oE '"allow_files":[^]]*\]' "$entry" | grep -oE '"[^"]+\.up\.sql"' | tr -d '"' || true
}

scan_dir() { # scan_dir <dir-with-sql> <ledger-dir> — 0 ok · 1 regression
	local rc=0 file hits base n allow_total=0
	while IFS= read -r file; do
		hits=$(grep -inE "$patterns" "$file" | grep -v "fw:allow-destructive" || true)
		[ -n "$hits" ] || continue
		base=$(basename "$file")
		n=$(printf '%s\n' "$hits" | grep -c . || true)
		if grep -qxF "$base" <(allowlisted_files "$2"); then
			echo "migration-safety: $base carries $n legacy destructive SQL line(s) (ledger-allowlisted; clean up via expand→contract)"
			allow_total=$((allow_total + n))
		else
			echo "migration-safety: destructive SQL in NEW file $file — expand→contract instead,"
			echo "migration-safety: or mark the line with fw:allow-destructive if truly safe:"
			echo "$hits"
			rc=1
		fi
	done < <(find "$1" -name '*.up.sql' -not -path '*node_modules*' | sort)
	# Ratchet: legacy hits must never grow beyond the ledgered ceiling.
	local max
	max=$(fw_resolve "$2" migration_safety allow_hits_max 2>/dev/null) || max=-1
	if [ "$max" -ge 0 ] && [ "$allow_total" -gt "$max" ]; then
		echo "migration-safety: legacy destructive hits $allow_total > ledger ceiling $max — destructive SQL grew inside the allowlist"
		rc=1
	fi
	return "$rc"
}

run_checks() { # run_checks <root>
	local rc=0 d
	local ledger="$1/quality-baseline.d"
	while IFS= read -r d; do
		scan_dir "$d" "$ledger" || rc=1
	done < <(find "$1" -type d \( -iname '*migration*' -o -iname 'postgres-init' \) -not -path '*node_modules*' 2>/dev/null)
	return "$rc"
}

if [ "${1:-}" = "--self-test" ]; then
	td="$(mktemp -d)"
	trap 'rm -rf "$td"' EXIT
	mkdir -p "$td/quality-baseline.d" "$td/migrations"
	printf '{"migration_safety":{"allow_files":["0001_legacy.up.sql"],"allow_hits_max":1}}\n' > "$td/quality-baseline.d/0001-seed.json"
	{
		echo "CREATE TABLE ok_sql (id text);"
		echo "DROP TABLE legacy_thing;"
	} > "$td/migrations/0001_legacy.up.sql"
	{
		echo "ALTER TABLE ok_sql ADD COLUMN note text;"
		echo "DROP TABLE marked -- fw:allow-destructive"
	} > "$td/migrations/0002_marked.up.sql"
	fail=0
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 || fail=1
	# Grow the allowlisted file past its ceiling:
	echo "DROP TABLE customers;" >> "$td/migrations/0001_legacy.up.sql"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 && { echo "self-test: grown allowlisted file passed"; fail=1; }
	# New file with destructive SQL:
	echo "DROP TABLE users;" > "$td/migrations/0003_new.up.sql"
	FW_ROOT="$td" run_checks "$td" > /dev/null 2>&1 && { echo "self-test: new destructive SQL passed"; fail=1; }
	fw_selftest_result "migration-safety" "$fail"
fi

run_checks "$root"
