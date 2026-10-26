ALTER TABLE bank_transactions DROP CONSTRAINT IF EXISTS bank_transactions_source_check_v2;
ALTER TABLE bank_transactions ADD CONSTRAINT bank_transactions_source_check
  CHECK (source::text = ANY (ARRAY['HESABYAR'::character varying, 'ERP_MANUAL'::character varying, 'IMPORT'::character varying, 'SYSTEM'::character varying]));
DELETE FROM bank_transactions WHERE source = 'OPERATIONAL';
