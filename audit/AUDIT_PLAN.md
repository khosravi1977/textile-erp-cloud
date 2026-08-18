# Viora Accounting Auditor — Audit Plan

## Target
- Product: Textile ERP Cloud
- Financial tenant requested for live audit: `paregol`
- Production URL requested by the audit specification: `https://textile.vioraapps.com/`
- Audit branch: `fix/accounting-audit-security-rbac`
- Safety prefix for any permitted live test data: `AGENT_TEST_`

## Guardrails
1. Never expose, log, commit, or transmit passwords, tokens, API keys, private keys, environment secrets, or production credentials.
2. Never delete or rewrite production business data that was not created by this audit agent.
3. No force-push, history rewrite, repository deletion, public release, or destructive infrastructure operation.
4. A scenario can be marked PASS only when the evidence supports the tested layer. Static/code review is not represented as live-production PASS.
5. Live test records, if access becomes available, must use `AGENT_TEST_` and note `Created by Viora Accounting Auditor`.

## Actual financial modules discovered from source
1. Dashboard
2. Financial Health
3. Initial Data
4. Operational Data
5. Incoming Invoices
6. Chelle Incoming
7. Yarn Out Invoices
8. Financial Invoices
9. Inventory
10. Costs
11. Receivable Documents
12. Payable Documents
13. Bank & Cash
14. Accounting / Ledgers & Trial Balance
15. Reports
16. Tax Reports
17. Credit Scoring
18. Advisor
19. Telegram Reports
20. HesabYar / Mobile Integration

## Audit method
Each business scenario is evaluated through the intended chain:

`UI -> API/workspace state -> validation/business rules -> persistence -> derived double-entry ledger -> accounting reports -> management/reporting surfaces`

The audit covers:
- Sales and purchase recognition
- Settlement methods: cash, bank, credit, check, barter, assigned receivable check
- Expenses and other cash movements
- Internal account transfers
- Receivable/payable check lifecycle
- Inventory and COGS
- Edit/reversal behavior
- Duplicate/required-field/period controls
- Permissions/RBAC
- Tax and management reporting
- Credit/advisor integrity
- Export/report consistency where source evidence exists

## Execution environments
### Source/PR audit
Completed against the repository branch. Root-cause fixes and regression tests were committed to the audit branch.

### Isolated targeted validation
Completed for the highest-risk accounting invariants using local Go/Node harnesses available in the execution environment:
- transfer liquidity invariant
- COGS-based profitability
- reversal period/date behavior
- financial permission fail-closed behavior
- financial-health helper behavior
- Vite source hardening transform syntax/behavior

### Repository CI
The repository CI workflow is configured to run financial Go tests, operational Go tests, hardened portal tests, financial web tests/build, operational web build, Telegram tests, Docker Compose validation, and Kustomize rendering. At audit time GitHub Actions created a job but did not execute any recorded step; job logs were unavailable. Therefore repository-wide CI is reported as **BLOCKED BY RUNNER/ACCOUNT INFRASTRUCTURE**, not as a product test failure.

### Live production E2E
No authenticated production mutation was performed. The execution environment could not establish usable production SSH/private-key access and no authenticated browser session was available. Production database/services were therefore not modified. Live E2E remains **BLOCKED**, and no production test record was created.

## Exit criteria used
A code-level finding is closed only when:
- root cause is identified;
- deterministic fix is on the branch;
- regression test/check is present;
- the change is wired into production build/runtime path;
- no secret is embedded in the change.

A production scenario remains open until the authenticated live E2E chain is executable and reconciled with DB/accounting reports.
