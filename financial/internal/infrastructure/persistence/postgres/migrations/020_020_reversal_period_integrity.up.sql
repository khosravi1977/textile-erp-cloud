-- Keep reversal vouchers in the same accounting period as the voucher they reverse.
-- This prevents an edit of an old transaction from moving the reversal into today's
-- P&L/cash-flow period. Closed periods remain protected: a reversal of a voucher in
-- a closed period is rejected instead of silently bypassing the fiscal-period lock.

CREATE OR REPLACE FUNCTION enforce_reversal_voucher_period()
RETURNS TRIGGER AS $$
DECLARE
    original_date DATE;
    original_company BIGINT;
    period_is_closed BOOLEAN;
BEGIN
    IF NEW.reversal_of IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT company_id, voucher_date
      INTO original_company, original_date
      FROM journal_vouchers
     WHERE id = NEW.reversal_of;

    IF original_date IS NULL THEN
        RAISE EXCEPTION 'Reversal voucher % references a missing voucher %', NEW.id, NEW.reversal_of;
    END IF;

    IF original_company <> NEW.company_id THEN
        RAISE EXCEPTION 'Cross-company reversal is not allowed: company %, original company %', NEW.company_id, original_company;
    END IF;

    SELECT EXISTS (
        SELECT 1
          FROM fiscal_periods
         WHERE company_id = NEW.company_id
           AND status = 'Closed'
           AND original_date BETWEEN start_date AND end_date
    ) INTO period_is_closed;

    IF period_is_closed THEN
        RAISE EXCEPTION 'The accounting period for reversal date % is closed', original_date;
    END IF;

    NEW.voucher_date := original_date;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_reversal_voucher_period ON journal_vouchers;
CREATE TRIGGER trg_reversal_voucher_period
BEFORE INSERT OR UPDATE OF reversal_of, voucher_date
ON journal_vouchers
FOR EACH ROW
WHEN (NEW.reversal_of IS NOT NULL)
EXECUTE FUNCTION enforce_reversal_voucher_period();
