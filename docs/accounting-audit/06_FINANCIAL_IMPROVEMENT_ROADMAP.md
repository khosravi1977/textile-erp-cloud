# 06 — Financial Improvement Roadmap

## P0 — before trusting Production accounting reports

1. Fix bank transfer source/destination direction end-to-end.
2. Require/derive COGS for owned-inventory sales.
3. Replace hard delete of posted invoices/checks with void/reversal workflows.
4. Remove/lock full financial reset from normal Production UI.
5. Fix Financial Health KPIs to use ledger-backed P&L, balance-sheet and cash-flow data.
6. Exclude bounced/returned checks from collectible liquidity and credit exposure.
7. Remove stored-password recovery/display from user-management APIs/UI.
8. Separate human owner identity from Viora Business Brain service identity.
9. Enforce period-correct reversals; staged DB guard: migration 020.

## P1 — reporting/accounting correctness

1. Correct aging 31–60 label and calculate overdue age from due date.
2. Correct tax-report taxable base/output VAT/input VAT/net VAT totals.
3. Exclude or separately classify non-financial consignment incoming records.
4. Include purchase-credit AP in liquidity/current-ratio liabilities.
5. Exclude assigned receivable checks from receivable current assets.
6. Preserve stable invoice IDs and safe invoice-number edits.
7. Validate payment-term percentages.
8. Add strong uniqueness keys for payable/receivable checks.

## P2 — professional ERP controls

1. Ledger-backed KPI service shared by dashboard, health, reports, advisor and Telegram.
2. Shared check-status policy module.
3. Posting preview and source↔voucher drill-down.
4. Full immutable document-history/audit log.
5. Period-close wizard with preflight reconciliation.
6. Automated end-to-end accounting regression suite using `AGENT_TEST_` fixtures.

## Release gate

Do not mark this audit complete until:

- all P0 accounting regression tests are green;
- Production smoke test passes on all 20 financial pages;
- one test sale and one test purchase reconcile source → settlement → ledger → trial balance → report;
- bank transfer test preserves total cash and moves balance in the correct direction;
- posted invoice/check cannot be physically deleted;
- financial/tax KPIs reconcile to ledger data;
- credentials are no longer recoverable after user creation.
