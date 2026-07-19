# Legacy Operational DB Sync

This project now includes a repeatable sync tool for importing the legacy operational SQLite database into the current operational database used by the new project.

## Source and target

- Source: legacy SQLite file such as `H:\9\database.db`
- Target: current operational database
  - default local target is PostgreSQL on `localhost:5433`
  - the tool also respects `OPERATIONAL_DATABASE_URL` or `OPERATIONAL_DB_DRIVER`

## Entry points

- PowerShell: [sync_legacy_operational_db.ps1](/F:/project/textile_erp_clean/sync_legacy_operational_db.ps1)
- Batch wrapper: [sync_legacy_operational_db.bat](/F:/project/textile_erp_clean/sync_legacy_operational_db.bat)
- Go command: [operational_cycle_go/cmd/legacysync/main.go](/F:/project/textile_erp_clean/operational_cycle_go/cmd/legacysync/main.go)

## Usage

Dry run:

```powershell
cd F:\project\textile_erp_clean
.\sync_legacy_operational_db.ps1 -Source 'H:\9\database.db' -DryRun
```

Real sync:

```powershell
cd F:\project\textile_erp_clean
.\sync_legacy_operational_db.ps1 -Source 'H:\9\database.db'
```

Each run writes a report under `legacy_sync_reports\YYYYMMDD_HHMMSS\summary.json`.

## What the tool does

- compares legacy SQLite tables against the live operational schema
- performs idempotent `upsert` into the new database
- preserves legacy IDs on business tables where IDs are still meaningful
- handles renamed columns
- derives `f_khor.kala_name_f_khor` from linked `salon.kala_salon` when the legacy DB does not have that column
- creates compatibility table `v_kh_moto` in the new DB if needed, then imports it
- archives legacy-only non-business tables to JSON files instead of silently dropping them

## Important schema differences handled

- `menu_items.menu_url -> menu_items.path`
- `menu_items.menu_icon -> menu_items.icon`
- legacy `f_khor.barcode_code` has no direct target column
  - the new schema uses `kala_name_f_khor`
  - the sync derives it from the related `salon` row when possible

## Legacy-only tables

These are not part of the active operational app schema and are archived to JSON reports:

- `_migrations`
- `auth_tokens`
- `flask_sessions`
- corrupted/unused legacy table name variant around `gerezan`

`v_kh_moto` is treated differently because it contains business data and is still useful for bridge/reporting.

## Validation notes from current comparison

Legacy source row counts seen during analysis:

- `salon`: 176
- `nakh_salon`: 146
- `f_khor`: 43
- `menu_items`: 19
- `v_kh_moto`: 3

After sync into the local project DB:

- `salon`: 176
- `nakh_salon`: 146
- `f_khor`: 43
- `v_kh_moto`: 3

`menu_items` is higher in the target because the new app seeds additional menus that do not exist in the legacy system. This is expected and does not indicate data loss.

## Recommendation

Always run `-DryRun` first on the newest legacy DB snapshot, inspect `summary.json`, then run the real sync.
