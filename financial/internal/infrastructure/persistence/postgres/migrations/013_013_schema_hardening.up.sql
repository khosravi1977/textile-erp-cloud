ALTER TABLE items
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE warehouses
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE boms
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE bom_lines
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE machines
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE tax_invoices
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE credit_score_logs
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);
ALTER TABLE credit_alerts
    ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id);

UPDATE branches
SET company_id = COALESCE(company_id, 1);
UPDATE parties
SET company_id = COALESCE(company_id, 1);
UPDATE customer_credit_profiles ccp
SET company_id = COALESCE(ccp.company_id, p.company_id, 1)
FROM parties p
WHERE ccp.party_id = p.id
  AND (ccp.company_id IS NULL OR ccp.company_id <> p.company_id);
UPDATE items
SET company_id = COALESCE(company_id, 1);
UPDATE warehouses w
SET company_id = COALESCE(w.company_id, b.company_id, 1)
FROM branches b
WHERE w.branch_id = b.id
  AND (w.company_id IS NULL OR w.company_id <> b.company_id);
UPDATE inventory_lots il
SET company_id = COALESCE(
    il.company_id,
    w.company_id,
    (SELECT p.company_id FROM parties p WHERE p.id = il.owner_party_id),
    1
)
FROM warehouses w
WHERE il.warehouse_id = w.id
  AND (
      il.company_id IS NULL OR
      il.company_id <> COALESCE(
          w.company_id,
          (SELECT p.company_id FROM parties p WHERE p.id = il.owner_party_id),
          1
      )
  );
UPDATE inventory_txns it
SET company_id = COALESCE(it.company_id, b.company_id, 1)
FROM branches b
WHERE it.branch_id = b.id
  AND (it.company_id IS NULL OR it.company_id <> b.company_id);
UPDATE inventory_txn_lines itl
SET company_id = COALESCE(itl.company_id, it.company_id, 1)
FROM inventory_txns it
WHERE itl.inventory_txn_id = it.id
  AND (itl.company_id IS NULL OR itl.company_id <> it.company_id);
UPDATE boms b
SET company_id = COALESCE(b.company_id, i.company_id, 1)
FROM items i
WHERE b.product_item_id = i.id
  AND (b.company_id IS NULL OR b.company_id <> i.company_id);
UPDATE bom_lines bl
SET company_id = COALESCE(
    bl.company_id,
    b.company_id,
    (SELECT i.company_id FROM items i WHERE i.id = bl.input_item_id),
    1
)
FROM boms b
WHERE bl.bom_id = b.id
  AND (
      bl.company_id IS NULL OR
      bl.company_id <> COALESCE(
          b.company_id,
          (SELECT i.company_id FROM items i WHERE i.id = bl.input_item_id),
          1
      )
  );
UPDATE machines m
SET company_id = COALESCE(m.company_id, b.company_id, 1)
FROM branches b
WHERE m.branch_id = b.id
  AND (m.company_id IS NULL OR m.company_id <> b.company_id);
UPDATE production_orders po
SET company_id = COALESCE(
    po.company_id,
    (SELECT w.company_id FROM warehouses w WHERE w.id = po.warehouse_id),
    (SELECT p.company_id FROM parties p WHERE p.id = po.customer_party_id),
    (SELECT b.company_id FROM branches b WHERE b.id = po.branch_id),
    1
)
WHERE po.company_id IS NULL
   OR po.company_id <> COALESCE(
        (SELECT w.company_id FROM warehouses w WHERE w.id = po.warehouse_id),
        (SELECT p.company_id FROM parties p WHERE p.id = po.customer_party_id),
        (SELECT b.company_id FROM branches b WHERE b.id = po.branch_id),
        1
   );
UPDATE production_consumptions pc
SET company_id = COALESCE(pc.company_id, po.company_id, 1)
FROM production_orders po
WHERE pc.production_order_id = po.id
  AND (pc.company_id IS NULL OR pc.company_id <> po.company_id);
UPDATE production_outputs po2
SET company_id = COALESCE(po2.company_id, po.company_id, 1)
FROM production_orders po
WHERE po2.production_order_id = po.id
  AND (po2.company_id IS NULL OR po2.company_id <> po.company_id);
