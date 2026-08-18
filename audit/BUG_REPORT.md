# Bug Report

## Closed on audit branch

### P0 / Critical — Stored portal passwords exposed by management APIs
**Root cause:** admin/team list endpoints explicitly decrypted `PasswordEnc` and `accessResponse` had a fallback that decrypted an existing password when no raw one-time password was supplied.

**Risk:** any authenticated manager/admin with endpoint access could recover reusable employee credentials; the UI also exposed copy controls.

**Fix:** production portal Docker build now runs a deterministic, tested source hardener before compilation. Stored passwords are write-only from API perspective. Existing credentials remain available only for internal downstream synchronization; a newly created/reset password can be returned once.

**Regression:** portal hardener tests inspect the real `main.go`, verify the insecure patterns disappear, verify idempotence and role mappings, and production Docker build compiles the hardened source.

### P0 / Critical — Unknown portal roles fail open to owner
**Root cause:** `normalizeAccessRole` returned `owner` for every unknown non-empty role.

**Risk:** corrupted/new/typo role values could gain owner-level permissions.

**Fix:** unknown non-empty roles fail closed to `viewer`; owner remains explicit.

### P1 / High — Operational role propagation downgraded manager/accountant to viewer
**Root cause:** provisioning mapped only owner→admin and every other portal role→viewer.

**Risk:** portal and operational permissions drift, encouraging direct DB corrections and inconsistent authorization.

**Fix:** deterministic mapping owner→admin, manager→manager, accountant→accountant, viewer/other→viewer. Operational session response also reports effective portal role instead of hard-coded `customer`.

### P1 / High — Financial frontend permission normalization failed open
**Root cause:** absent, empty, or fully invalid permission lists fell back to every financial page. Session profile also defaulted role to owner.

**Risk:** a malformed non-owner claim could expose the full financial navigation/UI surface.

**Fix:** strict permission normalizer: only owner/admin may use full-access fallback; all other empty/invalid lists produce zero pages. Child pages are derived only from their authorized parent permission.

### P1 / High — Management gross margin treated purchases as COGS
**Root cause:** workspace summary subtracted all incoming purchases and operating expenses directly from sales.

**Risk:** inventory purchases immediately depressed profit regardless of whether goods were sold; dashboard profit could materially disagree with ledger economics.

**Fix:** management revenue is sales net of output VAT plus financial yarn sales; gross margin uses recorded `costAmount` COGS; operating expenses are subtracted after gross margin. Purchases remain a separate KPI.

### P1 / High — Internal transfers changed backend cash KPI
**Root cause:** summary cash calculation applied a transfer only to `accountId` and ignored the `counterAccountId` side.

**Risk:** moving money between company accounts falsely changed total liquidity.

**Fix:** transfer-aware balance calculation applies the opposite side to the counter-account. Regression test proves total liquidity invariant.

### P1 / High — Historical edit reversal posted on current date
**Root cause:** `reverseLedgerEntry` forced reversal date to `time.Now()` while the corrected entry retained the source date.

**Risk:** editing a prior-period sale/purchase could shift revenue/cost between reporting periods and corrupt period P&L.

**Fix:** reversal remains on the source accounting date. Closed-period protection can therefore reject changes to closed historical periods instead of silently moving the reversal into today.

### P1 / High — Management revenue included output VAT
**Root cause:** invoice `total` was treated as revenue.

**Risk:** collected VAT, which is a liability, inflated reported sales/profit.

**Fix:** summary and audited Financial Health use gross invoice total for customer-facing sales but remove `taxAmount` from revenue/profitability.

### P1 / High — Legacy advisor/credit API returned fabricated profiles
**Root cause:** handlers constructed a fixed credit limit, risk group and score instead of loading actual customer credit state.

**Risk:** a user could receive business/credit recommendations presented as real despite being based on constants.

**Fix:** legacy endpoints now return Service Unavailable with an explicit data-source limitation until a real persisted profile source is wired. The data-driven current financial UI remains independent.

### P1 / High — Legacy profitability API silently substituted fixed revenue
**Root cause:** missing `revenue` query used a hard-coded default.

**Risk:** profitability report could appear valid while using invented revenue.

**Fix:** revenue is mandatory and validated; no fixed revenue is substituted.

### P2 / Medium — Assigned checks counted as receivable assets in backend summary
**Root cause:** summary considered every receivable check except `cleared` open.

**Risk:** a check already assigned to a supplier could remain in receivable assets after being used to settle a liability.

**Fix:** both `cleared` and `assigned` are excluded; bounced/open states remain collectible exposure as appropriate.

### P2 / Medium — Credit exposure double-reduced open checks
**Root cause:** invoice debt was already reduced when a check was accepted, then `futureChecks` was subtracted again from exposure. Returned/bounced checks could also be treated like good future checks.

**Risk:** customer risk was understated.

**Fix:** only `open` checks are considered current check exposure and are added back to invoice debt before subtracting owned barter collateral.

### P2 / Medium — Non-financial incoming rows entered tax purchase population
**Root cause:** Tax Report selected all incoming invoices regardless of `nonFinancial` flag.

**Risk:** operational/non-financial records could contaminate tax purchase totals.

**Fix:** production financial web build filters non-financial incoming rows out of the tax purchase set.

### P2 / Medium — Financial Health fabricated budget variances and mislabeled EBITDA
**Root cause:** budget sales/profit and -12%/-8% variances were generated from formulas without a persisted approved budget; net/operating profit proxy was labeled EBITDA.

**Risk:** management could act on invented budget variances and a misleading accounting KPI label.

**Fix:** variance rows stay empty until a real budget source exists; UI explicitly says approved budget is required; KPI label is changed to recorded operating profit.

### P3 / Low — Aging bucket label duplicated
**Root cause:** the 31–60 day bucket was labeled 61–90.

**Fix:** distinct 31–60 and 61–90 labels in audited health calculation.

## Open finding

### P2 / Medium — Auto-created party master type is always Customer
**Location:** `ensureLedgerParty` in the accounting engine.

**Evidence:** journal-line party creation uses `type='Customer'` for any new party name, including supplier-side accounting lines.

**Impact:** monetary postings remain balanced, but the party master classification can be semantically wrong for suppliers/contractors and may affect future master-data reports or integrations.

**Recommended fix:** pass party role/type from the originating business event into `ensureLedgerParty`, or maintain a typed party master keyed by company + normalized identity and reject ambiguous automatic creation. Do not infer supplier/customer solely from free text.

**Status:** OPEN because a safe fix needs a typed source-of-truth decision; silently rewriting existing party types would be a business-data migration and was not performed without live reconciliation.

## Environment blocker (not a product bug)
GitHub Actions jobs were created but recorded no executable steps and no job logs. This is treated as CI runner/account infrastructure blockage, not as a failed application test. Production SSH/runtime authentication was also unavailable from the execution environment, so no live tenant data was mutated.
