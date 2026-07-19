CREATE TABLE IF NOT EXISTS mobile_pairing_codes (
    code_hash VARCHAR(64) PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    created_by BIGINT,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mobile_pairing_company_expiry
    ON mobile_pairing_codes(company_id, expires_at DESC);

CREATE TABLE IF NOT EXISTS mobile_devices (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    device_key VARCHAR(160) NOT NULL,
    device_name VARCHAR(160) NOT NULL DEFAULT 'HesabYar Android',
    paired_by BIGINT,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(company_id, device_key)
);

CREATE INDEX IF NOT EXISTS idx_mobile_devices_company
    ON mobile_devices(company_id, revoked_at, last_seen_at DESC);
