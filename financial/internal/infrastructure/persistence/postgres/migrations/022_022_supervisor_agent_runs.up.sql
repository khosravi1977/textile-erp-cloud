-- Supervisor finance agent runs: audit trail for automated triage.
-- Additive only — no ledger data is touched.
CREATE TABLE IF NOT EXISTS financial_agent_runs (
 id BIGSERIAL PRIMARY KEY,
 company_id BIGINT NOT NULL REFERENCES companies(id),
 run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 input_hash TEXT NOT NULL,
 findings_total INT NOT NULL DEFAULT 0,
 escalate INT NOT NULL DEFAULT 0,
 needs_human INT NOT NULL DEFAULT 0,
 monitor INT NOT NULL DEFAULT 0,
 summary TEXT NOT NULL DEFAULT '',
 model TEXT NOT NULL DEFAULT '',
 llm_used BOOLEAN NOT NULL DEFAULT FALSE,
 triage JSONB NOT NULL DEFAULT '[]'::jsonb
);
CREATE INDEX IF NOT EXISTS financial_agent_runs_company_time
 ON financial_agent_runs (company_id, run_at DESC);
ALTER TABLE financial_agent_runs ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_agent_runs ON financial_agent_runs;
CREATE POLICY tenant_agent_runs ON financial_agent_runs
 USING (company_id=current_app_company_id()) WITH CHECK (company_id=current_app_company_id());
