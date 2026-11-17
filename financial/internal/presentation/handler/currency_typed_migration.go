package handler

import (
	"context"
	"database/sql"
	"fmt"
)

func typedCurrencyEntry(ctx context.Context, tx *sql.Tx, companyID, id, voucherID int64) (ledgerEntry, error) {
	entry := ledgerEntry{Key: fmt.Sprintf("rial-currency:typed:%d", id), SourceType: "CurrencyNormalization", Description: "اصلاح واحد پول حسابیار از تومان به ریال"}
	if err := tx.QueryRowContext(ctx, "SELECT voucher_date::text FROM journal_vouchers WHERE company_id=$1 AND id=$2 AND status='Posted'", companyID, voucherID).Scan(&entry.Date); err != nil {
		return entry, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.code,a.name,a.type,COALESCE(p.name,''),l.debit,l.credit FROM journal_voucher_lines l JOIN accounts a ON a.id=l.account_id AND a.company_id=l.company_id LEFT JOIN parties p ON p.id=l.party_id AND p.company_id=l.company_id WHERE l.company_id=$1 AND l.journal_voucher_id=$2 ORDER BY l.line_no,l.id`, companyID, voucherID)
	if err != nil {
		return entry, err
	}
	defer rows.Close()
	for rows.Next() {
		var line ledgerLine
		if err := rows.Scan(&line.AccountCode, &line.AccountName, &line.AccountType, &line.Party, &line.Debit, &line.Credit); err != nil {
			return entry, err
		}
		entry.Lines = append(entry.Lines, line)
	}
	if len(entry.Lines) == 0 {
		return entry, fmt.Errorf("typed voucher %d has no lines", voucherID)
	}
	return entry, rows.Err()
}
