-- Retain the source fingerprint and owner-approved exceptions alongside the
-- immutable pre-correction workspace, typed rows, allocations and party balances.
ALTER TABLE financial_currency_audits ADD COLUMN IF NOT EXISTS source_evidence JSONB;
