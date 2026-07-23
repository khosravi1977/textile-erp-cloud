-- Tenant-safe chart of accounts and an immutable workspace-to-ledger audit trail.
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_code_key;
ALTER TABLE branches DROP CONSTRAINT IF EXISTS branches_code_key;
ALTER TABLE parties DROP CONSTRAINT IF EXISTS parties_code_key;

CREATE UNIQUE INDEX IF NOT EXISTS ux_accounts_company_code ON accounts(company_id, code);
CREATE UNIQUE INDEX IF NOT EXISTS ux_branches_company_code ON branches(company_id, code);
CREATE UNIQUE INDEX IF NOT EXISTS ux_parties_company_code ON parties(company_id, code);
CREATE UNIQUE INDEX IF NOT EXISTS ux_parties_company_name ON parties(company_id, lower(name));

ALTER TABLE journal_vouchers
    ADD COLUMN IF NOT EXISTS external_key VARCHAR(160),
    ADD COLUMN IF NOT EXISTS source_reference VARCHAR(160),
    ADD COLUMN IF NOT EXISTS workspace_revision BIGINT,
    ADD COLUMN IF NOT EXISTS reversal_of BIGINT REFERENCES journal_vouchers(id);

-- Keep reversal links inside the same tenant. The single-column foreign key
-- generated above is removed after it has made the ADD COLUMN operation safe
-- for both new and already-upgraded databases.
ALTER TABLE journal_vouchers DROP CONSTRAINT IF EXISTS journal_vouchers_reversal_of_fkey;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_journal_vouchers_company_reversal') THEN
        ALTER TABLE journal_vouchers
            ADD CONSTRAINT fk_journal_vouchers_company_reversal
            FOREIGN KEY (company_id, reversal_of) REFERENCES journal_vouchers(company_id, id);
    END IF;
END;
$$;

ALTER TABLE journal_voucher_lines
    ADD COLUMN IF NOT EXISTS line_no INTEGER;

CREATE UNIQUE INDEX IF NOT EXISTS ux_journal_vouchers_company_external_key
    ON journal_vouchers(company_id, external_key)
    WHERE external_key IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS ux_journal_lines_voucher_line_no
    ON journal_voucher_lines(journal_voucher_id, line_no)
    WHERE line_no IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_journal_vouchers_company_date
    ON journal_vouchers(company_id, voucher_date, id);
CREATE INDEX IF NOT EXISTS idx_journal_vouchers_company_source_reference
    ON journal_vouchers(company_id, source_reference, id);
CREATE INDEX IF NOT EXISTS idx_journal_lines_company_account
    ON journal_voucher_lines(company_id, account_id, journal_voucher_id);
CREATE INDEX IF NOT EXISTS idx_journal_lines_company_party
    ON journal_voucher_lines(company_id, party_id, journal_voucher_id)
    WHERE party_id IS NOT NULL;

ALTER TABLE journal_voucher_lines DROP CONSTRAINT IF EXISTS ck_journal_line_one_side;
ALTER TABLE journal_voucher_lines ADD CONSTRAINT ck_journal_line_one_side CHECK (
    debit >= 0 AND credit >= 0 AND
    ((debit > 0 AND credit = 0) OR (credit > 0 AND debit = 0))
);

