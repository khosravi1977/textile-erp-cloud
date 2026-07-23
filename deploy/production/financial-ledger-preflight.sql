\set ON_ERROR_STOP on

BEGIN;
SET LOCAL TRANSACTION READ ONLY;
SET LOCAL row_security = off;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM accounts GROUP BY company_id, code HAVING COUNT(*) > 1) THEN
        RAISE EXCEPTION 'duplicate account codes exist inside a company';
    END IF;
    IF EXISTS (SELECT 1 FROM branches GROUP BY company_id, code HAVING COUNT(*) > 1) THEN
        RAISE EXCEPTION 'duplicate branch codes exist inside a company';
    END IF;
    IF EXISTS (SELECT 1 FROM parties GROUP BY company_id, code HAVING COUNT(*) > 1) THEN
        RAISE EXCEPTION 'duplicate party codes exist inside a company';
    END IF;
    IF EXISTS (SELECT 1 FROM parties GROUP BY company_id, lower(name) HAVING COUNT(*) > 1) THEN
        RAISE EXCEPTION 'duplicate party names exist inside a company';
    END IF;
    IF EXISTS (
        SELECT 1 FROM journal_voucher_lines
        WHERE debit < 0 OR credit < 0 OR NOT ((debit > 0 AND credit = 0) OR (credit > 0 AND debit = 0))
    ) THEN
        RAISE EXCEPTION 'invalid debit/credit journal lines exist';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM journal_vouchers v
        LEFT JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id
        WHERE v.status='Posted'
        GROUP BY v.id
        HAVING COUNT(l.id) < 2 OR COALESCE(SUM(l.debit),0) <= 0 OR COALESCE(SUM(l.debit),0) <> COALESCE(SUM(l.credit),0)
    ) THEN
        RAISE EXCEPTION 'unbalanced or empty posted journal vouchers exist';
    END IF;
END;
$$;

SELECT 'companies' AS metric, COUNT(*) AS value FROM companies
UNION ALL SELECT 'workspace_states', COUNT(*) FROM financial_workspace_states
UNION ALL SELECT 'workspace_history', COUNT(*) FROM financial_workspace_history
UNION ALL SELECT 'journal_vouchers', COUNT(*) FROM journal_vouchers
UNION ALL SELECT 'journal_lines', COUNT(*) FROM journal_voucher_lines
UNION ALL SELECT 'inventory_transactions', COUNT(*) FROM inventory_txns
ORDER BY metric;

ROLLBACK;
