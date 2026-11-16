-- Expand only. Historical values are converted by the explicit, audited
-- currency-normalize command, never by a blind schema migration.
ALTER TABLE bank_transactions ADD COLUMN IF NOT EXISTS currency TEXT;
ALTER TABLE bank_transactions ADD COLUMN IF NOT EXISTS source_currency TEXT;
ALTER TABLE bank_transactions ADD COLUMN IF NOT EXISTS source_amount BIGINT;
ALTER TABLE bank_transactions ADD CONSTRAINT bank_transactions_rial_currency
    CHECK (currency IS NULL OR currency = 'IRR');

CREATE TABLE IF NOT EXISTS financial_currency_audits (
    company_id BIGINT NOT NULL REFERENCES companies(id),
    policy_version INTEGER NOT NULL,
    original_workspace JSONB NOT NULL,
    original_typed_transactions JSONB NOT NULL,
    original_allocations JSONB NOT NULL,
    original_party_balances JSONB NOT NULL,
    resulting_revision BIGINT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (company_id, policy_version)
);
ALTER TABLE financial_currency_audits ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_currency_audits FORCE ROW LEVEL SECURITY;
CREATE POLICY financial_currency_audits_company ON financial_currency_audits
    USING (company_id = NULLIF(current_setting('app.company_id', true), '')::BIGINT)
    WITH CHECK (company_id = NULLIF(current_setting('app.company_id', true), '')::BIGINT);
