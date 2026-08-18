# 04 — Accounting Integrity Report

## Executive conclusion

The ledger core has meaningful protections, but several workspace/UI rules can feed misleading management reports or destroy the audit trail. The highest-risk areas are destructive deletion/reset, bank-transfer direction, missing COGS validation for ordinary sales, period placement of reversals, and risk/liquidity calculations that treat bad checks as collectible.

## Controls that are already strong

### Double-entry derivation
`deriveWorkspaceLedger()` rounds lines and rejects any derived voucher where debit and credit totals differ.

### Manual journal validation
Manual journals require at least two lines, one-sided positive debit/credit lines and equal total debit/credit.

### Posted-voucher database hardening
Migration 016 adds database-level protection for accounting lines/vouchers, tenant-safe uniqueness, immutability of posted vouchers and balance requirements before/when posting.

### Fiscal-period lock
`insertLedgerEntry()` checks whether the voucher date falls inside a Closed fiscal period and refuses posting into it.

### Idempotent workspace posting
Workspace vouchers use deterministic external keys containing revision/direction/source/hash components and database uniqueness to avoid duplicate insert on retry.

### Check lifecycle accounting
The accounting engine explicitly derives entries for cleared/paid/bounced/returned checks. Existing tests verify that bounced receivable checks reopen customer receivable and returned payable checks reopen supplier payable.

## Integrity gaps

### 1. Source-document audit trail is weaker than the ledger
Workspace invoices/checks can be hard-deleted while posted vouchers remain immutable. The synchronization engine responds by posting reversals, but the business source disappears from the active workspace. This is not sufficient for a financial audit trail.

Required invariant: `Posted source documents are never physically deleted; they are voided with immutable metadata and reversal linkage.`

### 2. Period integrity of reversals
Current Go logic dates reversals with the current day. That can move the reversal into another month. A database guard has been staged in migration 020 to keep reversal date aligned with the original voucher and reject reversals in a closed period.

Required invariant: `Correction policy must be explicit: same-period reversal when open; formal adjustment when closed.`

### 3. COGS consistency
Owned-yarn sale flow enforces COGS, while ordinary financial invoice sale flow does not. The ledger therefore may contain revenue without the associated inventory/COGS voucher.

Required invariant: `Any owned-inventory sale must either derive valuation automatically or reject zero/unknown COGS.`

### 4. Transfer semantics
The system has two concepts — movement `direction` and explicit source/destination accounts — but the transfer UI currently combines them inconsistently. For a transfer source A to destination B, accounting must always be Debit B / Credit A and total cash across both accounts must remain unchanged.

Required invariant: `Transfer source and destination define accounting direction; generic receipt/payment direction must not invert it.`

### 5. Management KPIs are not ledger-backed
Financial Health calculations are built from workspace arrays with bespoke formulas. Several formulas diverge from the ledger (COGS, assigned checks, AP, operating cash flow). This creates two competing sources of truth.

Required invariant: `Official financial KPIs and management health ratios must be derived from ledger balances/period movements, with reconciliations to source documents.`

### 6. Credit/liquidity state policy is fragmented
Different pages each hand-code check status filters. Bounced/returned checks can be counted as future liquidity in credit/advisor formulas although the ledger correctly reopens customer receivable.

Required invariant: `One shared status policy defines collectible, cleared, assigned, bounced and returned effects for all reports/advisors.`

## Accounting invariants to enforce before release

- Every posted voucher: total debit = total credit.
- Every source document has a stable immutable ID.
- Every void/correction retains source history and reversal link.
- Owned sale: Revenue and COGS/inventory effects are both present.
- Purchase credit: AP is included in liabilities and current ratio.
- Assigned check: removed from receivable asset.
- Bounced/returned check: never counted as expected cash; receivable/payable reopens as appropriate.
- Bank transfer: net total cash change = 0; source decreases; destination increases.
- Closed fiscal period: no silent edit/delete/reversal into that period.
- Tax totals: exported totals reconcile mathematically to filtered taxable rows.
