# 08 — Executive Summary

## Audit status

The Viora Accounting Auditor has been started against the current Textile ERP source and the observed `paregol` production behavior. This is an active audit/fix branch, not a completed certification.

### Scope discovered
- Financial pages: **20**
- Nested operational bridge tabs: **9**
- Verified defects recorded: **20 accounting/reporting defects + 2 security/permission architecture defects**
- P0 items: multiple; see roadmap and bug report
- Regression tests added: **5** (3 accounting + 2 credential-handling)
- Database hardening staged: **1 migration** for reversal-period integrity

## Highest-risk findings

1. Production full financial reset can erase active workspace state.
2. Posted invoices/checks can be hard-deleted instead of formally voided.
3. Bank-to-bank transfer direction is inverted.
4. Ordinary sales can recognize revenue without COGS/inventory relief.
5. Financial Health KPIs are not ledger-backed and can materially overstate profit/liquidity.
6. Bounced/returned receivable checks can still be counted as future liquidity/credit support.
7. Reversal vouchers are generated with today's date, distorting period reporting.
8. Stored employee passwords are recoverable/displayed in team management.
9. Human owner and integration/service-account permissions need separation.

## Positive controls already present

- Derived ledger entries are balanced before acceptance.
- Manual journals require balanced one-sided lines.
- Posted journal vouchers have strong PostgreSQL immutability/balance safeguards.
- Fiscal periods can be closed and posting into a closed date is rejected.
- Existing tests cover purchase VAT, assigned-check settlement, owned-yarn COGS, bounced receivable checks and returned payable checks.

## Changes staged on branch `accounting-audit-fixes`

- Audit documentation suite.
- Portal password-visibility regression tests.
- Accounting audit regression tests for transfer direction, reversal date and missing COGS.
- Migration `020_020_reversal_period_integrity.up.sql` to keep reversal vouchers in the source period and reject changes to closed periods.
- Draft PR #65 is intentionally not ready for merge.

## Current blocker

GitHub Actions is failing before workflow steps execute, so these new regression tests have not been run by CI. For that reason the PR remains Draft and `main`/Production has not been changed by this audit branch.

## Release decision

**NOT READY for accounting sign-off.**

The existing double-entry core is promising, but P0 source-document, transfer, COGS, KPI, check-risk and credential issues must be corrected and regression-tested before management should rely on all financial dashboards/reports as authoritative.
