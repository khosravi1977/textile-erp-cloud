# Financial ledger rollout and rollback

The accounting hardening migration is additive and preserves existing workspace, inventory, invoice, user, and audit data. It must never be tested first against the only production copy.

## Release gate

1. Create a timestamped PostgreSQL backup and verify that the backup is readable.
2. Restore that backup into an isolated database copy.
3. Run `deploy/production/financial-ledger-preflight.sql` against the copy. Resolve duplicates or unbalanced legacy vouchers explicitly; do not delete or rewrite them automatically.
4. Run every migration against the copy, then run the preflight again and compare all reported row counts.
5. Run the financial integration tests, including tenant isolation, balanced posting, and posted-voucher immutability.
6. Merge only after GitHub checks pass. Production images are built in GitHub and deployed by the restricted `viora-deploy` helper.

## Rollback

If health or smoke checks fail, `viora-deploy` must restore the previous immutable application image and configuration release. The additive database schema remains in place because removing audit columns, fiscal periods, or ledger constraints could destroy evidence. Restoring the database backup is reserved for a confirmed database-corruption incident and requires an explicit maintenance window.

The release is accepted only after HTTPS, `/health`, login, financial workspace reads, a non-destructive accounting report request, service logs, and pre/post data counts have been verified.
