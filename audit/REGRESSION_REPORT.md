# Regression Report

## Regression controls added

### Portal security/RBAC
`portal_server/tools/hardener/main_test.go`
- validates the hardening transform against the repository's real `portal_server/main.go`;
- verifies stored-password response patterns are removed;
- verifies unknown-role fail-closed behavior is generated;
- verifies owner/manager/accountant/viewer operational mapping;
- verifies the transform is idempotent.

`portal_server/Dockerfile`
- runs the hardener and gofmt before compiling the production portal binary.

`.github/workflows/ci.yml`
- portal test step now hardens `main.go`, formats it, then runs `go test ./...`, so CI is intended to exercise the same source semantics used by production build.

### Financial accounting backend
`financial/internal/presentation/handler/workspace_summary_audit_test.go`
- internal transfer keeps total liquidity unchanged;
- source/destination balances change by opposite amounts;
- gross margin uses COGS instead of purchases;
- operating profit subtracts operating expense after gross margin;
- non-financial incoming records do not enter financial purchase KPI;
- output VAT is excluded from accounting revenue;
- assigned and cleared receivable checks are excluded from receivable-check assets.

`financial/internal/presentation/handler/accounting_reversal_period_test.go`
- reversal keeps original accounting date;
- debit and credit sides are reversed correctly.

`financial/internal/presentation/handler/accounting_period_guard_test.go`
- Open→Closed is allowed;
- Closed→Closed is idempotent;
- Closed→Open is rejected.

`financial/internal/presentation/handler/workspace_alerts_audit_test.go`
- a valid internal transfer does not create a false negative-balance alert;
- assigned receivable checks do not produce ordinary overdue alerts;
- bounced checks produce a dedicated returned-check alert and are not double-classified as ordinary overdue.

`financial/internal/presentation/handler/workspace_lifecycle_guard_test.go`
- valid open-check assignment to one matching purchase succeeds;
- the same check cannot settle two purchases;
- a cleared check cannot become assigned;
- a new check cannot bypass the open state and be created directly as assigned;
- manual assignment to an unknown/non-supplier party is rejected;
- manual assignment to a known supplier is allowed;
- assigned payment/check amount mismatch is rejected;
- removing a purchase assignment cannot leave an orphaned linked assigned check;
- unchanged legacy assigned rows do not block an unrelated save.

`financial/internal/presentation/handler/legacy_integrity_guard_test.go`
- legacy profitability rejects missing revenue;
- legacy credit report does not return a fabricated fixed profile;
- legacy advisor does not generate advice from fabricated fixed profile.

### Financial frontend
`financial/web/src/financialIntegrity.test.js`
- non-owner missing/empty/invalid permission lists fail closed;
- owner/admin fallback remains full-access;
- derived child page permissions remain controlled by parent permissions;
- internal transfer preserves total cash;
- profit uses revenue net of output VAT and COGS;
- no fabricated budget variance;
- assigned/cleared checks excluded from receivable assets;
- aging bucket labels are unique and correct.

`financial/web/src/viteIntegrityTransform.test.js`
- applies the production Vite transform to the real `App.jsx` source;
- verifies old fail-open permission and legacy Financial Health implementation are removed from built source;
- verifies non-financial purchases are excluded from tax report population;
- verifies corrected credit exposure formula;
- verifies check lookup/assignment is open-only and exact rather than ambiguous by duplicate number;
- verifies misleading EBITDA/budget labels are removed;
- verifies transform idempotence.

`financial/web/package.json`
- production `npm run build` is gated by `npm test` before `vite build`.

## Targeted isolated execution completed
The available execution environment could not install external dependencies or clone the private repository directly, so targeted standard-library/local harnesses were used for the highest-risk pure calculations.

Results:
- Go accounting invariant harness: **PASS**
  - internal transfer liquidity
  - COGS gross-margin calculation
  - historical reversal date and debit/credit swap
- Go lifecycle/period harness: **PASS**
  - valid single receivable-check assignment
  - duplicate check assignment rejection
  - Closed→Open fiscal-period rejection
  - Open→Closed transition
  - transfer source/destination and total-liquidity invariant
- Node syntax/transform harness: **PASS**
  - build transform syntax
  - deterministic transformation behavior
- Node financial integrity helper harness: **PASS**
  - permission fail-closed logic
  - transfer balance logic
  - COGS/revenue financial-health math
  - check asset treatment

These are targeted validations, not a substitute for repository-wide CI or production E2E. The complete repository lifecycle/period/alert tests are committed and wired into the normal Go test suite but await an actually executing CI runner.

## Repository CI status
The existing CI workflow is configured to execute:
1. deployment smoke syntax and compose release-path validation;
2. financial Go tests and PostgreSQL migration integration;
3. operational Go tests;
4. hardened portal tests;
5. financial web tests + production build;
6. operational web production build;
7. Telegram relay/security/runtime tests;
8. Docker Compose config;
9. Kustomize render.

During this audit GitHub Actions runs repeatedly ended before any recorded step. Job metadata returned an empty/null step list and job logs were unavailable. The latest failed run was explicitly retried and again completed with `steps: null`. Therefore:

**CI RESULT: BLOCKED — runner/account infrastructure did not execute the test steps.**

This is not recorded as a code test failure and the PR is intentionally not merged.

## Live production regression status
Authenticated production runtime/database/browser access was unavailable from the audit environment. No production test data was created and no production data was changed.

**LIVE E2E RESULT: BLOCKED — not executed, not falsely marked PASS.**

## Regression gate before merge/deploy
The PR should not be merged/deployed until the normal CI quality job actually executes and passes. After deployment to a controlled environment, run the `AGENT_TEST_` live matrix in `FINANCIAL_TEST_MATRIX.md`, including DB/journal/report reconciliation and PDF/Excel output checks.
