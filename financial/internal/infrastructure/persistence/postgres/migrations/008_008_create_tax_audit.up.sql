CREATE TABLE IF NOT EXISTS tax_invoices (
    id BIGSERIAL PRIMARY KEY,
    commission_invoice_id BIGINT NOT NULL REFERENCES commission_invoices(id),
    tax_unique_code VARCHAR(50),
    send_status VARCHAR(20) DEFAULT 'NotSent',
    send_at TIMESTAMP,
    response_json JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    entity_name VARCHAR(100),
    entity_id BIGINT,
    action_type VARCHAR(20),
    before_data JSONB,
    after_data JSONB,
    user_id BIGINT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    branch_id BIGINT,
    ip_address VARCHAR(50)
);
