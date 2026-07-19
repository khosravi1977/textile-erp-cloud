# Textile ERP Financial

This service is the active financial frontend/backend bundle for the project.

## Active entry points
- Frontend: `web/index.html` -> `web/src/main.jsx` -> `web/src/App.jsx`
- Backend: `cmd/api`
- Migrations: `cmd/migrate`

## Notes
- The active financial UI is the React implementation in `web/src/App.jsx`.
- The old Mantine/TypeScript draft files were removed so there is only one live frontend path.
- Database access is tenant-aware through `company_id`.
- Migration `013_013_schema_hardening.up.sql` hardens tenant relations by backfilling `company_id`, adding composite foreign keys, and forcing RLS on business tables.
- `financial_users` and `user_module_access` keep RLS enabled but not forced, because login happens before a tenant session is established.

## Local run
- Financial API: `go run ./cmd/api`
- Migration utility: `go run ./cmd/migrate`
- Financial web preview: `npm run build && npm run preview -- --host 127.0.0.1 --port 8173`
