# 09 — Fix Execution Status

This file separates **verified findings**, **implemented fixes**, **temporary safety guards**, and **live Production retest**.

## Implemented in source on `accounting-audit-fixes`

### ACC-001 / 002 / 003 / 004 / 005 — Financial Health
Status: **FIXED IN BRANCH / LIVE RETEST REQUIRED**

- The rendered `سلامت مالی` page is redirected at build time from the legacy workspace formulas to `LedgerFinancialHealthPage`.
- Official values are loaded from `/accounting/reports`, whose summary/trial-balance data comes only from Posted journal vouchers.
- Net profit is ledger income minus ledger expense (including COGS expense accounts).
- Cash/bank, receivables, inventory, current liabilities, working capital, current ratio, signed equity and debt/equity are calculated from trial-balance accounts rather than ad-hoc UI arrays.
- Ledger debit/credit imbalance is surfaced as a critical health alert.
- Pure ledger-health tests were executed independently with Node 22 and passed: **2/2**.

The legacy `buildFinancialHealth()` code still exists as dead code in `App.jsx` and should be removed during refactor; it is no longer used by the rendered health page on this branch.

### ACC-016 — Bank transfer direction
Status: **FIXED IN BRANCH / LIVE RETEST REQUIRED**

- Backend rejects a new/changed transfer unless it has source→destination `direction=out` semantics and distinct source/destination accounts.
- Frontend compatibility layer `workspaceSafety.js` tracks the last workspace snapshot and normalizes only new/edited legacy-UI transfers from `in` to `out`.
- Unchanged historical transfer records are not silently rewritten.
- Node regression tests for the transfer normalizer were executed independently and passed: **3/3**.

### ACC-009 — Owned sale without COGS
Status: **FIXED IN BRANCH / LIVE RETEST REQUIRED**

- New/changed `pricingMode=sale` invoices with positive quantity/total and zero `costAmount` are rejected before persistence.
- Legacy rows are not rewritten merely because another module is saved.
- Go regression test added; GitHub Actions infrastructure has not executed it yet.

### ACC-017 — Reversal date crosses periods
Status: **FIXED IN SOURCE + DB DEFENSE / LIVE RETEST REQUIRED**

- `reverseLedgerEntry()` now keeps the original voucher date instead of `time.Now()`.
- Migration `020_020_reversal_period_integrity.up.sql` enforces the same date at database level and rejects reversal if the original period is Closed.
- Go regression test added; GitHub Actions infrastructure has not executed it yet.

### ACC-006 — Aging label
Status: **FIXED IN BRANCH / LIVE RETEST REQUIRED**

- The 31–60 day bucket is now displayed as `سررسید گذشته ۳۱ تا ۶۰ روز`.
- ACC-007 remains open because overdue age still needs a true contractual invoice due date rather than invoice issue date.

### ACC-010 / ACC-011 — Tax report reconciliation
Status: **FIXED IN BRANCH / LIVE RETEST REQUIRED**

- Non-financial/consignment incoming rows are excluded from financial purchase totals.
- Excel summary taxable/VAT/total values are calculated from the actual filtered export rows, so the summary reconciles mathematically to those rows.
- A later tax redesign can still separate output VAT/input VAT/net payable VAT into dedicated official columns.

### ACC-018 / ACC-019 — bad checks in Credit / Advisor
Status: **FIXED IN BRANCH / LIVE RETEST REQUIRED**

- Collectible receivable checks are normalized to `status === 'open'` in the affected formulas.
- Bounced/returned/assigned/cleared checks no longer count as future collectible liquidity or credit support.

### ACC-020 — payment-term percentages
Status: **FIXED IN BRANCH / LIVE RETEST REQUIRED**

- Editing check percentage clamps it to 0..100 and automatically sets cash percentage to `100 - check`.
- Existing cash edit already performed the reverse normalization.

### SEC-001 — stored employee passwords visible/recoverable
Status: **FIXED IN PRODUCTION DOCKER BUILD PATH / LIVE RETEST REQUIRED**

- Root cause remains in the large legacy `portal_server/main.go`: list/metadata responses could recover the encrypted current password and management UI could prefill/display it.
- A fail-closed build hardening package was added at `portal_server/internal/auditpatch`.
- Production `portal_server/Dockerfile` now runs `go run ./tools/auditpatch` before compiling the portal binary.
- The transform removes password recovery from `accessResponse`, prevents admin password prefill and changes team-management guidance so saved passwords are not advertised as recoverable.
- The transform verifies its expected source patterns and is idempotent; if the legacy source structure changes unexpectedly, the production Docker build fails rather than silently producing an unhardened binary.
- Portal regression tests verify the production hardening transform and idempotency.

