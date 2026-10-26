# ADR-0004 — Single Source of Truth: typed core is canonical, workspace is a projection

Status: accepted (staged rollout) · Date: 2026-10-26 · Owner-approved direction: MASTER-RESCUE-10 F1

## Context
The same financial event lives in up to four stores: typed core
(`bank_transactions`), workspace state (JSONB projection), GL vouchers
(`journal_vouchers`), and the phone's local store. Every major incident of
2026-09/10 (duplicate rows, balance negatives, cheque-flow stall, 109
mobile-core findings) traces to writers that updated one store without the
others.

## Decision
1. **Canonical store** = `bank_transactions` (typed core) + `journal_vouchers`
   (GL). The workspace JSONB is a **display projection** — it must be
   derivable, never independently authoritative.
2. **One-way write path**: UI/app → typed core (`financecore.PostTransaction`,
   voucherless, `withGL=false` — GL stays with the workspace ledger engine)
   → mirror into the workspace projection. Direct financial writes to the
   workspace from outside this path are regressions
   (`check-no-direct-financial-put` ledger shrinks over time).
3. **Reconciliation is continuous**: `scripts/recon/four_ledger.py` runs
   nightly on the VPS (03:30) and flags drift (I1 anchor invariant,
   I2 dual-write consistency). Drift is a bug, not a state.

## Staged rollout (this ADR ships in stages; each stage is deployable alone)
- **Stage 1 (this change)**: operational expenses post to the typed core at
  creation (`source=OPERATIONAL`, external id `OP-<id>`, idempotency key
  `operational-exp-<id>`). Idempotent re-syncs do NOT re-post. Supervisor is
  unaffected (its typed scan filters `source='HESABYAR'`).
- **Stage 2**: cheque dispositions (payableDocs/receivableDocs) gain typed
  instruments; the mobile/cheque flows post through the same path.
- **Stage 3**: historical backfill — the 132 payment-verification queue and
  ERP-only movements get typed twins via a reviewed, one-shot migration
  script (audited, reversible), shrinking I2 drift to the designed
  ERP-only residue.
- **Stage 4**: the workspace's `accounts.opening` anchoring moves from
  phone-reported balances to typed-core balances; the phone report becomes
  a cross-check, not the anchor.

## Known limitations of Stage 1
- Amount/date edits to an operational expense after creation remain
  workspace-only; the supervisor surfaces the drift and Stage 3 closes the
  class. (Typed edits need the reverse API; deferred to keep this stage
  low-risk.)
- Duplicate-post risk is handled by the typed core's external-id idempotency;
  a failed workspace save after a successful post leaves the typed row in
  place (correct — the event did happen).

## Consequences
- The reconciler's I2 drift stops growing for new operational expenses.
- Writers keep shrinking toward one path; the four-store incident class
  becomes structurally impossible rather than patched.
