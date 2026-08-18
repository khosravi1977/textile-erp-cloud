# Accounting Integrity Report

## Double-entry engine
The workspace accounting engine derives journal entries from business state and rejects unbalanced entries. The audit verified the intended source types and account directions at code level.

### Sales invoice
- Debit: Accounts Receivable — gross invoice total
- Credit: Sales Revenue — invoice total less output VAT
- Credit: Output VAT — tax amount
- If COGS exists: Debit COGS / Credit Inventory
- Cash/check/barter settlements create separate settlement entries reducing receivable.

### Purchase/incoming invoice
- Debit: Inventory/expense account — net of input VAT
- Debit: Input VAT — when present
- Credit: Accounts Payable — gross invoice total
- Settlement reduces payable against bank/cash/check-payable/assigned-receivable-check/barter asset.
- `nonFinancial` incoming rows are not posted as financial purchases.

### Yarn-out financial flow
- Sale/barter produces financial revenue.
- Owned yarn with cost produces COGS and inventory reduction.
- Amanat stock does not recognize owned-inventory COGS.

### Expense
- Debit: Operating Expense
- Credit: selected bank/cash account
- The UI links the expense and movement so edit/delete can identify the dependent movement.

### Bank/cash movements
- Customer receipt: cash/bank versus receivable
- Supplier payment: payable versus cash/bank
- Expense: expense versus cash/bank
- Other income: cash/bank versus other income
- Capital: cash/bank versus opening/equity
- Transfer: source cash account versus destination cash account

**Audit correction:** management cash totals and alerts now apply both sides of a transfer. Total company liquidity remains unchanged by an internal transfer and a valid transfer does not create a false negative-account alert.

### Receivable check lifecycle
- Receipt: receivable-check asset versus customer receivable
- Assignment to supplier: supplier payable versus receivable-check asset
- Clearance: bank versus receivable-check asset
- Return/bounce: customer receivable versus receivable-check asset

**Audit corrections:**
- assigned and cleared checks are excluded from open receivable-check assets;
- assigned/cleared checks are not treated as ordinary overdue receivables;
- bounced/returned checks receive a dedicated critical alert;
- `/api/workspace` validates lifecycle transitions against the previous state before persistence;
- only an existing open check can be assigned;
- one check cannot settle multiple purchases;
- purchase link, supplier and assigned amount must match the check record;
- a new receivable check cannot be created directly in assigned state;
- manual assignment requires a known supplier/payable party.

### Payable check lifecycle
- Issue: supplier payable versus check payable
- Payment: check payable versus bank
- Return/bounce: check payable versus supplier payable
- Paid checks are excluded from ordinary overdue alerts; returned/bounced states are surfaced explicitly.

## Validation controls observed or added
- Sales/purchase amount must be positive.
- Required party/date/item controls exist for financial source rows.
- Sale/purchase settlement must equal document total.
- Cash settlement requires account.
- Check settlement requires check number and due date.
- Assigned receivable-check settlement requires a valid document ID and lifecycle-consistent linkage.
- Owned yarn financial out requires COGS data.
- Expense requires payment account.
- Check numbers are protected against duplicates within their register.
- Manual journals require at least two valid lines and equal debit/credit totals.
- Business row uniqueness is checked for invoices/incoming/yarn-out.
- Closed fiscal-period posting/change is rejected.
- A closed fiscal period cannot be reopened through the normal ERP API.

## Reversal/edit and period integrity
A source edit produces a reversal for the prior derived entry and a new corrected entry. The audit found that reversals had been dated today, which could corrupt period results. The branch fixes this so reversal date remains the original source date. It also makes fiscal closing irreversible through the ordinary API. Therefore:
- an edit in an open historical period reverses/reposts in that period;
- an attempted change in a closed period is blocked by the period lock;
- `Closed → Open` is rejected; a later correction must use a controlled adjustment in an open period;
- current-period profit is not contaminated by correcting an older source transaction.

## Management KPI reconciliation rules added
- Customer invoice gross amount and accounting revenue are separated.
- Output VAT is not revenue.
- Purchases are not automatically COGS.
- COGS comes from recorded `costAmount` on sold owned inventory.
- Operating profit = revenue net of output VAT + yarn financial revenue - COGS - operating expenses.
- Internal transfer does not change total liquidity.
- Assigned/cleared receivable checks are not retained as open check assets.
- Alert logic uses the same transfer/check lifecycle semantics as the accounting view.
- No budget variance is reported without an approved budget source.

## Remaining accounting data-model issue
`ensureLedgerParty` auto-creates every unknown party with party type Customer. This does not break debit=credit, but can misclassify supplier/contractor master data. It is intentionally left open pending a typed source-of-truth/migration decision because changing historical master data without production reconciliation would be unsafe.

## Verification level
- Source accounting paths: reviewed.
- Highest-risk invariants (transfer, COGS margin, reversal period): targeted isolated validation passed.
- Lifecycle/fiscal-period/check-alert protections: regression tests added to the repository branch.
- Repository-wide CI: blocked before recorded steps by GitHub Actions runner/account infrastructure.
- Live production journal/DB reconciliation: not executed because authenticated production runtime access was unavailable.
