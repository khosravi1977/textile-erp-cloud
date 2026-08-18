# 07 — Tested Records & Evidence

## Production-visible evidence already observed

- Landing portal for company `paregol` initially did not show the team-management card.
- Direct `/team` access succeeded and showed the current manager plus other company users, proving team-management authorization was active.
- After the server-side role/access repair and portal restart performed by the cooperating server agent, the landing page showed `مدیریت کاربران و دسترسی‌ها` again.
- The team-management page visibly exposed employee plaintext passwords; this is recorded as SEC-001.

## Source-level scenarios executed/reviewed

### Balanced sale derivation
Existing automated test verifies a sale with cash + credit settlement derives balanced sale/receipt vouchers.

### Purchase with assigned receivable check
Existing test verifies payable debit and check-receivable credit on assignment settlement.

### Purchase VAT
Existing test verifies purchase base, input VAT and total AP are separated correctly.

### Owned yarn sale COGS
Existing test verifies COGS debit / yarn inventory credit for owned yarn.

### Bounced receivable check
Existing test verifies customer receivable reopens and active check assignment is removed.

### Returned payable check
Existing test verifies supplier payable reopens.

### Fiscal-period authorization
Existing test verifies a reports-only user cannot mutate accounting periods.

## Audit regression records added

- `TestAuditTransferPostsFromSourceToDestination`
  - Test data: source bank, destination bank, amount 1,000.
  - Expected: Dr destination / Cr source.
  - Current code expected to fail until transfer semantics are fixed.

- `TestAuditReversalKeepsOriginalAccountingDate`
  - Test source date: 2026-07-31.
  - Expected: reversal remains 2026-07-31.
  - Current Go code expected to fail; DB migration 020 stages period-level enforcement.

- `TestAuditSaleWithInventoryCostCannotOmitCOGS`
  - Test sale: owned inventory, total 10,000, cost amount 0.
  - Expected: reject save/accounting state.
  - Current code expected to fail until sale validation is hardened.

- Portal credential tests:
  - stored password must not be recovered into metadata response;
  - an explicitly newly-issued password may be returned once.

## Live scenarios not yet executed automatically

No direct browser/computer-control or SSH connector is available in this ChatGPT tool environment. Therefore no claim is made that CRUD clicks, PDFs, Excel downloads, Telegram delivery or mobile-device flows have been executed against Production in this audit pass. Those remain explicit live-retest items rather than assumed PASS results.
