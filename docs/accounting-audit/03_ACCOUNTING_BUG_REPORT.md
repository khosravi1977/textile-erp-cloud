# 03 — Accounting Bug Report

Status legend: `VERIFIED` means reproduced from source/business logic. `LIVE-RETEST` means source defect is verified but production UI must still be exercised after deployment.

## ACC-001 — Financial Health profit KPIs omit COGS
- Severity: **HIGH**
- Priority: **P0**
- Module: سلامت مالی
- Type: Accounting Logic / Reporting
- Status: VERIFIED
- Evidence: `financial/web/src/App.jsx`, `buildFinancialHealth()`.
- Actual: `netProfit = salesTotal - expensesTotal`; sale `costAmount` / COGS is not subtracted. `ebitda = netProfit`. Product profitability allocates general expenses proportionally instead of using actual item COGS.
- Impact: net profit margin, EBITDA and management alerts can be materially overstated.
- Fix: derive management KPIs from the accounting ledger/P&L or at minimum include COGS separately and define EBITDA correctly.

## ACC-002 — Financial Health operating cash flow double-counts receipts
- Severity: **HIGH**
- Priority: **P0**
- Module: سلامت مالی
- Type: Accounting Logic / Reporting
- Status: VERIFIED
- Evidence: `operatingCashFlow = cashBalance + paidTotal - expensesTotal` while `cashBalance` already incorporates cash movements.
- Impact: cash-flow health and recommendations can be materially wrong.
- Fix: compute operating cash flow from classified cash movements over the requested period, never from ending cash balance plus receipts.

## ACC-003 — Current ratio/payables calculation omits purchase-credit liabilities
- Severity: **HIGH**
- Priority: **P0**
- Module: سلامت مالی
- Type: Accounting Logic / Reporting
- Status: VERIFIED
- Evidence: liabilities are based on open `payableDocs` plus opening payables, while unpaid/credit portions of incoming purchase invoices post to GL account 2100 but are omitted from the KPI.
- Impact: current ratio and debt/equity can look healthier than reality.
- Fix: source liabilities from ledger balances (AP + payable checks + other current liabilities).

## ACC-004 — Equity is forced to at least 1
- Severity: **HIGH**
- Priority: **P1**
- Module: سلامت مالی
- Type: Reporting
- Status: VERIFIED
- Evidence: `equity = Math.max(currentAssets - payables, 1)`.
- Impact: negative equity is hidden; debt/equity ratio becomes misleading.
- Fix: preserve negative/zero equity and explicitly handle undefined ratios.

## ACC-005 — Assigned receivable checks are counted as current receivables
- Severity: **HIGH**
- Priority: **P0**
- Module: سلامت مالی
- Type: Accounting Logic
- Status: VERIFIED
- Evidence: receivables add checks having `assignedTo`; ledger assignment actually credits check receivable and settles supplier payable.
- Impact: current assets and liquidity are overstated.
- Fix: exclude assigned checks from receivable assets; reconcile from ledger account 1210.

## ACC-006 — Receivables aging 31–60 bucket is mislabeled 61–90
- Severity: **MEDIUM**
- Priority: **P1**
- Module: گزارشات/اعتبارسنجی
- Type: Reporting / UX
- Status: VERIFIED
- Evidence: two buckets are labeled `سررسید گذشته ۶۱ تا ۹۰ روز`, one with `min:31,max:60`.
- Impact: aging report is confusing and potentially misleading.
- Fix: label the second bucket `۳۱ تا ۶۰ روز`.

## ACC-007 — Aging uses invoice date rather than due date
- Severity: **HIGH**
- Priority: **P1**
- Module: گزارشات/اعتبارسنجی
- Type: Accounting Logic / Reporting
- Status: VERIFIED
- Evidence: age is calculated from `row.date`; no true invoice due-date is used.
- Impact: invoices are presented as overdue based on age since issue, not contractual due date.
- Fix: add/derive explicit due date and age only overdue balances from due date.

