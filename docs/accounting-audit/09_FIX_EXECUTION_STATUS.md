# 09 — Fix Execution Status

This file separates **verified findings**, **implemented fixes**, **temporary safety guards**, and **live Production retest**.

## Implemented in source on `accounting-audit-fixes`

### ACC-016 — Bank transfer direction
Status: **FIXED IN BRANCH / LIVE RETEST REQUIRED**

- Backend rejects a new/changed transfer unless it has source→destination `direction=out` semantics and distinct source/destination accounts.
- Frontend compatibility layer `workspaceSafety.js` tracks the last workspace snapshot and normalizes only new/edited legacy-UI transfers from `in` to `out`.
- Unchanged historical transfer records are not silently rewritten.
- Node regression tests for the transfer normalizer were executed locally outside GitHub Actions and passed: 3/3.

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

## Emergency database guards implemented

### ACC-012 / ACC-013 — hard delete of checks/invoices
Status: **PROTECTED IN BRANCH / PROPER VOID UX STILL REQUIRED**

Migration `021_021_protect_financial_source_history.up.sql` rejects disappearance of already-stored financial invoices, receivable checks and payable checks from the workspace. Invoice renumbering is also blocked until a stable immutable-ID renumber workflow is implemented.

This is deliberately a safety guard, not the final UX. The UI must later replace Delete with a formal `Void/Cancel + reason + user + timestamp + reversal` workflow.

### ACC-014 — full finance reset
Status: **PROTECTED IN BRANCH / UI REMOVAL STILL REQUIRED**

Migration 021 rejects replacing an established material financial workspace with an empty financial workspace. The dangerous Production reset control should still be removed or moved to a controlled archival/maintenance flow.

## Tests implemented

### Go audit regression tests
File: `financial/internal/presentation/handler/accounting_audit_regression_test.go`

- reject inverted transfer direction;
- accept source→destination transfer and derive Dr destination / Cr source;
- reversal keeps original accounting date;
- owned sale cannot omit COGS.

### Portal security regression tests
File: `portal_server/password_visibility_test.go`

- stored passwords must not be recovered into metadata responses;
- a newly-issued password may be returned once.

### JavaScript transfer tests
File: `financial/web/src/workspaceSafety.test.js`

- new transfer normalization;
- edited transfer normalization without rewriting unchanged history;
- non-transfer/already-correct transfer preservation.

Local independent execution result: **3 passed / 0 failed**.

## Verified but not yet fixed

### SEC-001 — stored passwords visible/recoverable
Status: **OPEN P0**

Regression tests exist, but `portal_server/main.go` still decrypts stored employee passwords for list responses. Do not mark this fixed until source is changed and the `/team` page no longer reveals current passwords.

### ACC-001 / 002 / 003 / 004 / 005 — Financial Health
Status: **OPEN P0/P1**

Financial Health remains based on bespoke workspace formulas rather than authoritative ledger balances. Profit, EBITDA, operating cash flow, current liabilities, assigned checks and equity handling require redesign/reconciliation.

### ACC-018 / ACC-019 — bounced/returned checks in Credit/Advisor
Status: **OPEN P0**

Bad checks must not be counted as collectible future liquidity or credit support.

### ACC-008 — invoice renumber/edit identity
Status: **OPEN P0; SAFETY BLOCK IN PLACE**

DB guard prevents unsafe disappearance/renumbering, but the final solution is a stable immutable invoice ID plus a controlled number-edit workflow.

### Tax/Aging/payment-term findings
Status: **OPEN P1**

ACC-006, ACC-007, ACC-010, ACC-011 and ACC-020 remain to be corrected.

### Human owner vs Business Brain service account
Status: **OPEN P0 ARCHITECTURE / PRODUCTION DATA RETEST**

The human company owner and integration service identity must be separate. The human account should be the tenant `owner`; the service identity must be least-privilege and unable to manage staff unless explicitly required.

## CI status

GitHub Actions currently fails before workflow steps execute. Therefore no claim is made that the new Go/portal tests passed in CI. The JavaScript pure-helper tests were executed independently with Node 22 and passed 3/3.

## Merge/deploy gate

Keep PR #65 Draft and do not deploy this branch until:

1. changed Go source is compiled/tested in a working runner;
2. migrations 020/021 are tested against a disposable PostgreSQL copy;
3. portal password exposure is fixed;
4. Financial Health P0 formulas are corrected or the page is clearly disabled pending correction;
5. `AGENT_TEST_` live scenarios run on `paregol` after deployment;
6. source → settlement → ledger → trial balance → report reconciliation passes.
