-- ADR-0004 stage 1: operational expenses post into the typed core.
-- Expand→contract: install the wider check first, then drop the old one.
ALTER TABLE bank_transactions DROP CONSTRAINT bank_transactions_source_check; -- fw:allow-destructive (replaced by bank_transactions_source_check_v2 in this migration)
ALTER TABLE bank_transactions ADD CONSTRAINT bank_transactions_source_check_v2
  CHECK (source::text = ANY (ARRAY['HESABYAR'::character varying, 'ERP_MANUAL'::character varying, 'IMPORT'::character varying, 'SYSTEM'::character varying, 'OPERATIONAL'::character varying]));
