# 02 — Accounting Test Matrix

This matrix records what can be verified from source/tests now and what still requires live browser execution against the `paregol` tenant.

| Area | Source/Logic | Existing Automated Guard | Audit Status | Live Retest |
|---|---|---|---|---|
| Dashboard | Reviewed | Partial | PARTIAL | Required |
| Financial Health | Reviewed deeply | No reliable KPI oracle | **FAIL** | Required after fix |
| Initial Data | Reviewed | Partial | **FAIL — destructive reset risk** | Required |
| Operational Bridge | Structure reviewed | Separate operational tests | PARTIAL | Required |
| Incoming Invoices | Accounting derivation reviewed | Go accounting tests | PARTIAL | Required CRUD/validation |
| Chelle Incoming | Structure reviewed | Partial | PARTIAL | Required |
| Yarn Out | Accounting + COGS validation reviewed | Go tests include owned-yarn COGS | PASS/PARTIAL | Required UI workflow |
| Financial Invoice | Save/edit/delete/accounting reviewed | Go ledger sale test | **FAIL** | Required after fix |
| Inventory | Ledger relationship reviewed | Partial | PARTIAL | Required quantity/valuation |
| Costs | Expense posting reviewed | Partial | PARTIAL | Required CRUD |
| Receivable Docs | Check lifecycle reviewed | Bounce/return tests exist | **FAIL — hard delete/credit forecast** | Required |
| Payable Docs | Check lifecycle reviewed | Return test exists | **FAIL — hard delete/duplicate** | Required |
| Bank & Cash | Balance/ledger transfer logic reviewed | Audit regression test staged | **FAIL — transfer direction** | Required after fix |
| Accounting / Ledger | Deep source + DB constraints reviewed | Strong Go + PostgreSQL guards | **FAIL/PASS MIXED** | Reversal period retest |
| Reports | Selected formulas reviewed | Partial | PARTIAL | Required numerical reconciliation |
| Tax Reports | Export formulas reviewed | No sufficient total reconciliation test | **FAIL** | Required after fix |
| Credit | Exposure formula reviewed | Partial | **FAIL** | Required after fix |
| AI Advisor | Liquidity filters reviewed | Partial | **FAIL** | Required after fix |
| Telegram Reports | Not yet functionally exercised | Relay tests separate | BLOCKED | Live/service access needed |
| Mobile App Bridge | Source integration visible | Mobile bridge migration exists | PARTIAL | Device workflow required |

## End-to-end scenarios to execute live

1. `AGENT_TEST_CUSTOMER_001` → sale invoice → partial/full settlement → customer balance → bank/cash → journal → GL → trial balance → dashboard/report.
2. `AGENT_TEST_SUPPLIER_001` → purchase invoice → AP → partial/full payment → inventory → input VAT → journal → supplier statement.
3. Bank A → Bank B transfer of a unique test amount; confirm A decreases, B increases and voucher is Dr B / Cr A.
4. Receivable check lifecycle: open → cleared; separate test: open → bounced; verify customer receivable reopens.
5. Payable check lifecycle: issued → paid; separate test: returned; verify supplier payable reopens.
6. Edit a test invoice in the same open fiscal period; verify stable identity, linked rows and period-correct reversal/reposting.
7. Attempt duplicate invoice/check numbers; system must block according to accounting uniqueness policy.
8. Attempt sale with owned inventory and zero COGS; system must block or derive valuation automatically.
9. Tax-report reconciliation: row taxable bases and VAT totals must equal exported totals.
10. Full ledger invariant: for every posted voucher, total debit = total credit.

## Safety policy

Live tests must use the `AGENT_TEST_` prefix. Real customer documents must not be edited, deleted or voided. Destructive reset is not to be executed in Production.