UPDATE machine_idle_penalties mip
SET company_id = COALESCE(mip.company_id, po.company_id, 1)
FROM production_orders po
WHERE mip.production_order_id = po.id
  AND (mip.company_id IS NULL OR mip.company_id <> po.company_id);
UPDATE waste_allocations wa
SET company_id = COALESCE(
    wa.company_id,
    po.company_id,
    (SELECT p.company_id FROM parties p WHERE p.id = wa.allocated_to_party_id),
    1
)
FROM production_orders po
WHERE wa.production_order_id = po.id
  AND (
      wa.company_id IS NULL OR
      wa.company_id <> COALESCE(
          po.company_id,
          (SELECT p.company_id FROM parties p WHERE p.id = wa.allocated_to_party_id),
          1
      )
  );
UPDATE accounts
SET company_id = COALESCE(company_id, 1);
UPDATE journal_vouchers jv
SET company_id = COALESCE(jv.company_id, b.company_id, 1)
FROM branches b
WHERE jv.branch_id = b.id
  AND (jv.company_id IS NULL OR jv.company_id <> b.company_id);
UPDATE journal_voucher_lines jvl
SET company_id = COALESCE(
    jvl.company_id,
    jv.company_id,
    (SELECT p.company_id FROM parties p WHERE p.id = jvl.party_id),
    1
)
FROM journal_vouchers jv
WHERE jvl.journal_voucher_id = jv.id
  AND (
      jvl.company_id IS NULL OR
      jvl.company_id <> COALESCE(
          jv.company_id,
          (SELECT p.company_id FROM parties p WHERE p.id = jvl.party_id),
          1
      )
  );
UPDATE ar_ap_balances aab
SET company_id = COALESCE(aab.company_id, p.company_id, 1)
FROM parties p
WHERE aab.party_id = p.id
  AND (aab.company_id IS NULL OR aab.company_id <> p.company_id);
UPDATE settlements s
SET company_id = COALESCE(
    s.company_id,
    p.company_id,
    (SELECT b.company_id FROM branches b WHERE b.id = s.branch_id),
    1
)
FROM parties p
WHERE s.party_id = p.id
  AND (
      s.company_id IS NULL OR
      s.company_id <> COALESCE(
          p.company_id,
          (SELECT b.company_id FROM branches b WHERE b.id = s.branch_id),
          1
      )
  );
UPDATE settlement_lines sl
SET company_id = COALESCE(
    sl.company_id,
    s.company_id,
    (SELECT i.company_id FROM items i WHERE i.id = sl.item_id),
    1
)
FROM settlements s
WHERE sl.settlement_id = s.id
  AND (
      sl.company_id IS NULL OR
      sl.company_id <> COALESCE(
          s.company_id,
          (SELECT i.company_id FROM items i WHERE i.id = sl.item_id),
          1
      )
  );
UPDATE commission_invoices ci
SET company_id = COALESCE(
    ci.company_id,
    po.company_id,
    (SELECT p.company_id FROM parties p WHERE p.id = ci.party_id),
    (SELECT b.company_id FROM branches b WHERE b.id = ci.branch_id),
    1
)
FROM production_orders po
WHERE ci.production_order_id = po.id
  AND (
      ci.company_id IS NULL OR
      ci.company_id <> COALESCE(
          po.company_id,
          (SELECT p.company_id FROM parties p WHERE p.id = ci.party_id),
          (SELECT b.company_id FROM branches b WHERE b.id = ci.branch_id),
          1
      )
  );
UPDATE tax_invoices ti
SET company_id = COALESCE(ti.company_id, ci.company_id, 1)
FROM commission_invoices ci
WHERE ti.commission_invoice_id = ci.id
  AND (ti.company_id IS NULL OR ti.company_id <> ci.company_id);
UPDATE credit_score_logs csl
SET company_id = COALESCE(csl.company_id, p.company_id, 1)
FROM parties p
WHERE csl.party_id = p.id
  AND (csl.company_id IS NULL OR csl.company_id <> p.company_id);
