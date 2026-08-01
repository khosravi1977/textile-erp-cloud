ALTER TABLE telegram_report_configs
    ADD COLUMN IF NOT EXISTS weekly_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS weekly_day SMALLINT NOT NULL DEFAULT 4,
    ADD COLUMN IF NOT EXISTS weekly_time CHAR(5) NOT NULL DEFAULT '20:00',
    ADD COLUMN IF NOT EXISTS monthly_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS monthly_day SMALLINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS monthly_time CHAR(5) NOT NULL DEFAULT '20:00',
    ADD COLUMN IF NOT EXISTS accounting_sla_days SMALLINT NOT NULL DEFAULT 2,
    ADD COLUMN IF NOT EXISTS last_weekly_on DATE,
    ADD COLUMN IF NOT EXISTS last_monthly_on DATE;

ALTER TABLE telegram_report_configs
    DROP CONSTRAINT IF EXISTS telegram_report_weekly_day_range,
    ADD CONSTRAINT telegram_report_weekly_day_range
        CHECK (weekly_day BETWEEN 0 AND 6),
    DROP CONSTRAINT IF EXISTS telegram_report_monthly_day_range,
    ADD CONSTRAINT telegram_report_monthly_day_range
        CHECK (monthly_day BETWEEN 1 AND 28),
    DROP CONSTRAINT IF EXISTS telegram_report_accounting_sla_range,
    ADD CONSTRAINT telegram_report_accounting_sla_range
        CHECK (accounting_sla_days BETWEEN 1 AND 30),
    DROP CONSTRAINT IF EXISTS telegram_report_weekly_time_format,
    ADD CONSTRAINT telegram_report_weekly_time_format
        CHECK (weekly_time ~ '^[0-2][0-9]:[0-5][0-9]$'),
    DROP CONSTRAINT IF EXISTS telegram_report_monthly_time_format,
    ADD CONSTRAINT telegram_report_monthly_time_format
        CHECK (monthly_time ~ '^[0-2][0-9]:[0-5][0-9]$');

CREATE TABLE IF NOT EXISTS telegram_report_recipients (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    chat_title TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    receive_daily BOOLEAN NOT NULL DEFAULT TRUE,
    receive_weekly BOOLEAN NOT NULL DEFAULT TRUE,
    receive_monthly BOOLEAN NOT NULL DEFAULT TRUE,
    receive_alerts BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (company_id, chat_id)
);

INSERT INTO telegram_report_recipients(company_id, chat_id, chat_title)
SELECT company_id, chat_id, chat_title
FROM telegram_report_configs
WHERE chat_id <> ''
ON CONFLICT(company_id, chat_id) DO NOTHING;

ALTER TABLE telegram_report_deliveries
    ADD COLUMN IF NOT EXISTS recipient_id BIGINT
        REFERENCES telegram_report_recipients(id) ON DELETE SET NULL;

ALTER TABLE telegram_report_deliveries
    DROP CONSTRAINT IF EXISTS telegram_report_deliveries_company_id_report_type_report_date_key;

CREATE UNIQUE INDEX IF NOT EXISTS uq_telegram_delivery_recipient_period
    ON telegram_report_deliveries(company_id, recipient_id, report_type, report_date)
    WHERE recipient_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_telegram_recipients_company
    ON telegram_report_recipients(company_id, enabled);

ALTER TABLE telegram_report_recipients ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_telegram_report_recipients
    ON telegram_report_recipients;
CREATE POLICY tenant_isolation_telegram_report_recipients
    ON telegram_report_recipients
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());
