CREATE TABLE IF NOT EXISTS boms (
    id BIGSERIAL PRIMARY KEY,
    product_item_id BIGINT NOT NULL REFERENCES items(id),
    version_no VARCHAR(10),
    std_waste_pct DECIMAL(5,2) DEFAULT 3.00,
    effective_from DATE,
    effective_to DATE,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bom_lines (
    id BIGSERIAL PRIMARY KEY,
    bom_id BIGINT NOT NULL REFERENCES boms(id),
    input_item_id BIGINT NOT NULL REFERENCES items(id),
    qty_per_unit DECIMAL(18,4),
    scrapable BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS machines (
    id BIGSERIAL PRIMARY KEY,
    branch_id BIGINT NOT NULL REFERENCES branches(id),
    code VARCHAR(20) NOT NULL,
    name VARCHAR(100),
    type VARCHAR(50),
    base_downtime_rate DECIMAL(18,2) DEFAULT 0,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS production_orders (
    id BIGSERIAL PRIMARY KEY,
    branch_id BIGINT NOT NULL REFERENCES branches(id),
    order_no VARCHAR(50) NOT NULL UNIQUE,
    customer_party_id BIGINT NOT NULL REFERENCES parties(id),
    contractor_party_id BIGINT REFERENCES parties(id),
    product_item_id BIGINT NOT NULL REFERENCES items(id),
    warehouse_id BIGINT NOT NULL REFERENCES warehouses(id),
    start_date TIMESTAMP,
    end_date TIMESTAMP,
    commitment_date TIMESTAMP,
    status VARCHAR(20) DEFAULT 'Draft',
    total_yarn_input DECIMAL(18,2),
    total_fabric_output DECIMAL(18,2),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by BIGINT
);

CREATE TABLE IF NOT EXISTS production_consumptions (
    id BIGSERIAL PRIMARY KEY,
    production_order_id BIGINT NOT NULL REFERENCES production_orders(id),
    item_id BIGINT NOT NULL REFERENCES items(id),
    warehouse_id BIGINT NOT NULL REFERENCES warehouses(id),
    lot_id BIGINT REFERENCES inventory_lots(id),
    qty DECIMAL(18,2),
    unit_cost DECIMAL(18,2),
    ownership_type VARCHAR(20),
    consumed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by BIGINT
);

CREATE TABLE IF NOT EXISTS production_outputs (
    id BIGSERIAL PRIMARY KEY,
    production_order_id BIGINT NOT NULL REFERENCES production_orders(id),
    item_id BIGINT NOT NULL REFERENCES items(id),
    warehouse_id BIGINT NOT NULL REFERENCES warehouses(id),
    lot_id BIGINT REFERENCES inventory_lots(id),
    qty_good DECIMAL(18,2),
    qty_scrap_std DECIMAL(18,2),
    qty_scrap_excess DECIMAL(18,2),
    production_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS machine_idle_penalties (
    id BIGSERIAL PRIMARY KEY,
    production_order_id BIGINT NOT NULL REFERENCES production_orders(id),
    machine_id BIGINT NOT NULL REFERENCES machines(id),
    penalty_type VARCHAR(10),
    duration_value DECIMAL(10,2),
    rate_per_unit DECIMAL(18,2),
    amount DECIMAL(18,2),
    reason VARCHAR(200),
    start_date TIMESTAMP,
    end_date TIMESTAMP,
    status VARCHAR(20) DEFAULT 'Pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS waste_allocations (
    id BIGSERIAL PRIMARY KEY,
    production_order_id BIGINT NOT NULL REFERENCES production_orders(id),
    excess_waste_qty DECIMAL(18,2),
    allocated_to_party_id BIGINT NOT NULL REFERENCES parties(id),
    debit_amount DECIMAL(18,2),
    basis VARCHAR(100),
    status VARCHAR(20) DEFAULT 'Pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
