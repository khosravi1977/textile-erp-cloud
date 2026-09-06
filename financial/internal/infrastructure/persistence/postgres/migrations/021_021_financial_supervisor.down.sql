-- Intentional non-destructive rollback: old releases ignore these additive
-- audit tables. Keep approval evidence and snapshots; do not DROP audit data.
SELECT 1;
