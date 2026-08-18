-- Emergency audit-trail guard for the financial workspace.
-- Posted/financial source documents must not disappear from active state through
-- a hard delete or the generic "reset all finance" UI. A future explicit Void
-- workflow may retain the row and mark it voided; it does not need this bypass.

CREATE OR REPLACE FUNCTION financial_workspace_array(value JSONB)
RETURNS JSONB AS $$
BEGIN
    IF value IS NULL OR jsonb_typeof(value) <> 'array' THEN
        RETURN '[]'::jsonb;
    END IF;
    RETURN value;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE OR REPLACE FUNCTION financial_workspace_material_row_count(state JSONB)
RETURNS INTEGER AS $$
BEGIN
    RETURN
        jsonb_array_length(financial_workspace_array(state -> 'invoices')) +
        jsonb_array_length(financial_workspace_array(state -> 'incomingInvoices')) +
        jsonb_array_length(financial_workspace_array(state -> 'yarnOutInvoices')) +
        jsonb_array_length(financial_workspace_array(state -> 'expenses')) +
        jsonb_array_length(financial_workspace_array(state -> 'receivableDocs')) +
        jsonb_array_length(financial_workspace_array(state -> 'payableDocs')) +
        jsonb_array_length(financial_workspace_array(state -> 'accounts')) +
        jsonb_array_length(financial_workspace_array(state -> 'openingBalances')) +
        jsonb_array_length(financial_workspace_array(state -> 'ownedInventory')) +
        jsonb_array_length(financial_workspace_array(state -> 'journalEntries'));
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE OR REPLACE FUNCTION financial_workspace_missing_identity(
    old_state JSONB,
    new_state JSONB,
    collection_name TEXT,
    primary_key TEXT,
    fallback_key TEXT
)
RETURNS TEXT AS $$
DECLARE
    old_row JSONB;
    identity TEXT;
    still_exists BOOLEAN;
BEGIN
    FOR old_row IN
        SELECT value
          FROM jsonb_array_elements(financial_workspace_array(old_state -> collection_name)) AS t(value)
    LOOP
        identity := NULLIF(BTRIM(old_row ->> primary_key), '');
        IF identity IS NULL AND fallback_key IS NOT NULL THEN
            identity := NULLIF(BTRIM(old_row ->> fallback_key), '');
        END IF;
        IF identity IS NULL THEN
            CONTINUE;
        END IF;

        SELECT EXISTS (
            SELECT 1
              FROM jsonb_array_elements(financial_workspace_array(new_state -> collection_name)) AS n(value)
             WHERE COALESCE(NULLIF(BTRIM(n.value ->> primary_key), ''),
                            CASE WHEN fallback_key IS NULL THEN NULL ELSE NULLIF(BTRIM(n.value ->> fallback_key), '') END) = identity
        ) INTO still_exists;

        IF NOT still_exists THEN
            RETURN identity;
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE OR REPLACE FUNCTION protect_financial_workspace_source_history()
RETURNS TRIGGER AS $$
DECLARE
    missing TEXT;
    old_material_rows INTEGER;
    new_material_rows INTEGER;
BEGIN
    old_material_rows := financial_workspace_material_row_count(OLD.state);
    new_material_rows := financial_workspace_material_row_count(NEW.state);

    -- The normal production UI must never be able to replace an established
    -- financial workspace with an empty one. A future controlled maintenance
    -- endpoint should use an archival procedure, not disable this trigger.
    IF old_material_rows > 0 AND new_material_rows = 0 THEN
        RAISE EXCEPTION 'پاک‌کردن کامل اطلاعات مالی در محیط عملیاتی مجاز نیست. ابتدا از جریان آرشیو/پشتیبان مدیریتی استفاده کنید.';
    END IF;

    -- Invoice number is the current human/business stable key. This also blocks
    -- unsafe invoice renumbering until the application has a stable immutable ID
    -- migration and an audited renumbering workflow.
    missing := financial_workspace_missing_identity(OLD.state, NEW.state, 'invoices', 'number', 'id');
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'حذف یا تغییر شماره فاکتور مالی ثبت‌شده مجاز نیست (%). از جریان ابطال/اصلاح استفاده کنید.', missing;
    END IF;

    missing := financial_workspace_missing_identity(OLD.state, NEW.state, 'incomingInvoices', 'id', 'sourceId');
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'حذف فاکتور ورود ثبت‌شده مجاز نیست (%). از جریان ابطال/اصلاح استفاده کنید.', missing;
    END IF;

    missing := financial_workspace_missing_identity(OLD.state, NEW.state, 'yarnOutInvoices', 'id', 'sourceId');
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'حذف خروج مالی نخ ثبت‌شده مجاز نیست (%). از جریان ابطال/اصلاح استفاده کنید.', missing;
    END IF;

    missing := financial_workspace_missing_identity(OLD.state, NEW.state, 'expenses', 'id', 'sourceId');
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'حذف هزینه ثبت‌شده مجاز نیست (%). از جریان ابطال/اصلاح استفاده کنید.', missing;
    END IF;

    missing := financial_workspace_missing_identity(OLD.state, NEW.state, 'openingBalances', 'id', NULL);
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'حذف مانده افتتاحیه ثبت‌شده مجاز نیست (%). از جریان اصلاح افتتاحیه استفاده کنید.', missing;
    END IF;

    missing := financial_workspace_missing_identity(OLD.state, NEW.state, 'receivableDocs', 'id', 'checkNo');
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'حذف سند دریافتنی ثبت‌شده مجاز نیست (%). وضعیت سند را با جریان ابطال/برگشت تغییر دهید.', missing;
    END IF;

    missing := financial_workspace_missing_identity(OLD.state, NEW.state, 'payableDocs', 'id', 'checkNo');
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'حذف سند پرداختنی ثبت‌شده مجاز نیست (%). وضعیت سند را با جریان ابطال/برگشت تغییر دهید.', missing;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_protect_financial_workspace_source_history ON financial_workspace_states;
CREATE TRIGGER trg_protect_financial_workspace_source_history
BEFORE UPDATE OF state
ON financial_workspace_states
FOR EACH ROW
EXECUTE FUNCTION protect_financial_workspace_source_history();
