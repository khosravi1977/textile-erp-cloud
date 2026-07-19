CREATE TABLE IF NOT EXISTS financial_users (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT REFERENCES companies(id),
    username VARCHAR(100) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_module_access (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT REFERENCES companies(id),
    user_id BIGINT REFERENCES financial_users(id) ON DELETE CASCADE,
    module_key VARCHAR(100) NOT NULL,
    can_view BOOLEAN NOT NULL DEFAULT true,
    can_create BOOLEAN NOT NULL DEFAULT false,
    can_update BOOLEAN NOT NULL DEFAULT false,
    can_delete BOOLEAN NOT NULL DEFAULT false,
    granted_by BIGINT,
    granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, module_key)
);

CREATE INDEX IF NOT EXISTS idx_financial_users_company_id ON financial_users(company_id);
CREATE INDEX IF NOT EXISTS idx_user_module_access_user_id ON user_module_access(user_id);
