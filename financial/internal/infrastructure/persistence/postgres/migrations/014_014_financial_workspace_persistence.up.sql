CREATE TABLE IF NOT EXISTS financial_workspace_states (
    company_id BIGINT PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
    state JSONB NOT NULL DEFAULT '{}'::jsonb,
    revision BIGINT NOT NULL DEFAULT 1,
    checksum VARCHAR(64) NOT NULL DEFAULT '',
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT financial_workspace_revision_positive CHECK (revision > 0),
    CONSTRAINT financial_workspace_state_object CHECK (jsonb_typeof(state) = 'object')
);

CREATE TABLE IF NOT EXISTS financial_workspace_history (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL,
    checksum VARCHAR(64) NOT NULL,
    state JSONB NOT NULL,
    changed_by BIGINT,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (company_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_financial_workspace_history_company_date
    ON financial_workspace_history(company_id, changed_at DESC);

ALTER TABLE financial_workspace_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_workspace_states FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_workspace_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_workspace_history FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_financial_workspace_states ON financial_workspace_states;
CREATE POLICY tenant_isolation_financial_workspace_states ON financial_workspace_states
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_financial_workspace_history ON financial_workspace_history;
CREATE POLICY tenant_isolation_financial_workspace_history ON financial_workspace_history
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());