## ACC-008 — Editing invoice number can duplicate invoice and orphan links
- Severity: **HIGH**
- Priority: **P0**
- Module: فاکتور مالی
- Type: Data Integrity / Business Logic
- Status: VERIFIED
- Evidence: edit keeps `editingNumber` but creates a new `id`; replacement/cleanup filters by the new invoice number. Duplicate check is skipped whenever editing.
- Impact: old invoice and linked documents can survive when number changes, creating duplicates/orphaned relationships.
- Fix: preserve stable invoice ID; treat original number as immutable or perform conflict-safe rename using original key; clean linked records by stable source ID.

## ACC-009 — Sale can be recorded with zero COGS
- Severity: **HIGH**
- Priority: **P0**
- Module: فاکتور مالی / انبار
- Type: Accounting Logic / Validation
- Status: VERIFIED
- Evidence: `pricingMode === 'sale'` exposes cost fields but save validation does not require positive `costUnitPrice`; accounting engine posts COGS only when `costAmount > 0`.
- Impact: revenue can be recognized without inventory relief/COGS, overstating inventory and profit.
- Fix: for owned-inventory sales require/derive valuation cost and stock availability before save. Regression test added.

## ACC-010 — Tax Excel taxable total is mathematically wrong
- Severity: **HIGH**
- Priority: **P1**
- Module: گزارش مالیاتی
- Type: Reporting
- Status: VERIFIED
- Evidence: export total uses gross `salesTotal + purchasesTotal + expensesTotal` for `taxable_amount`, including non-taxable records and amounts containing VAT; VAT total uses net payable VAT in a column containing per-row VAT.
- Impact: exported tax totals can disagree with row sums and official tax logic.
- Fix: export separate taxable bases, output VAT, input VAT and net payable VAT with mathematically consistent totals.

## ACC-011 — Non-financial/consignment incoming items are included in purchase totals
- Severity: **MEDIUM**
- Priority: **P1**
- Module: گزارش مالیاتی
- Type: Reporting
- Status: VERIFIED
- Evidence: `purchases` includes all incoming invoices and aggregate amount includes `nonFinancial` rows even though they are marked as no financial effect.
- Impact: purchase totals may include consignment/non-monetary entries.
- Fix: separate `خرید مالی` from `ورود غیرمالی/امانی` in all financial/tax totals.

## ACC-012 — Checks can be hard-deleted after accounting lifecycle events
- Severity: **HIGH**
- Priority: **P0**
- Module: اسناد دریافتی / اسناد پرداختی
- Type: Audit Trail / Data Integrity
- Status: VERIFIED
- Evidence: Docs table always exposes Delete; removal filters the row from workspace with no lifecycle restriction.
- Impact: cleared/paid/assigned/bounced document history can disappear from operational state; ledger later reverses but source audit trail is lost.
- Fix: replace delete with Void/Cancel for posted/linked checks; retain reason, user, timestamp and immutable history.

## ACC-013 — Financial invoices can be hard-deleted
- Severity: **HIGH**
- Priority: **P0**
- Module: فاکتور مالی
- Type: Audit Trail / Data Integrity
- Status: VERIFIED
- Evidence: invoice table exposes delete and `removeInvoice` physically removes invoice and linked workspace rows.
- Impact: posted source document disappears instead of being formally voided.
- Fix: add `voided` status, void reason/date/user and reversal; prohibit physical delete after posting.

## ACC-014 — Full financial reset is exposed in production UI
- Severity: **CRITICAL**
- Priority: **P0**
- Module: اطلاعات اولیه
- Type: Destructive Operation / UX / Data Integrity
- Status: VERIFIED
- Evidence: `resetAllFinance()` replaces workspace with `emptyFinance()` after browser confirmations.
- Impact: entire tenant financial workspace can be wiped from active state.
- Fix: remove from normal production UI; require maintenance mode, owner re-authentication, typed confirmation, verified backup, immutable audit record and preferably archive rather than erase.

## ACC-015 — Duplicate checks are allowed after confirmation
- Severity: **HIGH**
- Priority: **P0**
- Module: اسناد دریافتی / پرداختی
- Type: Validation
- Status: VERIFIED
- Evidence: duplicate check number/bank shows `window.confirm` and allows save if user approves.
- Impact: duplicate payable instruments may enter accounting and cash forecast.
- Fix: block duplicates using a strong composite key; for payable checks use bank/checkbook/check number as hard uniqueness.