UPDATE credit_alerts ca
SET company_id = COALESCE(ca.company_id, p.company_id, 1)
FROM parties p
WHERE ca.party_id = p.id
  AND (ca.company_id IS NULL OR ca.company_id <> p.company_id);
UPDATE financial_users
SET company_id = COALESCE(company_id, 1);
UPDATE user_module_access uma
SET company_id = COALESCE(uma.company_id, fu.company_id, 1)
FROM financial_users fu
WHERE uma.user_id = fu.id
  AND (uma.company_id IS NULL OR uma.company_id <> fu.company_id);
UPDATE audit_logs
SET company_id = COALESCE(company_id, 1);

ALTER TABLE branches ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE parties ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE customer_credit_profiles ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE items ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE warehouses ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE inventory_lots ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE inventory_txns ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE inventory_txn_lines ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE boms ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE bom_lines ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE machines ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE production_orders ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE production_consumptions ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE production_outputs ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE machine_idle_penalties ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE waste_allocations ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE accounts ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE journal_vouchers ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE journal_voucher_lines ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE ar_ap_balances ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE settlements ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE settlement_lines ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE commission_invoices ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE tax_invoices ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE credit_score_logs ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE credit_alerts ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE financial_users ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE user_module_access ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE audit_logs ALTER COLUMN company_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_customer_credit_profiles_party_id ON customer_credit_profiles(party_id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_branches_company_id_id ON branches(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_parties_company_id_id ON parties(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_customer_credit_profiles_company_id_id ON customer_credit_profiles(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_items_company_id_id ON items(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_warehouses_company_id_id ON warehouses(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_inventory_lots_company_id_id ON inventory_lots(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_inventory_txns_company_id_id ON inventory_txns(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_boms_company_id_id ON boms(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_machines_company_id_id ON machines(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_production_orders_company_id_id ON production_orders(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_accounts_company_id_id ON accounts(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_journal_vouchers_company_id_id ON journal_vouchers(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_settlements_company_id_id ON settlements(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_commission_invoices_company_id_id ON commission_invoices(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_financial_users_company_id_id ON financial_users(company_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_machines_company_code ON machines(company_id, code);

CREATE INDEX IF NOT EXISTS idx_items_company_id ON items(company_id);
CREATE INDEX IF NOT EXISTS idx_warehouses_company_id ON warehouses(company_id);
CREATE INDEX IF NOT EXISTS idx_boms_company_id ON boms(company_id);
CREATE INDEX IF NOT EXISTS idx_bom_lines_company_id ON bom_lines(company_id);
CREATE INDEX IF NOT EXISTS idx_machines_company_id ON machines(company_id);
CREATE INDEX IF NOT EXISTS idx_tax_invoices_company_id ON tax_invoices(company_id);
CREATE INDEX IF NOT EXISTS idx_credit_score_logs_company_id ON credit_score_logs(company_id);
CREATE INDEX IF NOT EXISTS idx_credit_alerts_company_id ON credit_alerts(company_id);
CREATE INDEX IF NOT EXISTS idx_financial_users_company_id_required ON financial_users(company_id);
CREATE INDEX IF NOT EXISTS idx_user_module_access_company_id ON user_module_access(company_id);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_customer_credit_profiles_company_party') THEN
        ALTER TABLE customer_credit_profiles
            ADD CONSTRAINT fk_customer_credit_profiles_company_party
            FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_warehouses_company_branch') THEN
        ALTER TABLE warehouses
            ADD CONSTRAINT fk_warehouses_company_branch
            FOREIGN KEY (company_id, branch_id) REFERENCES branches(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_lots_company_item') THEN
        ALTER TABLE inventory_lots
            ADD CONSTRAINT fk_inventory_lots_company_item
            FOREIGN KEY (company_id, item_id) REFERENCES items(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_lots_company_warehouse') THEN
        ALTER TABLE inventory_lots
            ADD CONSTRAINT fk_inventory_lots_company_warehouse
            FOREIGN KEY (company_id, warehouse_id) REFERENCES warehouses(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_lots_company_owner_party') THEN
        ALTER TABLE inventory_lots
            ADD CONSTRAINT fk_inventory_lots_company_owner_party
            FOREIGN KEY (company_id, owner_party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_txns_company_branch') THEN
        ALTER TABLE inventory_txns
            ADD CONSTRAINT fk_inventory_txns_company_branch
            FOREIGN KEY (company_id, branch_id) REFERENCES branches(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_txn_lines_company_txn') THEN
        ALTER TABLE inventory_txn_lines
            ADD CONSTRAINT fk_inventory_txn_lines_company_txn
            FOREIGN KEY (company_id, inventory_txn_id) REFERENCES inventory_txns(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_txn_lines_company_item') THEN
        ALTER TABLE inventory_txn_lines
            ADD CONSTRAINT fk_inventory_txn_lines_company_item
            FOREIGN KEY (company_id, item_id) REFERENCES items(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_txn_lines_company_lot') THEN
        ALTER TABLE inventory_txn_lines
            ADD CONSTRAINT fk_inventory_txn_lines_company_lot
            FOREIGN KEY (company_id, lot_id) REFERENCES inventory_lots(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_txn_lines_company_owner_party') THEN
        ALTER TABLE inventory_txn_lines
            ADD CONSTRAINT fk_inventory_txn_lines_company_owner_party
            FOREIGN KEY (company_id, owner_party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_boms_company_product_item') THEN
        ALTER TABLE boms
            ADD CONSTRAINT fk_boms_company_product_item
            FOREIGN KEY (company_id, product_item_id) REFERENCES items(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_bom_lines_company_bom') THEN
        ALTER TABLE bom_lines
            ADD CONSTRAINT fk_bom_lines_company_bom
            FOREIGN KEY (company_id, bom_id) REFERENCES boms(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_bom_lines_company_input_item') THEN
        ALTER TABLE bom_lines
            ADD CONSTRAINT fk_bom_lines_company_input_item
            FOREIGN KEY (company_id, input_item_id) REFERENCES items(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_machines_company_branch') THEN
        ALTER TABLE machines
            ADD CONSTRAINT fk_machines_company_branch
            FOREIGN KEY (company_id, branch_id) REFERENCES branches(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_orders_company_branch') THEN
        ALTER TABLE production_orders
            ADD CONSTRAINT fk_production_orders_company_branch
            FOREIGN KEY (company_id, branch_id) REFERENCES branches(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_orders_company_customer_party') THEN
        ALTER TABLE production_orders
            ADD CONSTRAINT fk_production_orders_company_customer_party
            FOREIGN KEY (company_id, customer_party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_orders_company_contractor_party') THEN
        ALTER TABLE production_orders
            ADD CONSTRAINT fk_production_orders_company_contractor_party
            FOREIGN KEY (company_id, contractor_party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_orders_company_product_item') THEN
        ALTER TABLE production_orders
            ADD CONSTRAINT fk_production_orders_company_product_item
            FOREIGN KEY (company_id, product_item_id) REFERENCES items(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_orders_company_warehouse') THEN
        ALTER TABLE production_orders
            ADD CONSTRAINT fk_production_orders_company_warehouse
            FOREIGN KEY (company_id, warehouse_id) REFERENCES warehouses(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_consumptions_company_order') THEN
        ALTER TABLE production_consumptions
            ADD CONSTRAINT fk_production_consumptions_company_order
            FOREIGN KEY (company_id, production_order_id) REFERENCES production_orders(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_consumptions_company_item') THEN
        ALTER TABLE production_consumptions
            ADD CONSTRAINT fk_production_consumptions_company_item
            FOREIGN KEY (company_id, item_id) REFERENCES items(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_consumptions_company_warehouse') THEN
        ALTER TABLE production_consumptions
            ADD CONSTRAINT fk_production_consumptions_company_warehouse
            FOREIGN KEY (company_id, warehouse_id) REFERENCES warehouses(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_consumptions_company_lot') THEN
        ALTER TABLE production_consumptions
            ADD CONSTRAINT fk_production_consumptions_company_lot
            FOREIGN KEY (company_id, lot_id) REFERENCES inventory_lots(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_outputs_company_order') THEN
        ALTER TABLE production_outputs
            ADD CONSTRAINT fk_production_outputs_company_order
            FOREIGN KEY (company_id, production_order_id) REFERENCES production_orders(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_outputs_company_item') THEN
        ALTER TABLE production_outputs
            ADD CONSTRAINT fk_production_outputs_company_item
            FOREIGN KEY (company_id, item_id) REFERENCES items(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_outputs_company_warehouse') THEN
        ALTER TABLE production_outputs
            ADD CONSTRAINT fk_production_outputs_company_warehouse
            FOREIGN KEY (company_id, warehouse_id) REFERENCES warehouses(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_production_outputs_company_lot') THEN
        ALTER TABLE production_outputs
            ADD CONSTRAINT fk_production_outputs_company_lot
            FOREIGN KEY (company_id, lot_id) REFERENCES inventory_lots(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_machine_idle_penalties_company_order') THEN
        ALTER TABLE machine_idle_penalties
            ADD CONSTRAINT fk_machine_idle_penalties_company_order
            FOREIGN KEY (company_id, production_order_id) REFERENCES production_orders(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_machine_idle_penalties_company_machine') THEN
        ALTER TABLE machine_idle_penalties
            ADD CONSTRAINT fk_machine_idle_penalties_company_machine
            FOREIGN KEY (company_id, machine_id) REFERENCES machines(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_waste_allocations_company_order') THEN
        ALTER TABLE waste_allocations
            ADD CONSTRAINT fk_waste_allocations_company_order
            FOREIGN KEY (company_id, production_order_id) REFERENCES production_orders(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_waste_allocations_company_party') THEN
        ALTER TABLE waste_allocations
            ADD CONSTRAINT fk_waste_allocations_company_party
            FOREIGN KEY (company_id, allocated_to_party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_journal_vouchers_company_branch') THEN
        ALTER TABLE journal_vouchers
            ADD CONSTRAINT fk_journal_vouchers_company_branch
            FOREIGN KEY (company_id, branch_id) REFERENCES branches(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_journal_voucher_lines_company_voucher') THEN
        ALTER TABLE journal_voucher_lines
            ADD CONSTRAINT fk_journal_voucher_lines_company_voucher
            FOREIGN KEY (company_id, journal_voucher_id) REFERENCES journal_vouchers(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_journal_voucher_lines_company_account') THEN
        ALTER TABLE journal_voucher_lines
            ADD CONSTRAINT fk_journal_voucher_lines_company_account
            FOREIGN KEY (company_id, account_id) REFERENCES accounts(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_journal_voucher_lines_company_party') THEN
        ALTER TABLE journal_voucher_lines
            ADD CONSTRAINT fk_journal_voucher_lines_company_party
            FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_ar_ap_balances_company_party') THEN
        ALTER TABLE ar_ap_balances
            ADD CONSTRAINT fk_ar_ap_balances_company_party
            FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_settlements_company_branch') THEN
        ALTER TABLE settlements
            ADD CONSTRAINT fk_settlements_company_branch
            FOREIGN KEY (company_id, branch_id) REFERENCES branches(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_settlements_company_party') THEN
        ALTER TABLE settlements
            ADD CONSTRAINT fk_settlements_company_party
            FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_settlement_lines_company_settlement') THEN
        ALTER TABLE settlement_lines
            ADD CONSTRAINT fk_settlement_lines_company_settlement
            FOREIGN KEY (company_id, settlement_id) REFERENCES settlements(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_settlement_lines_company_item') THEN
        ALTER TABLE settlement_lines
            ADD CONSTRAINT fk_settlement_lines_company_item
            FOREIGN KEY (company_id, item_id) REFERENCES items(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_commission_invoices_company_branch') THEN
        ALTER TABLE commission_invoices
            ADD CONSTRAINT fk_commission_invoices_company_branch
            FOREIGN KEY (company_id, branch_id) REFERENCES branches(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_commission_invoices_company_party') THEN
        ALTER TABLE commission_invoices
            ADD CONSTRAINT fk_commission_invoices_company_party
            FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_commission_invoices_company_order') THEN
        ALTER TABLE commission_invoices
            ADD CONSTRAINT fk_commission_invoices_company_order
            FOREIGN KEY (company_id, production_order_id) REFERENCES production_orders(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_tax_invoices_company_invoice') THEN
        ALTER TABLE tax_invoices
            ADD CONSTRAINT fk_tax_invoices_company_invoice
            FOREIGN KEY (company_id, commission_invoice_id) REFERENCES commission_invoices(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_credit_score_logs_company_party') THEN
        ALTER TABLE credit_score_logs
            ADD CONSTRAINT fk_credit_score_logs_company_party
            FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_credit_alerts_company_party') THEN
        ALTER TABLE credit_alerts
            ADD CONSTRAINT fk_credit_alerts_company_party
            FOREIGN KEY (company_id, party_id) REFERENCES parties(company_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_user_module_access_company_user') THEN
        ALTER TABLE user_module_access
            ADD CONSTRAINT fk_user_module_access_company_user
            FOREIGN KEY (company_id, user_id) REFERENCES financial_users(company_id, id) ON DELETE CASCADE;
    END IF;
END;
$$;

ALTER TABLE IF EXISTS items ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS warehouses ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS boms ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS bom_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS machines ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS tax_invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS credit_score_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS credit_alerts ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_items ON items;
CREATE POLICY tenant_isolation_items ON items
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_warehouses ON warehouses;
CREATE POLICY tenant_isolation_warehouses ON warehouses
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_boms ON boms;
CREATE POLICY tenant_isolation_boms ON boms
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_bom_lines ON bom_lines;
CREATE POLICY tenant_isolation_bom_lines ON bom_lines
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_machines ON machines;
CREATE POLICY tenant_isolation_machines ON machines
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_tax_invoices ON tax_invoices;
CREATE POLICY tenant_isolation_tax_invoices ON tax_invoices
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_credit_score_logs ON credit_score_logs;
CREATE POLICY tenant_isolation_credit_score_logs ON credit_score_logs
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_credit_alerts ON credit_alerts;
CREATE POLICY tenant_isolation_credit_alerts ON credit_alerts
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

ALTER TABLE branches FORCE ROW LEVEL SECURITY;
ALTER TABLE parties FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_credit_profiles FORCE ROW LEVEL SECURITY;
ALTER TABLE items FORCE ROW LEVEL SECURITY;
ALTER TABLE warehouses FORCE ROW LEVEL SECURITY;
ALTER TABLE inventory_lots FORCE ROW LEVEL SECURITY;
ALTER TABLE inventory_txns FORCE ROW LEVEL SECURITY;
ALTER TABLE inventory_txn_lines FORCE ROW LEVEL SECURITY;
ALTER TABLE boms FORCE ROW LEVEL SECURITY;
ALTER TABLE bom_lines FORCE ROW LEVEL SECURITY;
ALTER TABLE machines FORCE ROW LEVEL SECURITY;
ALTER TABLE production_orders FORCE ROW LEVEL SECURITY;
ALTER TABLE production_consumptions FORCE ROW LEVEL SECURITY;
ALTER TABLE production_outputs FORCE ROW LEVEL SECURITY;
ALTER TABLE machine_idle_penalties FORCE ROW LEVEL SECURITY;
ALTER TABLE waste_allocations FORCE ROW LEVEL SECURITY;
ALTER TABLE accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE journal_vouchers FORCE ROW LEVEL SECURITY;
ALTER TABLE journal_voucher_lines FORCE ROW LEVEL SECURITY;
ALTER TABLE ar_ap_balances FORCE ROW LEVEL SECURITY;
ALTER TABLE settlements FORCE ROW LEVEL SECURITY;
ALTER TABLE settlement_lines FORCE ROW LEVEL SECURITY;
ALTER TABLE commission_invoices FORCE ROW LEVEL SECURITY;
ALTER TABLE tax_invoices FORCE ROW LEVEL SECURITY;
ALTER TABLE credit_score_logs FORCE ROW LEVEL SECURITY;
ALTER TABLE credit_alerts FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_users NO FORCE ROW LEVEL SECURITY;
ALTER TABLE user_module_access NO FORCE ROW LEVEL SECURITY;
