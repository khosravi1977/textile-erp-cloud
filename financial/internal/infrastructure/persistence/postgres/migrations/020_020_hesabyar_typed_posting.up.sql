-- HesabYar typed posting core: party roles, bank accounts, typed bank
-- transactions, invoice allocations and a migration review queue.
-- Amounts are stored as BIGINT in Toman (no floating point money).

-- 1) Party roles (additive, legacy parties.type stays untouched)
CREATE TABLE IF NOT EXISTS party_roles (
    party_id BIGINT NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    role VARCHAR(30) NOT NULL CHECK (role IN ('CUSTOMER','SUPPLIER','EMPLOYEE','PETTY_CASH_HOLDER','OWNER','OTHER')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (party_id, role)
);

INSERT INTO party_roles (party_id, role)
SELECT p.id,
       CASE p.type
           WHEN 'Customer' THEN 'CUSTOMER'
           WHEN 'Supplier' THEN 'SUPPLIER'
           ELSE 'OTHER'
       END
FROM parties p
WHERE NOT EXISTS (
    SELECT 1 FROM party_roles pr WHERE pr.party_id = p.id
)
ON CONFLICT DO NOTHING;

-- 2) Bank and cash accounts
CREATE TABLE IF NOT EXISTS bank_accounts (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    account_type VARCHAR(20) NOT NULL DEFAULT 'BANK' CHECK (account_type IN ('BANK','CASH')),
    opening_balance BIGINT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    source VARCHAR(20) NOT NULL DEFAULT 'ERP_MANUAL' CHECK (source IN ('ERP_MANUAL','MOBILE_IMPORT','MIGRATION')),
    external_ref VARCHAR(120),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (company_id, name)
);

CREATE INDEX IF NOT EXISTS idx_bank_accounts_company ON bank_accounts(company_id, is_active);

-- 3) Typed bank transactions (single source of truth for HesabYar money events)
CREATE TABLE IF NOT EXISTS bank_transactions (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    external_transaction_id VARCHAR(120) NOT NULL,
    idempotency_key VARCHAR(120),
    bank_account_id BIGINT NOT NULL REFERENCES bank_accounts(id),
    direction VARCHAR(3) NOT NULL CHECK (direction IN ('IN','OUT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    transaction_date DATE NOT NULL,
    transaction_time VARCHAR(10),
    transaction_type VARCHAR(30) NOT NULL CHECK (transaction_type IN (
        'CUSTOMER_RECEIPT','DIRECT_EXPENSE','SUPPLIER_PAYMENT','PAYROLL_PAYMENT',
        'INTERNAL_TRANSFER','PETTY_CASH_FUNDING','PETTY_CASH_RETURN','LOAN_RECEIPT',
        'LOAN_REPAYMENT','OWNER_DEPOSIT','OWNER_WITHDRAWAL','ASSET_PURCHASE',
        'BANK_FEE','CHECK_RECEIPT','CHECK_PAYMENT','REFUND','OTHER_RECEIPT','OTHER_PAYMENT'
    )),
    category_id BIGINT,
    subcategory_id BIGINT,
    party_id BIGINT REFERENCES parties(id),
    counter_account_id BIGINT REFERENCES bank_accounts(id),
    transfer_pair_id VARCHAR(60),
    interest_amount BIGINT CHECK (interest_amount IS NULL OR interest_amount >= 0),
    description VARCHAR(400),
    bank_reference VARCHAR(120),
    source VARCHAR(20) NOT NULL DEFAULT 'HESABYAR' CHECK (source IN ('HESABYAR','ERP_MANUAL','IMPORT','SYSTEM')),
    raw_source_reference VARCHAR(200),
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','VOIDED','REVERSED')),
    posting_status VARCHAR(20) NOT NULL DEFAULT 'POSTED' CHECK (posting_status IN ('PENDING','POSTED','FAILED','NEEDS_REVIEW')),
    journal_voucher_id BIGINT,
    reversal_of BIGINT REFERENCES bank_transactions(id),
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (company_id, external_transaction_id)
);

CREATE INDEX IF NOT EXISTS idx_bank_txn_company_date ON bank_transactions(company_id, transaction_date DESC);
CREATE INDEX IF NOT EXISTS idx_bank_txn_company_type ON bank_transactions(company_id, transaction_type);
CREATE INDEX IF NOT EXISTS idx_bank_txn_company_party ON bank_transactions(company_id, party_id);
CREATE INDEX IF NOT EXISTS idx_bank_txn_pair ON bank_transactions(company_id, transfer_pair_id);

-- 4) Payment allocations to documents/invoices
CREATE TABLE IF NOT EXISTS transaction_allocations (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    bank_transaction_id BIGINT NOT NULL REFERENCES bank_transactions(id) ON DELETE CASCADE,
    document_type VARCHAR(40) NOT NULL CHECK (document_type IN ('INVOICE','UNALLOCATED_CREDIT','OTHER_DOCUMENT')),
    document_id VARCHAR(60),
    party_id BIGINT NOT NULL REFERENCES parties(id),
    allocated_amount BIGINT NOT NULL CHECK (allocated_amount > 0),
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_txn_alloc_company_txn ON transaction_allocations(company_id, bank_transaction_id);
CREATE INDEX IF NOT EXISTS idx_txn_alloc_party ON transaction_allocations(company_id, party_id);

-- 5) Review queue for ambiguous legacy migration rows
CREATE TABLE IF NOT EXISTS migration_review_queue (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    source_table VARCHAR(80) NOT NULL,
    source_ref VARCHAR(120) NOT NULL,
    reason VARCHAR(200) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','RESOLVED','DISMISSED')),
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (company_id, source_table, source_ref)
);

-- 6) Chart-of-accounts extensions required by typed posting, per company
--    (codes follow the canonical chart used by the accounting engine, and the
--     engine upserts them again defensively at posting time).
INSERT INTO accounts (company_id, code, name, type, is_detail, is_active)
SELECT c.id, v.code, v.name, v.type, true, true
FROM companies c
CROSS JOIN (VALUES
    ('1150','تنخواه','Asset'),
    ('3250','برداشت مالک','Equity'),
    ('5910','هزینه کارمزد بانکی','Expense'),
    ('5920','هزینه حقوق و دستمزد','Expense'),
    ('5930','هزینه مالی و بهره','Expense')
) AS v(code, name, type)
ON CONFLICT (company_id, code) DO NOTHING;

-- 7) Tenant isolation for all new tables
ALTER TABLE party_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE bank_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE bank_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE transaction_allocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE migration_review_queue ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_party_roles ON party_roles;
CREATE POLICY tenant_isolation_party_roles ON party_roles
    USING (EXISTS (SELECT 1 FROM parties p WHERE p.id = party_id AND p.company_id = current_app_company_id()))
    WITH CHECK (EXISTS (SELECT 1 FROM parties p WHERE p.id = party_id AND p.company_id = current_app_company_id()));

DROP POLICY IF EXISTS tenant_isolation_bank_accounts ON bank_accounts;
CREATE POLICY tenant_isolation_bank_accounts ON bank_accounts
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_bank_transactions ON bank_transactions;
CREATE POLICY tenant_isolation_bank_transactions ON bank_transactions
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_transaction_allocations ON transaction_allocations;
CREATE POLICY tenant_isolation_transaction_allocations ON transaction_allocations
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_migration_review_queue ON migration_review_queue;
CREATE POLICY tenant_isolation_migration_review_queue ON migration_review_queue
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());
