CREATE TABLE IF NOT EXISTS ai_analysis_usage (
    id TEXT PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    app_key VARCHAR(80) NOT NULL,
    provider_model VARCHAR(120) NOT NULL DEFAULT '',
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    total_tokens BIGINT NOT NULL DEFAULT 0 CHECK (total_tokens >= 0),
    status VARCHAR(20) NOT NULL CHECK (status IN ('completed', 'failed')),
    error_code VARCHAR(80) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ai_analysis_usage_company_month
    ON ai_analysis_usage(company_id, created_at DESC, status);

ALTER TABLE ai_analysis_usage ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_ai_analysis_usage ON ai_analysis_usage;
CREATE POLICY tenant_isolation_ai_analysis_usage ON ai_analysis_usage
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());
