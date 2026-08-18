# UX Improvement Report

## Improvements implemented on the audit branch

### 1. Fail-closed financial navigation
A non-owner with missing/empty/invalid permission claims no longer receives the complete financial page set by default. Owner/admin fallback remains available for intended full-access accounts.

**UX effect:** fewer confusing/unauthorized tabs and safer behavior when session metadata is incomplete.

### 2. Truthful management labels
The former `EBITDA` label was backed by a net/operating-profit proxy without depreciation/amortization data. Production financial build now labels the KPI as recorded operating profit.

The budget variance section now explicitly says an approved budget is required instead of presenting synthetic targets/variances.

**UX effect:** the screen no longer overstates the accounting meaning of a number.

### 3. Safer credential management wording
The hardened portal path no longer encourages recovery/copy of an already stored password. Existing stored credentials are treated as non-displayable; only a newly created/reset secret can be shown one time.

**UX effect:** security behavior matches common password-management expectations and reduces accidental credential exposure.

### 4. Tax report population
Operational rows marked non-financial are filtered from the financial tax purchase set.

**UX effect:** users are less likely to see operational-only rows mixed into a report that looks tax-ready.

### 5. Credit exposure semantics
Only open receivable checks are treated as current check exposure. The calculation no longer subtracts an accepted check twice from invoice debt.

**UX effect:** credit score/reasons align more closely with the actual collection risk visible to the finance user.

### 6. Financial Health explanatory behavior
The audited helper reports grounded narrative based on available data and explicitly states when budget data is missing rather than inventing a benchmark.

## Recommended next UX improvements after live E2E becomes available
1. Add an explicit data-quality badge to every advanced KPI: `Complete`, `Estimated`, or `Insufficient data`.
2. Show the accounting source link beside management KPIs (for example, click gross margin → journal/report drill-down).
3. Add a reconciliation panel: `Dashboard total = Trial balance source = Report total`, with green/red difference indicators.
4. For destructive financial edits, show the generated reversal/reposting impact before confirmation.
5. For check lifecycle actions, show a small accounting preview such as `Dr Bank / Cr Receivable Checks`.
6. Add a typed party selector (`Customer`, `Supplier`, `Contractor`, `Internal`) instead of relying on a free-text party name where accounting classification matters.
7. Add a real approved-budget entry/import module before enabling budget variance and forecast-vs-actual charts.
8. Add an export verification footer containing period, company, generation timestamp, report revision and total checksum to PDF/Excel outputs.
9. Expose a read-only audit trail for workspace revision, actor and generated journal voucher IDs.
10. In credit scoring, display separately: invoice debt, open checks, bounced checks, collateral/barter value and final net exposure so the score is explainable.

## Accessibility / localization notes
- Persian labels are the primary UI language in the audited financial surface.
- Numbers and Jalali formatting utilities exist in the frontend.
- Live browser validation for RTL overflow, mobile widths, keyboard focus, print layout and Excel/PDF Persian rendering could not be executed in this run and remains part of the live matrix.
