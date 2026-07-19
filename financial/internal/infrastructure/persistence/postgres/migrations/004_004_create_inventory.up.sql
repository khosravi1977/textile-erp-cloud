CREATE TABLE IF NOT EXISTS inventory_lots (
    id BIGSERIAL PRIMARY KEY,
    item_id BIGINT NOT NULL REFERENCES items(id),
    warehouse_id BIGINT NOT NULL REFERENCES warehouses(id),
    owner_party_id BIGINT REFERENCES parties(id),
    lot_no VARCHAR(50),
    receipt_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expiry_date TIMESTAMP,
    qty_on_hand DECIMAL(18,2) NOT NULL DEFAULT 0,
    qty_reserved DECIMAL(18,2) NOT NULL DEFAULT 0,
    unit_cost DECIMAL(18,2),
    agreed_price DECIMAL(18,2),
    ownership_type VARCHAR(20) NOT NULL CHECK (ownership_type IN ('Owned','Custody')),
    source_doc_type VARCHAR(50),
    source_doc_id BIGINT,
    status VARCHAR(20) DEFAULT 'Active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS inventory_txns (
    id BIGSERIAL PRIMARY KEY,
    branch_id BIGINT NOT NULL REFERENCES branches(id),
    warehouse_id BIGINT NOT NULL REFERENCES warehouses(id),
    txn_type VARCHAR(20) NOT NULL CHECK (txn_type IN ('Receipt','Issue','Transfer','Adjustment')),
    txn_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ref_doc_type VARCHAR(50),
    ref_doc_id BIGINT,
    description VARCHAR(200),
    created_by BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS inventory_txn_lines (
    id BIGSERIAL PRIMARY KEY,
    inventory_txn_id BIGINT NOT NULL REFERENCES inventory_txns(id),
    item_id BIGINT NOT NULL REFERENCES items(id),
    lot_id BIGINT REFERENCES inventory_lots(id),
    qty_in DECIMAL(18,2) DEFAULT 0,
    qty_out DECIMAL(18,2) DEFAULT 0,
    unit_cost DECIMAL(18,2),
    ownership_type VARCHAR(20),
    owner_party_id BIGINT REFERENCES parties(id),
    related_production_id BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
