# Root Cause Report

## RC-01 — Credential model mixed authentication storage with credential recovery
The portal stored both a password hash and decryptable password material because downstream module synchronization needed the original credential. Management responses then reused the decryption helper and treated the stored password as display data.

**Systemic cause:** authentication, downstream provisioning and UI credential-delivery responsibilities were not separated.

**Correction:** existing stored passwords are no longer response data. Downstream synchronization may use the internal decrypted value, while create/reset may reveal only the newly supplied/generated password once. The portal production build applies and tests this hardening deterministically.

## RC-02 — Authorization defaults optimized for convenience instead of least privilege
Two fail-open defaults existed:
1. unknown portal role → owner;
2. missing/empty financial permission list → all pages, with session role defaulting to owner.

**Systemic cause:** backward-compatibility/default handling was implemented as permissive fallback without distinguishing trusted owner migration from malformed non-owner claims.

**Correction:** unknown roles fail closed to viewer and non-owner missing/invalid permissions result in no financial page access. Explicit owner/admin fallback remains.

## RC-03 — Portal and operational role vocabularies were not mapped one-to-one
Provisioning used a two-level translation: owner→admin, everyone else→viewer.

**Systemic cause:** the operational integration was originally designed around a simpler role model and was not upgraded when portal roles expanded.

**Correction:** explicit owner/manager/accountant/viewer mapping and effective role returned in operational portal session.

## RC-04 — Management KPIs were reconstructed independently from accounting semantics
The workspace summary/Financial Health formulas directly combined sales, purchases, expenses and inventory state rather than deriving definitions from accounting concepts.

Symptoms included:
- purchases treated as immediate COGS;
- output VAT treated as revenue;
- product costs allocated from general operating expenses;
- DIO based on expenses;
- invented budget targets/variances;
- operating-profit proxy labeled EBITDA.

**Systemic cause:** management reporting and accounting ledger evolved separately, with formulas chosen for visible dashboard output before complete accounting source data existed.

**Correction:** audited KPI helper uses net revenue, recorded COGS, operating expense, working-capital balances and honest data-availability behavior. No budget variance is invented without a budget source.

## RC-05 — Internal transfer stored as one business row but some readers treated it as a one-sided cash movement
The UI account balance helper already interpreted the counter-account, but backend summary and alerts did not consistently apply `counterAccountId`.

**Systemic cause:** duplicated balance logic across layers with different interpretations of the same transfer representation.

**Correction:** backend summary and alert logic now apply the counter-account side and tests verify total-liquidity invariance and absence of false negative-balance alerts.

## RC-06 — Reversal logic used processing time instead of accounting event time
Historical source corrections built a reversing entry and overwrote its date with today.

**Systemic cause:** technical reversal timestamp was confused with accounting posting date.

**Correction:** reversal keeps the original accounting date. Closed-period checks then correctly protect historical locked periods.

## RC-07 — Check lifecycle state and balance-sheet classification were not consistently aligned across UI, API, reports and alerts
Some readers treated any non-cleared receivable check as an asset, including assigned checks. Credit risk calculation also subtracted an open check from invoice debt after the invoice debt had already been reduced by that check settlement. The workspace API validated row shape but not the transition from the previous check state, while UI lookup could match non-open or ambiguous duplicate check numbers.

**Systemic cause:** check lifecycle was modeled across multiple arrays/status consumers without one cross-document transition rule.

**Correction:** assigned/cleared checks are removed from receivable-check assets; current credit view uses only open checks as check exposure; bounced/returned alerts are explicit; UI selection is open-only and exact; `/api/workspace` now validates previous→new lifecycle, supplier, amount and one-check/one-purchase linkage before persistence.

## RC-08 — Legacy demo/default values survived into callable business APIs
Credit/advisor endpoints constructed a fixed profile and profitability silently used fixed revenue.

**Systemic cause:** prototype defaults were left in production-callable handlers after the product moved to real data.

**Correction:** affected endpoints now reject insufficient real data instead of manufacturing a business result.

## RC-09 — Tax report population did not distinguish operational-only incoming rows
The tax page selected all incoming invoices, including `nonFinancial` rows.

**Systemic cause:** the financial/non-financial semantic flag was enforced in ledger posting but not reused by the independent tax UI filter.

**Correction:** production financial build filters non-financial incoming rows from the tax purchase population.

## RC-10 — Fiscal-period status was treated as a freely editable field
The period endpoint allowed a closed period to be switched back to open without a transition rule.

**Systemic cause:** fiscal closing was implemented as a status update rather than an accounting state transition with immutability semantics.

**Correction:** period updates lock the target row and reject `Closed → Open`. Corrections after close must be represented as controlled adjustment entries in an open period.

## RC-11 — Party master typing is under-specified
Automatic GL party creation currently uses Customer type for all unknown party names.

**Systemic cause:** journal lines carry party name but not typed business-party semantics. Free-text auto-creation cannot safely infer whether the party is a customer, supplier, contractor or internal party.

**Status:** open design/data-model issue. A safe correction requires a typed party source and live data migration/reconciliation; the audit did not rewrite production party classifications without that evidence.

## Cross-cutting prevention controls added
- deterministic portal build hardener that fails on source-anchor drift;
- financial Vite integrity transform that fails the build if audited replacements no longer apply;
- API-level workspace lifecycle guard for cross-document check transitions;
- irreversible fiscal-period transition guard;
- regression tests for accounting math, permissions, transfer invariance, check classification/assignment, tax/revenue, alerts, fiscal-period transitions and reversal date;
- financial production build now runs Node tests before Vite build;
- repository CI portal step hardens the source before portal tests;
- audit documents separate code evidence, targeted local evidence and live-production evidence so blocked live tests cannot be misreported as PASS.
