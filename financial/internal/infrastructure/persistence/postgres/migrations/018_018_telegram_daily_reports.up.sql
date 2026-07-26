CREATE TABLE IF NOT EXISTS telegram_report_configs (
    company_id BIGINT PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL DEFAULT '',
    chat_title TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    alerts_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    daily_time CHAR(5) NOT NULL DEFAULT '20:00',
    timezone TEXT NOT NULL DEFAULT 'Asia/Tehran',
    last_daily_on DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT telegram_report_time_format CHECK (daily_time ~ '^[0-2][0-9]:[0-5][0-9]$')
);

CREATE TABLE IF NOT EXISTS telegram_pairing_codes (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    code_hash CHAR(64) NOT NULL UNIQUE,
    created_by BIGINT,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS telegram_report_deliveries (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    report_type TEXT NOT NULL,
    report_date DATE NOT NULL,
    status TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    telegram_message_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (company_id, report_type, report_date)
);

CREATE INDEX IF NOT EXISTS idx_telegram_pairing_expiry
    ON telegram_pairing_codes(expires_at);
CREATE INDEX IF NOT EXISTS idx_telegram_delivery_company_date
    ON telegram_report_deliveries(company_id, created_at DESC);

ALTER TABLE telegram_report_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE telegram_pairing_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE telegram_report_deliveries ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_telegram_report_configs ON telegram_report_configs;
CREATE POLICY tenant_isolation_telegram_report_configs ON telegram_report_configs
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_telegram_pairing_codes ON telegram_pairing_codes;
CREATE POLICY tenant_isolation_telegram_pairing_codes ON telegram_pairing_codes
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_telegram_report_deliveries ON telegram_report_deliveries;
CREATE POLICY tenant_isolation_telegram_report_deliveries ON telegram_report_deliveries
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());