CREATE TABLE IF NOT EXISTS fiscal_periods (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT NOT NULL REFERENCES companies(id),
    title VARCHAR(100) NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'Open',
    closed_at TIMESTAMP,
    closed_by BIGINT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ck_fiscal_period_dates CHECK (end_date >= start_date),
    CONSTRAINT ck_fiscal_period_status CHECK (status IN ('Open', 'Closed'))
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_fiscal_period_company_dates
    ON fiscal_periods(company_id, start_date, end_date);
CREATE INDEX IF NOT EXISTS idx_fiscal_period_company_status
    ON fiscal_periods(company_id, status);

ALTER TABLE fiscal_periods ENABLE ROW LEVEL SECURITY;
ALTER TABLE fiscal_periods FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_fiscal_periods ON fiscal_periods;
CREATE POLICY tenant_isolation_fiscal_periods ON fiscal_periods
    USING (company_id = current_app_company_id())
    WITH CHECK (company_id = current_app_company_id());

-- Posted vouchers are immutable. Corrections must be represented by a linked
-- reversal voucher, preserving the original audit trail.
CREATE OR REPLACE FUNCTION prevent_posted_voucher_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'Posted' THEN
        RAISE EXCEPTION 'posted journal vouchers are immutable; create a reversal';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_prevent_posted_voucher_mutation ON journal_vouchers;
CREATE TRIGGER trg_prevent_posted_voucher_mutation
BEFORE UPDATE OR DELETE ON journal_vouchers
FOR EACH ROW EXECUTE FUNCTION prevent_posted_voucher_mutation();

CREATE OR REPLACE FUNCTION prevent_posted_voucher_line_mutation()
RETURNS TRIGGER AS $$
DECLARE
    voucher_status VARCHAR(20);
    target_voucher_id BIGINT;
BEGIN
    target_voucher_id := CASE WHEN TG_OP = 'INSERT' THEN NEW.journal_voucher_id ELSE OLD.journal_voucher_id END;
    SELECT status INTO voucher_status
    FROM journal_vouchers
    WHERE id = target_voucher_id;
    IF voucher_status = 'Posted' THEN
        RAISE EXCEPTION 'posted journal voucher lines are immutable; create a reversal';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_prevent_posted_voucher_line_mutation ON journal_voucher_lines;
CREATE TRIGGER trg_prevent_posted_voucher_line_mutation
BEFORE INSERT OR UPDATE OR DELETE ON journal_voucher_lines
FOR EACH ROW EXECUTE FUNCTION prevent_posted_voucher_line_mutation();

-- A posted voucher must contain at least two non-zero lines and be balanced.
-- The constraint is deferred so applications can create a Draft voucher,
-- insert all of its lines, and post it atomically in one transaction.
CREATE OR REPLACE FUNCTION enforce_posted_voucher_balance()
RETURNS TRIGGER AS $$
DECLARE
    voucher_status VARCHAR(20);
    line_count BIGINT;
    total_debit NUMERIC(20,2);
    total_credit NUMERIC(20,2);
BEGIN
    SELECT status INTO voucher_status FROM journal_vouchers WHERE id = NEW.id;
    IF voucher_status = 'Posted' THEN
        SELECT COUNT(*), COALESCE(SUM(debit),0), COALESCE(SUM(credit),0)
        INTO line_count, total_debit, total_credit
        FROM journal_voucher_lines
        WHERE journal_voucher_id = NEW.id;
        IF line_count < 2 OR total_debit <= 0 OR total_credit <= 0 OR total_debit <> total_credit THEN
            RAISE EXCEPTION 'posted journal voucher % is not balanced', NEW.id;
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_posted_voucher_line_balance()
RETURNS TRIGGER AS $$
DECLARE
    target_voucher_id BIGINT;
    voucher_status VARCHAR(20);
    line_count BIGINT;
    total_debit NUMERIC(20,2);
    total_credit NUMERIC(20,2);
BEGIN
    target_voucher_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.journal_voucher_id ELSE NEW.journal_voucher_id END;
    SELECT status INTO voucher_status FROM journal_vouchers WHERE id = target_voucher_id;
    IF voucher_status = 'Posted' THEN
        SELECT COUNT(*), COALESCE(SUM(debit),0), COALESCE(SUM(credit),0)
        INTO line_count, total_debit, total_credit
        FROM journal_voucher_lines
        WHERE journal_voucher_id = target_voucher_id;
        IF line_count < 2 OR total_debit <= 0 OR total_credit <= 0 OR total_debit <> total_credit THEN
            RAISE EXCEPTION 'posted journal voucher % is not balanced', target_voucher_id;
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_enforce_posted_voucher_balance ON journal_vouchers;
CREATE CONSTRAINT TRIGGER trg_enforce_posted_voucher_balance
AFTER INSERT OR UPDATE ON journal_vouchers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_posted_voucher_balance();

DROP TRIGGER IF EXISTS trg_enforce_posted_voucher_line_balance ON journal_voucher_lines;
CREATE CONSTRAINT TRIGGER trg_enforce_posted_voucher_line_balance
AFTER INSERT OR UPDATE OR DELETE ON journal_voucher_lines
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_posted_voucher_line_balance();
