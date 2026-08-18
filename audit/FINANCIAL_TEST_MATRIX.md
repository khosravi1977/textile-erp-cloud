# Financial Test Matrix

Status legend:
- **CODE PASS**: implementation and regression condition are present in the branch.
- **TARGETED PASS**: additionally exercised in the available isolated execution environment.
- **LIVE BLOCKED**: production E2E could not be executed because authenticated production runtime access was unavailable.
- **OPEN**: a remaining issue/limitation is documented and not falsely marked complete.

| Area | Scenario | Expected accounting effect | Branch status | Live status |
|---|---|---|---|---|
| RBAC | Missing/empty permissions for non-owner | No implicit full access | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| RBAC | Owner without explicit permission list | Owner retains intended full access | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Portal | Existing stored password in list/read | Password omitted, never decrypted for response | CODE PASS | LIVE BLOCKED |
| Portal | Explicit new/reset password | May be returned one time only | CODE PASS | LIVE BLOCKED |
| Portal/Operational | Role propagation | owner→admin, manager→manager, accountant→accountant, viewer→viewer | CODE PASS | LIVE BLOCKED |
| Sales | Taxable sale | Dr receivable / Cr revenue net of VAT / Cr output VAT | CODE PASS | LIVE BLOCKED |
| Sales | COGS | Dr COGS / Cr inventory when cost exists | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Sales | Credit settlement | Receivable remains for credit portion | CODE PASS | LIVE BLOCKED |
| Sales | Cash/bank settlement | Dr cash/bank / Cr receivable | CODE PASS | LIVE BLOCKED |
| Sales | Check receipt | Dr checks receivable / Cr receivable | CODE PASS | LIVE BLOCKED |
| Purchase | Financial incoming invoice | Dr inventory/expense + input VAT / Cr payable | CODE PASS | LIVE BLOCKED |
| Purchase | Non-financial operational incoming | Excluded from financial purchase KPI/tax purchase set | CODE PASS | LIVE BLOCKED |
| Purchase | Cash/bank payment | Dr payable / Cr cash/bank | CODE PASS | LIVE BLOCKED |
| Purchase | Assigned receivable check | Dr payable / Cr receivable-check asset | CODE PASS | LIVE BLOCKED |
| Purchase | Same receivable check assigned twice | API rejects duplicate settlement reference | CODE PASS | LIVE BLOCKED |
| Purchase | Assigned check supplier/amount mismatch | API rejects inconsistent assignment | CODE PASS | LIVE BLOCKED |
| Yarn out | Owned yarn sale/barter | Revenue + COGS/inventory reduction | CODE PASS | LIVE BLOCKED |
| Yarn out | Amanat yarn | No owned-inventory COGS recognition | CODE PASS | LIVE BLOCKED |
| Expense | Manual financial expense | Dr operating expense / Cr selected bank/cash | CODE PASS | LIVE BLOCKED |
| Bank/Cash | Internal transfer | Source decreases, destination increases, total liquidity unchanged | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Bank/Cash | Transfer alerts | Valid two-sided transfer does not create false negative-balance alert | CODE PASS | LIVE BLOCKED |
| Bank/Cash | Other income/capital/customer receipt/supplier payment | Counter-account chosen by transaction nature | CODE PASS | LIVE BLOCKED |
| Checks | Receivable open | Remains receivable-check asset | CODE PASS | LIVE BLOCKED |
| Checks | Receivable assigned | Removed from receivable-check asset | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Checks | Assign cleared/bounced/returned check | API rejects non-open assignment | CODE PASS | LIVE BLOCKED |
| Checks | New check created directly assigned | API rejects lifecycle bypass | CODE PASS | LIVE BLOCKED |
| Checks | Manual assignment to unknown party | API rejects; known supplier/payable party required | CODE PASS | LIVE BLOCKED |
| Checks | Receivable cleared | Dr bank / Cr receivable-check asset | CODE PASS | LIVE BLOCKED |
| Checks | Receivable returned/bounced | Recreate customer receivable / remove check asset | CODE PASS | LIVE BLOCKED |
| Checks | Assigned/cleared overdue alerts | No ordinary overdue alert | CODE PASS | LIVE BLOCKED |
| Checks | Bounced/returned alert | Dedicated critical returned-check alert | CODE PASS | LIVE BLOCKED |
| Checks | Payable paid | Dr checks payable / Cr bank | CODE PASS | LIVE BLOCKED |
| Checks | Payable returned/bounced | Remove checks payable / recreate supplier payable | CODE PASS | LIVE BLOCKED |
| Accounting | Manual journal | Minimum two lines, exactly one debit/credit side per line, balanced total | CODE PASS | LIVE BLOCKED |
| Accounting | Edit historical source | Reversal stays in original accounting date/period | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Accounting | Closed period posting/change | Posting/change rejected for closed fiscal period | CODE PASS | LIVE BLOCKED |
| Accounting | Reopen closed period | Normal API rejects Closed→Open; adjustment must use open period | CODE PASS | LIVE BLOCKED |
| Accounting | Duplicate invoice/check identifiers | Duplicate controls present | CODE PASS | LIVE BLOCKED |
| Dashboard | Cash balance | Includes both sides of transfer | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Dashboard | Gross margin | Revenue net of output VAT minus COGS, not purchases | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Financial Health | Product margin | Uses item revenue and actual recorded costAmount | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Financial Health | DSO/DIO/DPO | Uses revenue/COGS/purchases rather than general expense proxy | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Financial Health | Budget variance | No fabricated variance without persisted approved budget | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Financial Health | EBITDA label | No longer labels operating-profit proxy as EBITDA | CODE PASS | LIVE BLOCKED |
| Tax | Output VAT | Excluded from management revenue; retained as tax liability/report VAT | CODE PASS / TARGETED PASS | LIVE BLOCKED |
| Tax | Non-financial incoming | Excluded from tax purchase population | CODE PASS | LIVE BLOCKED |
| Credit | Open receivable checks | Added to exposure, not subtracted twice from invoice debt | CODE PASS | LIVE BLOCKED |
| Credit | Assigned/cleared/returned checks | Only genuinely open checks alter current check-exposure calculation | CODE PASS | LIVE BLOCKED |
| Advisor legacy API | Missing real credit profile | Fails honestly instead of returning hard-coded risk/profile | CODE PASS | LIVE BLOCKED |
| Profitability legacy API | Missing revenue input | Rejects request instead of using fixed fabricated revenue | CODE PASS | LIVE BLOCKED |
| Reports | Trial balance / GL / P&L / BS | Derived from posted vouchers and balanced entries | CODE REVIEWED | LIVE BLOCKED |
| Reports | PDF/Excel visual totals | Requires authenticated rendered UI and downloaded files | OPEN | LIVE BLOCKED |
| Operational bridge | Tenant isolation | Company ID propagated through portal/API bridge | CODE REVIEWED | LIVE BLOCKED |
| Master data | Party classification in auto-created GL party | Supplier/customer role should remain semantically correct | OPEN — see BUG_REPORT | LIVE BLOCKED |

## Production E2E cases still required when runtime access becomes available
Use only `AGENT_TEST_` records and reconcile each case across UI, workspace/API response, persisted DB row, derived journal voucher/lines, trial balance, general ledger, dashboard, and exported report:
1. taxable sale: cash / credit / check / mixed settlement;
2. taxable purchase: cash / credit / payable check / assigned receivable check;
3. internal transfer between two bank/cash accounts;
4. receivable check lifecycle open → assigned/cleared/returned/bounced plus duplicate/invalid assignment rejection;
5. payable check lifecycle open → paid/returned/bounced;
6. expense create/edit/delete and linked movement reversal;
7. owned yarn sale with COGS and amanat yarn without COGS;
8. historical edit in an open period, attempted edit in a closed period, and attempted Closed→Open transition;
9. owner/manager/accountant/viewer UI/API permission matrix;
10. PDF/Excel totals against source report totals.
