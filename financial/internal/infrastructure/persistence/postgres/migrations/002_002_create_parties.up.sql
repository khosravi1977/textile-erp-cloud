CREATE TABLE IF NOT EXISTS parties (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(20) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    type VARCHAR(20) NOT NULL CHECK (type IN ('Customer','Supplier','Contractor','Internal')),
    national_id VARCHAR(20),
    tax_id VARCHAR(20),
    mobile VARCHAR(15),
    phone VARCHAR(15),
    address VARCHAR(200),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS customer_credit_profiles (
    id BIGSERIAL PRIMARY KEY,
    party_id BIGINT NOT NULL REFERENCES parties(id),
    credit_limit DECIMAL(18,2) NOT NULL DEFAULT 0,
    credit_days INT DEFAULT 30,
    std_wastage_rate DECIMAL(5,2) DEFAULT 3.00,
    wastage_responsibility VARCHAR(20) DEFAULT 'Contractor',
    downtime_rate DECIMAL(18,2) DEFAULT 0,
    base_score INT DEFAULT 50,
    risk_group VARCHAR(20) DEFAULT 'Medium',
    is_blocked BOOLEAN DEFAULT false,
    block_reason VARCHAR(200),
    last_score_update TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
