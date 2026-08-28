# Report Consistency Report

## Objective
Ensure that dashboard/management/tax/accounting reports use accounting-compatible definitions rather than visually plausible but inconsistent formulas.

## Corrected consistency rules

### Sales and revenue
- Customer-facing invoice total remains gross including VAT.
- Management/accounting revenue excludes output VAT.
- Yarn financial sale/barter revenue is included separately.
- Output VAT remains tax liability/report data, not income.

### Gross margin and operating profit
Before the audit, one management summary effectively used purchases and expenses as immediate deductions from sales. The audited implementation separates:
- Purchases: acquisition KPI
- COGS: cost attached to sold owned goods
- Operating expenses: period expense

Therefore management gross margin now follows `net revenue - COGS`, and operating profit follows `gross margin - operating expenses`.

### Cash and bank
Internal transfers are one economic movement with two account effects. Both backend management summary and audited frontend health calculation now preserve total liquidity while changing the two account balances.

### Receivable checks
- `open`/collectible and `bounced` claims remain exposure/assets according to their accounting lifecycle.
- `cleared` checks are no longer receivable-check assets.
- `assigned` checks are no longer receivable-check assets after supplier assignment.

### Financial Health
The audited calculation removes synthetic values:
- no invented budget-sales target;
- no invented budget-profit target;
- no fixed -12%/-8% variance;
- no general-expense pro-rata allocation presented as product COGS;
- no net-profit proxy presented as EBITDA.

The UI now labels the available KPI as recorded operating profit and tells the user that approved budget data is required for budget variance.

### Working-capital metrics
The audited health calculation uses:
- DSO: receivable exposure relative to net revenue over the observed period;
- DIO: inventory relative to COGS, not general expenses;
- DPO: payable exposure relative to financial purchases;
- CCC: DSO + DIO - DPO.

These remain management indicators and require sufficient period data to be decision-useful.

### Aging
The audited calculation:
- uses due date when available, falling back to document date only when necessary;
- has distinct 1–30, 31–60, 61–90 and >90 day buckets;
- removes the former duplicate 61–90 label.

### Tax report
The production build transform excludes `nonFinancial` incoming records from the tax purchase population. Sales/purchase tax amount remains separated from gross totals. Official submission requirements remain outside this control report unless the required official identifiers/signature/integration are configured.

### Credit report
The audit corrected current exposure logic so an open check accepted against an invoice is not subtracted a second time from already-reduced invoice debt. Only genuinely `open` checks are treated as current check exposure in this calculation; assigned/cleared/returned/bounced records are not treated as healthy future collection collateral.

### Legacy API reports
Legacy credit/advisor endpoints previously generated reports from hard-coded credit data, and profitability could silently use fixed revenue. These endpoints now fail explicitly when real inputs/source data are absent instead of generating a false report.

## Accounting reports
The General Ledger / Trial Balance / P&L / Balance Sheet handler reads posted journal vouchers/lines and validates fiscal periods. Code review confirms the reports share the journal source rather than independently reconstructing every business event.

## Export/PDF limitation
Rendered PDF/Excel files require an authenticated live UI/browser execution and downloaded output for final visual and numeric reconciliation. That environment was not available during this run, so export consistency is **not marked PASS**. The required live matrix remains in `FINANCIAL_TEST_MATRIX.md`.

## Overall consistency status
- Code-level KPI/report inconsistencies identified in this audit: fixed on branch, except the documented party-type master-data issue.
- Targeted accounting math/invariant checks: passed in isolated local validation.
- Full repository CI: blocked before test steps by GitHub Actions infrastructure.
- Live production report reconciliation: blocked by unavailable authenticated production runtime access.
