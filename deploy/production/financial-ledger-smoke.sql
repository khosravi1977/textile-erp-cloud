\set ON_ERROR_STOP on

BEGIN;
SET LOCAL row_security = off;

INSERT INTO companies(code, name)
VALUES ('LEDGER-SMOKE', 'Ledger migration smoke test')
ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name
RETURNING id AS smoke_company_id \gset

INSERT INTO branches(company_id, code, name)
VALUES (:smoke_company_id, 'MAIN', 'Smoke branch')
ON CONFLICT (company_id, code) DO UPDATE SET name=EXCLUDED.name
RETURNING id AS smoke_branch_id \gset

INSERT INTO accounts(company_id, code, name, type, is_detail, is_active)
VALUES (:smoke_company_id, '1110', 'Smoke cash', 'Asset', true, true)
ON CONFLICT (company_id, code) DO UPDATE SET name=EXCLUDED.name
RETURNING id AS smoke_debit_account_id \gset

INSERT INTO accounts(company_id, code, name, type, is_detail, is_active)
VALUES (:smoke_company_id, '3100', 'Smoke equity', 'Equity', true, true)
ON CONFLICT (company_id, code) DO UPDATE SET name=EXCLUDED.name
RETURNING id AS smoke_credit_account_id \gset

INSERT INTO journal_vouchers(
    company_id, branch_id, voucher_no, voucher_date, description,
    source_doc_type, status, external_key, source_reference, workspace_revision
)
VALUES (
    :smoke_company_id, :smoke_branch_id, 'SMOKE-1', CURRENT_DATE, 'migration smoke test',
    'SmokeTest', 'Draft', 'SMOKE:IMMUTABILITY', 'SMOKE', 1
)
RETURNING id AS smoke_voucher_id \gset

INSERT INTO journal_voucher_lines(company_id, journal_voucher_id, account_id, debit, credit, line_no)
VALUES
    (:smoke_company_id, :smoke_voucher_id, :smoke_debit_account_id, 100, 0, 1),
    (:smoke_company_id, :smoke_voucher_id, :smoke_credit_account_id, 0, 100, 2);

UPDATE journal_vouchers
SET status='Posted', posted_at=CURRENT_TIMESTAMP
WHERE company_id=:smoke_company_id AND id=:smoke_voucher_id;

SET CONSTRAINTS ALL IMMEDIATE;

DO $$
BEGIN
    BEGIN
        INSERT INTO journal_voucher_lines(company_id, journal_voucher_id, account_id, debit, credit, line_no)
        SELECT v.company_id, v.id, l.account_id, 1, 0, 99
        FROM journal_vouchers v
        JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id
        WHERE v.external_key='SMOKE:IMMUTABILITY'
        ORDER BY l.line_no
        LIMIT 1;
        RAISE EXCEPTION 'posted voucher mutation unexpectedly succeeded';
    EXCEPTION WHEN OTHERS THEN
        IF SQLERRM = 'posted voucher mutation unexpectedly succeeded' THEN
            RAISE;
        END IF;
    END;
END;
$$;

ROLLBACK;
