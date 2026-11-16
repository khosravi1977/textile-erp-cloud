-- Intentionally no automatic data/unit rollback. Stop writers and reconcile
-- financial_currency_audits before a coordinated code + data rollback.
SELECT 1;
