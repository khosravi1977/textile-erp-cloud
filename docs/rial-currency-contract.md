# Rial currency contract

All stored amounts, invoice prices, cheques, ledger postings, reports, exports
and printed statements use **IRR (Iranian rials)**. Money is an exact integer
within JavaScript's safe range. Manual rial documents retain their numbers.
Direct ERP forms accept IRR only and reject unknown or ambiguous units,
fractional money and unsafe numbers.

## HesabYar boundary

The owner-confirmed HesabYar export and incoming payloads use IRR. An omitted
currency means IRR for transaction sync, typed posting, payable cheques,
settings bank balances and bootstrap. Explicit IRR is unchanged. Only explicit
IRT is multiplied by ten, exactly once, before posting. Original source amount
and currency remain attached. Allocations, interest and reported balances use
the same unit. Amount magnitude, source name and display settings never prove a
currency unit.

Bootstrap defaults to IRR and declares both response and canonical currencies.
An explicit ?currency=IRT requests an adapter response, which rejects values
that cannot be represented in whole tomans instead of rounding away rials.
This does not change the Android app's local storage.

## Verified forward correction

Policy 1 incorrectly inferred omitted HesabYar units as IRT. The owner's source
workbook proves these amounts were already rials. Its writer and all guessing
helpers now compile only in historical test fixtures, never production binaries.
The schema migration itself does not rescale any financial data.

The replacement command requires explicit tenant, source file and confirmed IRR:

    ./currency-normalize -company COMPANY_ID \
      -source-file /private/HesabYar-transactions.xls -source-currency IRR

It reads Excel 2003 XML without executing formulas, validates its headers, and
matches existing transactions by normalized bank, date, available original time,
direction, amount and available reported balance. It never imports unsent Excel
rows, merges source duplicates or divides amounts based on magnitude. Every
correction requires retained metadata proving current = original source x 10.
Typed rows and allocations must also agree with policy-1 originals.

Unmatched external IDs require explicit owner approval to restore their retained
original amount, supplied with -restore-audit-ids ID1,ID2. Existing mobile payable
documents outside the transaction workbook are restored only when they exactly
reproduce the policy-1 conversion of the preserved original document. Bank
balances require the source report and matching Excel or retained original
evidence. Later manual payment residuals remain intact. New canonical IRR
transactions are preserved and need no historical repair.

Review the read-only plan and rehearse on an isolated restored database first.
Apply the same arguments with -apply -expect-checksum CHECKSUM_FROM_PLAN.

Apply uses a serializable tenant transaction, shared writer lock, exact workspace
checksum and policy-2 audit. That audit retains the entire pre-correction
workspace, typed rows, allocations and party balances, source SHA-256, parsed
source rows and approved restore IDs. Policy-1 originals remain untouched.
No source transactions, vouchers, uploads, volumes or secrets are deleted.

Typed vouchers use the ordinary immutable reversal path and are reposted at the
verified full rial value. The transaction points to its corrected voucher so
later ordinary reversal uses the correct amount. Linked workspace projections,
allocations and AR/AP deltas are reconciled together. Currency vouchers retain
a separate CUR: namespace from WS: projections. Bank openings use the actual
posted opening net; undated adjustments receive a marked current UTC date.

Independent database checks verify posted openings, full typed voucher amounts
and balanced posted vouchers before commit. A stale checksum, changed allocation,
unsupported source or posting error rolls back everything. Repeating the same
completed correction is a no-op. A different file cannot silently replace the
policy-2 source audit.

## Recovery

Retain a fresh production backup and verify restoration before applying. Prefer
an audited forward correction. Do not redeploy the retired toman-assuming API.
Coordinate code and data rollback with intervening user writes; never overwrite
new transactions with an old whole-database restore. Down migrations preserve
audits and source evidence and never rescale money.
