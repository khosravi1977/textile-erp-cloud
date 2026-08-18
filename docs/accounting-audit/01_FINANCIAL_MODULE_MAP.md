# 01 — Financial Module Map

Audit target: Textile ERP / tenant `paregol`

Source-derived financial pages:

1. `dashboard` — داشبورد
2. `financialHealth` — سلامت مالی
3. `initialData` — اطلاعات اولیه
4. `operational` — داده‌های عملیاتی
5. `incomingInvoices` — فاکتور ورود
6. `chelleIncomingInvoices` — ورود چله
7. `yarnOutInvoices` — خروج نخ
8. `invoices` — فاکتور مالی
9. `inventory` — انبار و نخ
10. `costs` — هزینه‌ها
11. `receivableDocs` — اسناد دریافتی
12. `payableDocs` — اسناد پرداختی
13. `bankCash` — بانک و صندوق
14. `accounting` — دفاتر و تراز
15. `reports` — گزارشات
16. `taxReports` — گزارش مالیاتی
17. `credit` — اعتبارسنجی
18. `advisor` — تحلیل و مشاور هوشمند
19. `telegramReports` — گزارش‌های تلگرام
20. `mobileApp` — اتصال به اپ حسابیار

Nested operational bridge tabs discovered in the financial UI:

- فاکتور خروج
- مشتریان
- کالاها
- نخ‌ها
- ورود نخ
- خروج نخ
- ورود قطعات
- موجودی قطعات
- هزینه‌های عملیاتی

## Accounting flow under audit

Sales / Purchases / Expenses / Checks / Cash movements
→ Financial workspace state
→ accounting validation
→ derived double-entry ledger
→ journal vouchers and lines
→ general ledger / trial balance / reports

The audit must treat these as one connected workflow, not independent screens.
