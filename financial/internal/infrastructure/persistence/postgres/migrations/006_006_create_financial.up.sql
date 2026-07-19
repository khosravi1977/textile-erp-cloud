CREATE TABLE IF NOT EXISTS accounts (
    id BIGSERIAL PRIMARY KEY,
    parent_account_id BIGINT REFERENCES accounts(id),
    code VARCHAR(20) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    type VARCHAR(20),
    is_detail BOOLEAN DEFAULT true,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS journal_vouchers (
    id BIGSERIAL PRIMARY KEY,
    branch_id BIGINT NOT NULL REFERENCES branches(id),
    voucher_no VARCHAR(50) NOT NULL,
    voucher_date DATE DEFAULT CURRENT_DATE,
    description VARCHAR(200),
    source_doc_type VARCHAR(50),
    source_doc_id BIGINT,
    status VARCHAR(20) DEFAULT 'Draft',
    created_by BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    posted_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS journal_voucher_lines (
    id BIGSERIAL PRIMARY KEY,
    journal_voucher_id BIGINT NOT NULL REFERENCES journal_vouchers(id),
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    party_id BIGINT REFERENCES parties(id),
    sub_project_id BIGINT,
    debit DECIMAL(18,2) DEFAULT 0,
    credit DECIMAL(18,2) DEFAULT 0,
    description VARCHAR(200),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ar_ap_balances (
    id BIGSERIAL PRIMARY KEY,
    party_id BIGINT NOT NULL REFERENCES parties(id),
    sub_project_id BIGINT,
    debit_balance DECIMAL(18,2) DEFAULT 0,
    credit_balance DECIMAL(18,2) DEFAULT 0,
    last_recalc_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settlements (
    id BIGSERIAL PRIMARY KEY,
    branch_id BIGINT NOT NULL REFERENCES branches(id),
    party_id BIGINT NOT NULL REFERENCES parties(id),
    settlement_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    total_amount DECIMAL(18,2) DEFAULT 0,
    status VARCHAR(20) DEFAULT 'Draft',
    created_by BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settlement_lines (
    id BIGSERIAL PRIMARY KEY,
    settlement_id BIGINT NOT NULL REFERENCES settlements(id),
    settlement_type VARCHAR(20) NOT NULL,
    reference_doc_type VARCHAR(50),
    reference_doc_id BIGINT,
    amount DECIMAL(18,2),
    item_id BIGINT REFERENCES items(id),
    qty DECIMAL(18,2),
    unit_price DECIMAL(18,2),
    check_no VARCHAR(30),
    check_due_date DATE,
    bank_name VARCHAR(50),
    from_sub_project_id BIGINT,
    to_sub_project_id BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS commission_invoices (
    id BIGSERIAL PRIMARY KEY,
    branch_id BIGINT NOT NULL REFERENCES branches(id),
    invoice_no VARCHAR(50) NOT NULL UNIQUE,
    party_id BIGINT NOT NULL REFERENCES parties(id),
    production_order_id BIGINT NOT NULL REFERENCES production_orders(id),
    labor_amount DECIMAL(18,2) DEFAULT 0,
    machine_idle_penalty_amount DECIMAL(18,2) DEFAULT 0,
    waste_debit_amount DECIMAL(18,2) DEFAULT 0,
    discount DECIMAL(18,2) DEFAULT 0,
    total_amount DECIMAL(18,2) DEFAULT 0,
    tax_amount DECIMAL(18,2) DEFAULT 0,
    net_amount DECIMAL(18,2) DEFAULT 0,
    status VARCHAR(20) DEFAULT 'Draft',
    issued_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    due_date DATE,
    created_by BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
