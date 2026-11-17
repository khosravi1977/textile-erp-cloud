package handler

import (
	"context"
	"database/sql"
)

func lockRialCompany(ctx context.Context, tx *sql.Tx, companyID int64) error {
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('rial-policy:' || $1::text,0))", companyID)
	return err
}
