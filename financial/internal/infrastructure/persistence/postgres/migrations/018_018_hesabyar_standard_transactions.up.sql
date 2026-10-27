-- Standard HesabYar integration entities. Historical workspace and mobile rows
-- remain untouched while new API transactions are stored durably and posted once.

ALTER TABLE parties
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP;

CREATE TABLE IF NOT EXISTS party_roles (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    party_id BIGINT NOT NULL,
    role VARCHAR(40) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_party_roles_company_party
        FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id) ON DELETE CASCADE,
    CONSTRAINT ck_party_roles_role CHECK (role IN (
        'CUSTOMER', 'SUPPLIER', 'EMPLOYEE', 'PETTY_CASH_HOLDER', 'OWNER', 'OTHER'
    )),
    UNIQUE (company_id, party_id, role)
);

CREATE INDEX IF NOT EXISTS idx_party_roles_company_role
    ON party_roles(company_id, role, party_id);

CREATE TABLE IF NOT EXISTS bank_transactions (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    external_transaction_id VARCHAR(160) NOT NULL,
    bank_account_id VARCHAR(120) NOT NULL,
    direction VARCHAR(3) NOT NULL,
    amount NUMERIC(20,2) NOT NULL,
    transaction_type VARCHAR(40) NOT NULL,
    party_id BIGINT,
    category_id BIGINT,
    subcategory_id BIGINT,
    counter_account_id VARCHAR(120),
    description VARCHAR(500),
    bank_reference VARCHAR(160),
    occurred_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(24) NOT NULL DEFAULT 'SYNCED',
    source VARCHAR(40) NOT NULL DEFAULT 'hesabyar',
    posting_voucher_id BIGINT,
    raw_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_bank_transactions_company_party
        FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id),
    CONSTRAINT fk_bank_transactions_company_voucher
        FOREIGN KEY (company_id, posting_voucher_id) REFERENCES journal_vouchers(company_id, id),
    CONSTRAINT ck_bank_transactions_direction CHECK (direction IN ('IN', 'OUT')),
    CONSTRAINT ck_bank_transactions_amount CHECK (amount > 0),
    CONSTRAINT ck_bank_transactions_type CHECK (transaction_type IN (
        'CUSTOMER_RECEIPT', 'CASH_SALE', 'INTERNAL_TRANSFER', 'LOAN_RECEIPT',
        'OWNER_DEPOSIT', 'CHECK_RECEIPT', 'REFUND', 'OTHER_RECEIPT',
        'DIRECT_EXPENSE', 'SUPPLIER_PAYMENT', 'PAYROLL_PAYMENT',
        'PETTY_CASH_FUNDING', 'LOAN_REPAYMENT', 'ASSET_PURCHASE',
        'CHECK_PAYMENT', 'OWNER_WITHDRAWAL', 'BANK_FEE', 'OTHER_PAYMENT'
    )),
    CONSTRAINT ck_bank_transactions_status CHECK (status IN (
        'NEW', 'CLASSIFIED', 'SYNC_PENDING', 'SYNCED', 'SYNC_FAILED', 'NEEDS_REVIEW', 'REVERSED'
    )),
    UNIQUE (company_id, id),
    UNIQUE (company_id, external_transaction_id)
);

CREATE INDEX IF NOT EXISTS idx_bank_transactions_company_occurred
    ON bank_transactions(company_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_bank_transactions_company_party
    ON bank_transactions(company_id, party_id, occurred_at DESC)
    WHERE party_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_bank_transactions_company_status
    ON bank_transactions(company_id, status, occurred_at DESC);

CREATE TABLE IF NOT EXISTS transaction_allocations (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    bank_transaction_id BIGINT NOT NULL,
    reference_type VARCHAR(40) NOT NULL DEFAULT 'INVOICE',
    reference_id VARCHAR(160) NOT NULL,
    amount NUMERIC(20,2) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_transaction_allocations_company_transaction
        FOREIGN KEY (company_id, bank_transaction_id)
        REFERENCES bank_transactions(company_id, id) ON DELETE CASCADE,
    CONSTRAINT ck_transaction_allocations_amount CHECK (amount > 0),
    UNIQUE (company_id, bank_transaction_id, reference_type, reference_id)
);

CREATE INDEX IF NOT EXISTS idx_transaction_allocations_company_reference
    ON transaction_allocations(company_id, reference_type, reference_id);

ALTER TABLE party_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE party_roles FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_party_roles ON party_roles;
CREATE POLICY tenant_isolation_party_roles ON party_roles
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

ALTER TABLE bank_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE bank_transactions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_bank_transactions ON bank_transactions;
CREATE POLICY tenant_isolation_bank_transactions ON bank_transactions
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

ALTER TABLE transaction_allocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE transaction_allocations FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_transaction_allocations ON transaction_allocations;
CREATE POLICY tenant_isolation_transaction_allocations ON transaction_allocations
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());
