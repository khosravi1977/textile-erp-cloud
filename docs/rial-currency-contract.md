# Rial currency contract

All persisted ERP amounts, invoice prices, cheque amounts, ledger amounts,
reports, Excel exports and printed statements use **IRR (Iranian rials)**.
Existing manually entered rial invoices and cheques retain their numeric values.
`Money` JSON is integer rials, matching the existing `_rials` database columns.

Direct financial and operational JSON writes accept rials only. Explicit IRT,
unknown units, alternate ambiguous mobile unit fields, nonnumeric values,
NaN/infinity and amounts outside JavaScript's exact safe range are rejected.
Omitted units on established direct ERP forms mean IRR.

## HesabYar boundary

The shipped Android client stores whole tomans internally (even when its display
switch says rials), and its legacy sync omits a unit. Only the mobile adapter
therefore treats an omitted unit as IRT. It converts the transaction, allocations,
interest, cheque and reported bank balance to IRR before posting. Explicit IRR
inputs remain unchanged; explicit IRT inputs are multiplied by ten exactly once.
Canonical rows retain their source unit and original transaction amount.

Legacy bootstrap responses use an explicit IRT boundary contract to prevent the
old app from receiving rials and sending them back as tomans. Modern clients use
`GET /api/mobile/bootstrap?currency=IRR`. Every response declares its unit and the
canonical ERP unit. The legacy balance supersedes opening balance, which is
omitted from IRT bootstrap. If a value cannot be represented in whole tomans,
bootstrap rejects it instead of silently discarding rials; that client needs IRR
support. This adapter does not change the Android app's local storage.

## Audited historical normalization

The additive currency schema does **not** blindly rescale data. The maintenance
command does not run seeds or migrations. Run it from the new API image, for an
explicit tenant, first in read-only mode:

```sh
docker exec textile-erp-financial-api ./currency-normalize -company COMPANY_ID
docker exec textile-erp-financial-api ./currency-normalize -company COMPANY_ID \
  -apply -expect-checksum CHECKSUM_FROM_READ_ONLY_PLAN
```

Review the plan and rehearse on an isolated database before production apply.
Only proven legacy Android sources (`mobile_sms`, `hesabyar_mobile`, mobile
transaction projections, typed HESABYAR, and HY-prefixed IMPORT) or explicit IRT
rows are rescaled. Manual documents and OPERATIONAL typed rows remain numeric
rials. Reported mobile bank balances are converted and opening balances are
reconciled against the converted movements, including transfer counterlegs.
If later payments create a residual after the last bank report, an exact
matching historical workspace supplies the balance anchor. Its revision is
retained, so later manual rial payments are neither multiplied nor erased.
An unprovable anchor stops the transaction.

The apply uses one serializable tenant transaction and a shared writer lock. A
changed workspace checksum, invalid amount, closed-period posting or accounting
failure rolls the entire operation back. It retains the original workspace,
typed transactions, allocations and party balances in tenant-scoped
`financial_currency_audits`, appends a workspace revision, and records a policy
version. Repeating a completed migration does not multiply anything again.

Typed GL vouchers are reversed using the normal immutable reversal path, then
reposted at their full rial value. The typed transaction points to the new
voucher so a subsequent ordinary reversal reverses the correct amount. Currency
vouchers use a separate `CUR:` namespace from derived `WS:` workspace vouchers.
Allocations and AR/AP deltas use the same unit and posting rules.
Undated legacy bank openings use their actual posted ledger net for reversal.
An opening without a prior posted date receives an explicitly marked current
UTC currency-adjustment date; no historical transaction date is fabricated.

## Recovery

Take the normal production database backup before applying. Do not deploy an old
toman-assuming API after data normalization. Prefer a tested forward correction.
If restoration is required, coordinate an API rollback with an audited database
restore/replay including intervening transactions. The schema down migration is
intentionally non-destructive; it does not delete the audit or blindly divide
current values, vouchers or balances. No volumes, uploads or secrets are touched.
