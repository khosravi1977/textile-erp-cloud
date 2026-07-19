package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type tenantQueryable interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type SessionQueryable = tenantQueryable

func normalizeCompanyID(companyID int64) int64 {
	if companyID > 0 {
		return companyID
	}
	return 1
}

func setCompanySession(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, companyID int64, local bool) error {
	_, err := execer.ExecContext(ctx, "SELECT set_config('app.company_id', $1, $2)", fmt.Sprintf("%d", normalizeCompanyID(companyID)), local)
	return err
}

func setTenantSession(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, local bool) error {
	return setCompanySession(ctx, execer, requestctx.CompanyID(ctx), local)
}

func resetTenantSession(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}) error {
	_, err := execer.ExecContext(ctx, "RESET app.company_id")
	return err
}

func withTenantConn[T any](ctx context.Context, db *sql.DB, fn func(tenantQueryable) (T, error)) (T, error) {
	return WithCompanySession(ctx, db, requestctx.CompanyID(ctx), func(q SessionQueryable) (T, error) {
		return fn(q)
	})
}

func WithCompanySession[T any](ctx context.Context, db *sql.DB, companyID int64, fn func(SessionQueryable) (T, error)) (T, error) {
	var zero T
	conn, err := db.Conn(ctx)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	if err := setCompanySession(ctx, conn, companyID, false); err != nil {
		return zero, err
	}
	defer resetTenantSession(ctx, conn)
	return fn(conn)
}

func WithCompanyTx(ctx context.Context, db *sql.DB, companyID int64, fn func(*sql.Tx) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := setCompanySession(ctx, tx, companyID, true); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func beginTenantTx(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := setTenantSession(ctx, tx, true); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}
