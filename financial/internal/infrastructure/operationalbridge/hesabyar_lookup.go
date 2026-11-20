package operationalbridge

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// HesabyarCatalog uses a qualified table in the company's PostgreSQL schema.
// Legacy operational lookups may use SQLite or a public schema fallback.
func (b *Bridge) HesabyarCatalog(ctx context.Context, companyID int64, catalog string) ([]LookupRow, error) {
	if b == nil || b.db == nil || b.dialect != "postgres" || companyID <= 0 {
		return nil, errors.New("company PostgreSQL catalog required")
	}
	var table, idColumn, nameColumn string
	switch catalog {
	case "kala-items":
		table, idColumn, nameColumn = "kala_name", "id_kala_name", "name_kala_name"
	case "yarn-items":
		table, idColumn, nameColumn = "nakh_name", "id_nakh_name", "name_nakh_name"
	default:
		return nil, errors.New("unknown operational catalog")
	}
	tx, err := b.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_catalog.set_config('app.company_id', $1, true)", strconv.FormatInt(companyID, 10)); err != nil {
		return nil, err
	}
	var schema string
	if err := tx.QueryRowContext(ctx, `
		SELECT (
			SELECT schema_name FROM public.operational_tenants
			WHERE external_company_id = $1 AND active = 1
		)
	`, companyID).Scan(&schema); err != nil {
		return nil, err
	}
	lower := strings.ToLower(schema)
	if !safeSchemaName.MatchString(schema) || len(schema) > 63 || lower == "public" ||
		lower == "information_schema" || strings.HasPrefix(lower, "pg_") {
		return nil, errors.New("invalid operational tenant schema")
	}
	var shared bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM public.operational_tenants
			WHERE schema_name = $1 AND external_company_id <> $2 AND active = 1)
	`, schema, companyID).Scan(&shared); err != nil {
		return nil, err
	}
	if shared {
		return nil, errors.New("operational tenant schema is shared")
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+idColumn+", COALESCE("+nameColumn+",'') FROM "+
		pq.QuoteIdentifier(schema)+"."+table+" ORDER BY "+nameColumn+", "+idColumn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LookupRow{}
	for rows.Next() {
		var row LookupRow
		if err := rows.Scan(&row.ID, &row.Name); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
