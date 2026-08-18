# 05 — UX & Workflow Recommendations

## P0 — prevent irreversible/accounting-dangerous actions

- Replace `Delete` on posted invoices/checks with `ابطال / Void` and require reason.
- Remove `پاک کردن کل اطلاعات مالی` from normal Production navigation; keep it behind controlled maintenance mode only.
- Block duplicate payable checks rather than allowing user override.
- Make bank transfer fields explicitly `حساب مبدأ` and `حساب مقصد`; remove ambiguous direction for transfers.
- Never show current passwords in team management. Use `تغییر رمز` / `صدور رمز جدید`.

## P1 — reduce accounting mistakes

- Stable immutable document IDs; document number changes must not change identity.
- Show a permanent posting state: Draft / Posted / Voided / Reversed.
- Disable edits to Posted documents unless a formal correction flow is used.
- Add due date explicitly to invoices and use it in aging.
- Validate payment-term percentages and show `جمع درصدها = 100%`.
- For owned inventory sales, display valuation source and COGS before save.
- Add a preview panel before posting: Debit / Credit / Account / Amount.
- Show related-record links on every document: invoice ↔ settlement ↔ check ↔ voucher ↔ ledger.

## P1 — Persian accounting usability

- Consistent thousand separators for all monetary inputs and tables.
- Accept Persian/Arabic/Latin digits but normalize to a single numeric representation.
- Display both operational status and accounting status where relevant.
- Add keyboard navigation and searchable autocomplete for customers/accounts/items.
- Prevent double submit by disabling Save immediately and using idempotency keys.

## P2 — professional auditability

- Add document history drawer: who created, edited, voided, posted, reopened and when.
- Add `Why this number?` drill-down on every KPI to show ledger accounts/transactions forming the value.
- Add reconciliation badges: Source = Ledger = Report.
- Add period-close checklist and exception list before closing a fiscal period.
- Add a dedicated Audit Log page with filters by user, document, date, action and tenant.

## Role/permission UX

- Human company owner should be a distinct `owner` identity.
- Integration/service identities should be clearly labelled `Service Account` and should not share human credentials.
- Central Portal should be the permission authority; downstream modules should consume mapped roles/menu permissions, not require manual DB role patches.
