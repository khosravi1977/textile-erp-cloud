DROP POLICY IF EXISTS tenant_isolation_migration_review_queue ON migration_review_queue;
DROP POLICY IF EXISTS tenant_isolation_transaction_allocations ON transaction_allocations;
DROP POLICY IF EXISTS tenant_isolation_bank_transactions ON bank_transactions;
DROP POLICY IF EXISTS tenant_isolation_bank_accounts ON bank_accounts;
DROP POLICY IF EXISTS tenant_isolation_party_roles ON party_roles;

DROP TABLE IF EXISTS migration_review_queue;
DROP TABLE IF EXISTS transaction_allocations;
DROP TABLE IF EXISTS bank_transactions;
DROP TABLE IF EXISTS bank_accounts;
DROP TABLE IF EXISTS party_roles;

DELETE FROM accounts WHERE code IN ('1150','3250','5910','5920','5930');
