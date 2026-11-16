package handler

import (
	"context"
	"database/sql"
	"time"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
)

// Old undated openings may never have been posted. Reverse only their actual
// ledger net, then post the rial opening on an explicitly audited adjustment
// date. Never invent a historical business date or a phantom reversal.
func prepareRialOpeningDates(ctx context.Context, tx *sql.Tx, company int64, oldState, newState map[string]any) error {
	oldAccounts := map[string]map[string]any{}
	for _, row := range rowsFrom(oldState, "accounts") {
		oldAccounts[stringValue(row["id"])] = row
	}
	cash := workspaceCashAccounts(oldState)
	for _, row := range rowsFrom(newState, "accounts") {
		id := stringValue(row["id"])
		previous := oldAccounts[id]
		var actual float64
		var date string
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(l.debit-l.credit),0),COALESCE(MAX(v.voucher_date)::text,'') FROM journal_vouchers v JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id JOIN accounts a ON a.id=l.account_id AND a.company_id=l.company_id WHERE v.company_id=$1 AND v.status='Posted' AND v.source_reference=$2 AND a.code=$3`, company, "account-opening:"+id, cash[id].Code).Scan(&actual, &date)
		if err != nil {
			return err
		}
		if date == "" {
			date = time.Now().UTC().Format("2006-01-02")
		}
		row["sourceOpeningLedgerNet"] = actual
		if _, err := financecore.AccountingDate(firstText(row, "date", "createdAt")); err != nil && number(row["opening"]) != 0 {
			row["date"], row["openingDateBasis"], row["sourceOpeningDateMissing"] = date, "currency_normalization", true
		}
		if previous != nil {
			previous["date"], previous["opening"] = date, actual
		}
	}
	return nil
}