## ACC-016 — Bank-to-bank transfer direction is inverted
- Severity: **HIGH**
- Priority: **P0**
- Module: بانک و صندوق
- Type: Accounting Logic
- Status: VERIFIED
- Evidence: UI identifies `counterAccountId` as transfer destination but transfer defaults to `direction='in'`; both UI balance logic and accounting engine debit `accountId` and credit `counterAccountId` for `in`.
- Expected A→B: debit B, credit A; decrease A, increase B.
- Actual: increase A, decrease B.
- Fix: normalize transfers to source→destination semantics (`direction='out'`) and derive the ledger independently from transfer source/destination. Regression test added.

## ACC-017 — Reversal voucher is dated today instead of source accounting date
- Severity: **HIGH**
- Priority: **P0**
- Module: دفاتر و تراز
- Type: Accounting Period Integrity
- Status: VERIFIED
- Evidence: `reverseLedgerEntry()` assigns `time.Now()` to reversal date.
- Impact: editing/removing an older transaction can distort monthly P&L and balances by putting reversal in current period while replacement remains in source period.
- Fix staged: migration `020_020_reversal_period_integrity.up.sql` forces reversal date to original voucher date and rejects reversal if that period is closed. Regression test added.

## ACC-018 — Bounced/returned receivable checks improve credit exposure calculation
- Severity: **HIGH**
- Priority: **P0**
- Module: اعتبارسنجی
- Type: Risk Logic
- Status: VERIFIED
- Evidence: future-check filtering excludes only `cleared` and `assigned`, so `bounced` and `returned` remain collectible in exposure/score logic.
- Impact: customer creditworthiness can be overstated precisely because a bad check is counted as future collection.
- Fix: only collectible states may reduce exposure; bounced/returned must increase risk and remain customer receivable.

## ACC-019 — Advisor counts bounced/returned checks as expected liquidity
- Severity: **HIGH**
- Priority: **P0**
- Module: تحلیل و مشاور هوشمند
- Type: Risk / Forecasting Logic
- Status: VERIFIED
- Evidence: due/future receivables filters also exclude only cleared/assigned.
- Impact: liquidity forecast and AI recommendations can overstate expected cash receipts.
- Fix: use a shared `isCollectibleReceivableCheck()` policy and ledger-backed cash forecast.

## ACC-020 — Payment-term percentages can exceed 100%
- Severity: **MEDIUM**
- Priority: **P1**
- Module: فاکتور مالی / برنامه وصول
- Type: Validation / Forecasting
- Status: VERIFIED
- Evidence: changing cash percent auto-adjusts check percent, but changing check percent directly does not normalize cash; e.g. 30% cash + 90% check is possible.
- Impact: expected cash/check schedule can exceed invoice debt and corrupt forecasts.
- Fix: validate cash+check=100 (or explicitly model other terms) before save.

## SEC-001 — Stored employee passwords are recoverable/displayed
- Severity: **HIGH SECURITY**
- Priority: **P0**
- Module: مدیریت کاربران
- Type: Authentication / Credential Handling
- Status: VERIFIED; FIX TEST STAGED
- Evidence: portal list APIs recover encrypted password and UI displays/copies it; screenshot confirmed plaintext employee credentials.
- Impact: anyone with team-management page access can recover other users' current passwords.
- Fix: passwords must be non-recoverable after creation; return a newly issued password once only. Regression tests added in `portal_server/password_visibility_test.go`. The source patch remains staged because GitHub Actions currently fails before executing jobs.

## PERM-001 — Human owner/service-account responsibilities are conflated
- Severity: **HIGH SECURITY / ARCHITECTURE**
- Priority: **P0**
- Module: Portal / Operational SSO
- Status: VERIFIED from production incident + source model; LIVE-RETEST required
- Impact: an integration/service identity may gain manager/admin privileges, and a human owner may be downgraded during synchronization.
- Fix: maintain a dedicated human `owner` identity and a separate least-privilege Business Brain service identity. Portal is the authority for permissions; operational SSO should map roles consistently instead of manual DB patching.
