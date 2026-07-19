CREATE TABLE IF NOT EXISTS companies (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(150) NOT NULL,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO companies (code, name)
VALUES ('default', 'Default Company')
ON CONFLICT (code) DO NOTHING;

ALTER TABLE branches
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE parties
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE customer_credit_profiles
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE inventory_lots
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE inventory_txns
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE inventory_txn_lines
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE production_orders
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE production_consumptions
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE production_outputs
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE machine_idle_penalties
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE waste_allocations
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE journal_vouchers
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE journal_voucher_lines
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE ar_ap_balances
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE settlements
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE settlement_lines
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;
ALTER TABLE commission_invoices
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS version_number BIGINT NOT NULL DEFAULT 1;

UPDATE branches SET company_id = COALESCE(company_id, 1);
UPDATE parties SET company_id = COALESCE(company_id, 1);
UPDATE customer_credit_profiles SET company_id = COALESCE(company_id, 1);
UPDATE inventory_lots SET company_id = COALESCE(company_id, 1);
UPDATE inventory_txns SET company_id = COALESCE(company_id, 1);
UPDATE inventory_txn_lines SET company_id = COALESCE(company_id, 1);
UPDATE production_orders SET company_id = COALESCE(company_id, 1);
UPDATE production_consumptions SET company_id = COALESCE(company_id, 1);
UPDATE production_outputs SET company_id = COALESCE(company_id, 1);
UPDATE machine_idle_penalties SET company_id = COALESCE(company_id, 1);
UPDATE waste_allocations SET company_id = COALESCE(company_id, 1);
UPDATE accounts SET company_id = COALESCE(company_id, 1);
UPDATE journal_vouchers SET company_id = COALESCE(company_id, 1);
UPDATE journal_voucher_lines SET company_id = COALESCE(company_id, 1);
UPDATE ar_ap_balances SET company_id = COALESCE(company_id, 1);
UPDATE settlements SET company_id = COALESCE(company_id, 1);
UPDATE settlement_lines SET company_id = COALESCE(company_id, 1);
UPDATE commission_invoices SET company_id = COALESCE(company_id, 1);

CREATE INDEX IF NOT EXISTS idx_branches_company_id ON branches(company_id);
CREATE INDEX IF NOT EXISTS idx_parties_company_id ON parties(company_id);
CREATE INDEX IF NOT EXISTS idx_inventory_lots_company_id ON inventory_lots(company_id);
CREATE INDEX IF NOT EXISTS idx_inventory_txns_company_id ON inventory_txns(company_id);
CREATE INDEX IF NOT EXISTS idx_production_orders_company_id ON production_orders(company_id);
CREATE INDEX IF NOT EXISTS idx_settlements_company_id ON settlements(company_id);
CREATE INDEX IF NOT EXISTS idx_commission_invoices_company_id ON commission_invoices(company_id);

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT REFERENCES companies(id),
    user_id BIGINT,
    method VARCHAR(10) NOT NULL,
    path VARCHAR(255) NOT NULL,
    entity_name VARCHAR(100),
    entity_id VARCHAR(100),
    remote_ip VARCHAR(100),
    user_agent TEXT,
    request_id VARCHAR(100),
    duration_ms BIGINT DEFAULT 0,
    status_code INTEGER DEFAULT 200,
    details JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE audit_logs
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id),
    ADD COLUMN IF NOT EXISTS method VARCHAR(10),
    ADD COLUMN IF NOT EXISTS path VARCHAR(255),
    ADD COLUMN IF NOT EXISTS entity_name VARCHAR(100),
    ADD COLUMN IF NOT EXISTS entity_id VARCHAR(100),
    ADD COLUMN IF NOT EXISTS remote_ip VARCHAR(100),
    ADD COLUMN IF NOT EXISTS user_agent TEXT,
    ADD COLUMN IF NOT EXISTS request_id VARCHAR(100),
    ADD COLUMN IF NOT EXISTS duration_ms BIGINT DEFAULT 0,
    ADD COLUMN IF NOT EXISTS status_code INTEGER DEFAULT 200,
    ADD COLUMN IF NOT EXISTS details JSONB DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_audit_logs_company_id ON audit_logs(company_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