Important: this does **not** mean the currently deployed Production portal is fixed. The hardened Docker artifact still needs a successful build/deploy and `/team` live retest. Also, a developer who bypasses Docker and runs raw `go run .` would still run the untransformed legacy source; a later refactor should move the fix directly into source and remove this temporary build transform.

## Emergency database/source-history guards implemented

### ACC-012 / ACC-013 — hard delete of posted/source documents
Status: **PROTECTED IN BRANCH / PROPER VOID UX STILL REQUIRED**

Migration `021_021_protect_financial_source_history.up.sql` rejects disappearance of already-stored:

- financial invoices;
- incoming/purchase invoices;
- financial yarn-out documents;
- expenses;
- opening balances;
- receivable checks;
- payable checks.

Invoice renumbering is also blocked until a stable immutable-ID amendment workflow is implemented.

This is deliberately a safety guard, not the final UX. The UI must later replace Delete with a formal `Void/Cancel + reason + user + timestamp + reversal` workflow.

### ACC-014 — full finance reset
Status: **FIXED WITH UI + DB GUARD IN BRANCH / LIVE RETEST REQUIRED**

- Vite build transform removes the dangerous normal Production reset action and shows a disabled notice instead.
- Migration 021 independently rejects replacing an established material financial workspace with an empty workspace.

## Build-time guarded UI transformation

`financial/web/auditAppTransform.js` applies only exact, asserted source transformations to the large legacy `App.jsx`. If any expected pattern count changes, the Vite build fails instead of silently applying an unsafe patch.

It currently routes/patches:

- Ledger-backed Financial Health page;
- Aging 31–60 label;
- collectible-check policy;
- payment-term normalization;
- non-financial tax purchase exclusion;
- tax Excel row-summary reconciliation;
- full-finance-reset UI removal.

Independent transform tests were executed with Node 22 and passed: **2/2**.

## Tests implemented

### Go audit regression tests
File: `financial/internal/presentation/handler/accounting_audit_regression_test.go`

- reject inverted transfer direction;
- accept source→destination transfer and derive Dr destination / Cr source;
- reversal keeps original accounting date;
- owned sale cannot omit COGS.

### Portal security regression tests
File: `portal_server/password_visibility_test.go`

- production build hardening removes credential-recovery patterns;
- the hardening transform is idempotent.

### JavaScript tests executed independently

- `workspaceSafety`: **3 passed / 0 failed**.
- `ledgerHealth`: **2 passed / 0 failed**.
- guarded App transform fixture: **2 passed / 0 failed**.

Total independently executed JS audit tests in this pass: **7 passed / 0 failed**.

## Verified but not yet fully resolved

### ACC-007 — aging due date
Status: **OPEN P1**

The display label is corrected, but true aging still requires an explicit contractual due date on invoices.

### ACC-008 — invoice renumber/edit identity
Status: **OPEN P0; SAFETY BLOCK IN PLACE**

DB guard prevents unsafe disappearance/renumbering, but the final solution is a stable immutable invoice ID plus a controlled number-edit/amendment workflow.

### ACC-015 — duplicate-check UI/API mismatch
Status: **OPEN P1 UX CONSISTENCY**

Backend uniqueness already blocks duplicate check numbers. The remaining defect is that the UI can offer an override confirmation that the server will reject.

### Human owner vs Business Brain service account
Status: **OPEN P0 ARCHITECTURE / PRODUCTION DATA RETEST**

The human company owner and integration service identity must be separate. The human account should be the tenant `owner`; the service identity must be least-privilege and unable to manage staff unless explicitly required.

## CI status

GitHub Actions currently fails before workflow steps execute. Therefore no claim is made that the new Go/portal tests passed in CI. The pure JavaScript audit tests above were executed independently with Node 22 and passed 7/7.

## Merge/deploy gate

Keep PR #65 Draft and do not deploy this branch until:

1. changed Go source is compiled/tested in a working runner;
2. migrations 020/021 are tested against a disposable PostgreSQL copy;
3. production Docker builds for financial/portal succeed, including portal credential hardening;
4. `AGENT_TEST_` live scenarios run on `paregol` after deployment;
5. source → settlement → ledger → trial balance → report reconciliation passes;
6. human owner/service-account identities are verified in Production.
