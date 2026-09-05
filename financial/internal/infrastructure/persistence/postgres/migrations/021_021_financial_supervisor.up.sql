-- Additive only: no transaction, balance or production data is rewritten.
CREATE TABLE IF NOT EXISTS financial_supervisor_snapshots (
 company_id BIGINT PRIMARY KEY REFERENCES companies(id),
 revision BIGINT NOT NULL,
 checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 report JSONB NOT NULL
);
CREATE TABLE IF NOT EXISTS financial_supervisor_approvals (
 company_id BIGINT NOT NULL REFERENCES companies(id),
 revision BIGINT NOT NULL,
 approved_by BIGINT,
 draft_checksum TEXT NOT NULL,
 approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(company_id, revision)
);
ALTER TABLE financial_supervisor_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_supervisor_approvals ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_supervisor_snapshot ON financial_supervisor_snapshots;
CREATE POLICY tenant_supervisor_snapshot ON financial_supervisor_snapshots
 USING (company_id=current_app_company_id()) WITH CHECK (company_id=current_app_company_id());
DROP POLICY IF EXISTS tenant_supervisor_approval ON financial_supervisor_approvals;
CREATE POLICY tenant_supervisor_approval ON financial_supervisor_approvals
 USING (company_id=current_app_company_id()) WITH CHECK (company_id=current_app_company_id());
