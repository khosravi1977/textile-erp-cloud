CREATE TABLE IF NOT EXISTS credit_score_logs (
    id BIGSERIAL PRIMARY KEY,
    party_id BIGINT NOT NULL REFERENCES parties(id),
    old_score INT,
    new_score INT,
    change_reason VARCHAR(50),
    change_amount INT,
    reference_doc_type VARCHAR(50),
    reference_doc_id BIGINT,
    changed_by BIGINT,
    changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS credit_alerts (
    id BIGSERIAL PRIMARY KEY,
    party_id BIGINT NOT NULL REFERENCES parties(id),
    alert_type VARCHAR(30),
    severity VARCHAR(10),
    message VARCHAR(200),
    is_resolved BOOLEAN DEFAULT false,
    resolved_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS risk_group_definitions (
    id BIGSERIAL PRIMARY KEY,
    group_name VARCHAR(20),
    min_score INT,
    max_score INT,
    credit_multiplier DECIMAL(5,2) DEFAULT 1.00,
    prepayment_percent INT DEFAULT 0,
    allow_check BOOLEAN DEFAULT true,
    allow_barter BOOLEAN DEFAULT true,
    allow_credit_days INT DEFAULT 30
);
