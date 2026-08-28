# Test Data Created

## Production
**No production business/test record was created, modified, or deleted by this audit run.**

Reason: authenticated production UI/SSH/database access was not available from the execution environment. The audit therefore did not attempt to bypass access controls or use unknown credentials.

If live access becomes available, every audit-created business record must:
- begin with `AGENT_TEST_` where a name/reference field exists;
- include note `Created by Viora Accounting Auditor` where notes are supported;
- be isolated from real customer/supplier/check/inventory data;
- be deleted only if the record was created by this audit and cleanup is accounting-safe;
- preserve audit evidence for any generated journal/reversal before cleanup.

## Repository test fixtures
Regression tests added to the branch use synthetic in-memory/unit values only. Examples include:
- two internal accounts and a transfer to verify total-liquidity invariance;
- sample sale/purchase/expense values to verify COGS-based profit;
- taxable invoice values to verify output VAT is excluded from revenue;
- open/assigned/cleared/bounced checks to verify receivable asset treatment;
- historical ledger entry to verify reversal remains in the source period;
- synthetic session permissions to verify fail-closed non-owner access.

These fixtures do not contain production identifiers, secrets, customer data, passwords, tokens or API keys.

## Cleanup
No production cleanup was required because no production test data was created.
