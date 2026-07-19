-- Tenant isolation guardrails for PostgreSQL.
-- Application code should set app.company_id per connection/transaction before
-- enabling FORCE ROW LEVEL SECURITY in production.

CREATE OR REPLACE FUNCTION current_app_company_id()
RETURNS BIGINT AS $$
BEGIN
    RETURN NULLIF(current_setting('app.company_id', true), '')::BIGINT;
EXCEPTION WHEN invalid_text_representation THEN
    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

ALTER TABLE IF EXISTS branches ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS parties ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS customer_credit_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS inventory_lots ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS inventory_txns ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS inventory_txn_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS production_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS production_consumptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS production_outputs ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS machine_idle_penalties ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS waste_allocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS journal_vouchers ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS journal_voucher_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS ar_ap_balances ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS settlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS settlement_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS commission_invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS financial_users ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS user_module_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE IF EXISTS audit_logs ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_branches ON branches;
CREATE POLICY tenant_isolation_branches ON branches
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_parties ON parties;
CREATE POLICY tenant_isolation_parties ON parties
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_customer_credit_profiles ON customer_credit_profiles;
CREATE POLICY tenant_isolation_customer_credit_profiles ON customer_credit_profiles
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_inventory_lots ON inventory_lots;
CREATE POLICY tenant_isolation_inventory_lots ON inventory_lots
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_inventory_txns ON inventory_txns;
CREATE POLICY tenant_isolation_inventory_txns ON inventory_txns
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_inventory_txn_lines ON inventory_txn_lines;
CREATE POLICY tenant_isolation_inventory_txn_lines ON inventory_txn_lines
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_production_orders ON production_orders;
CREATE POLICY tenant_isolation_production_orders ON production_orders
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_production_consumptions ON production_consumptions;
CREATE POLICY tenant_isolation_production_consumptions ON production_consumptions
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_production_outputs ON production_outputs;
CREATE POLICY tenant_isolation_production_outputs ON production_outputs
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_machine_idle_penalties ON machine_idle_penalties;
CREATE POLICY tenant_isolation_machine_idle_penalties ON machine_idle_penalties
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_waste_allocations ON waste_allocations;
CREATE POLICY tenant_isolation_waste_allocations ON waste_allocations
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_accounts ON accounts;
CREATE POLICY tenant_isolation_accounts ON accounts
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_journal_vouchers ON journal_vouchers;
CREATE POLICY tenant_isolation_journal_vouchers ON journal_vouchers
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_journal_voucher_lines ON journal_voucher_lines;
CREATE POLICY tenant_isolation_journal_voucher_lines ON journal_voucher_lines
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_ar_ap_balances ON ar_ap_balances;
CREATE POLICY tenant_isolation_ar_ap_balances ON ar_ap_balances
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_settlements ON settlements;
CREATE POLICY tenant_isolation_settlements ON settlements
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_settlement_lines ON settlement_lines;
CREATE POLICY tenant_isolation_settlement_lines ON settlement_lines
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_commission_invoices ON commission_invoices;
CREATE POLICY tenant_isolation_commission_invoices ON commission_invoices
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_financial_users ON financial_users;
CREATE POLICY tenant_isolation_financial_users ON financial_users
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_user_module_access ON user_module_access;
CREATE POLICY tenant_isolation_user_module_access ON user_module_access
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

DROP POLICY IF EXISTS tenant_isolation_audit_logs ON audit_logs;
CREATE POLICY tenant_isolation_audit_logs ON audit_logs
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());
