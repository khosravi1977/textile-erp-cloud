package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// Verify against stored postings before committing, independently of the
// in-memory ledger planner. A missing/zeroed posting must roll back the entire
// normalization, including amounts, party balances and its audit marker.
func verifyRialLedger(ctx context.Context, tx *sql.Tx, company int64, state map[string]any) error {
	cash := workspaceCashAccounts(state)
	for _, row := range rowsFrom(state, "accounts") {
		id := stringValue(row["id"])
		expected, err := rialInteger(row["opening"])
		if err != nil {
			return err
		}
		var actual float64
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(l.debit-l.credit),0) FROM journal_vouchers v JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id JOIN accounts a ON a.id=l.account_id AND a.company_id=l.company_id WHERE v.company_id=$1 AND v.status='Posted' AND v.source_reference=$2 AND a.code=$3`, company, "account-opening:"+id, cash[id].Code).Scan(&actual)
		if err != nil {
			return err
		}
		if !amountsEqual(actual, float64(expected)) {
			return fmt.Errorf("rial opening ledger reconciliation failed for %s", id)
		}
	}
	var invalid int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bank_transactions t LEFT JOIN LATERAL (SELECT SUM(debit) total FROM journal_voucher_lines WHERE company_id=t.company_id AND journal_voucher_id=t.journal_voucher_id) l ON TRUE WHERE t.company_id=$1 AND t.currency='IRR' AND t.source_currency='IRT' AND t.status='ACTIVE' AND t.posting_status='POSTED' AND t.journal_voucher_id IS NOT NULL AND COALESCE(l.total,0)<>t.amount`, company).Scan(&invalid)
	if err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("%d typed vouchers do not match full rial amounts", invalid)
	}
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT v.id FROM journal_vouchers v JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id WHERE v.company_id=$1 AND v.status='Posted' GROUP BY v.id HAVING ABS(SUM(l.debit-l.credit))>0.001) invalid`, company).Scan(&invalid)
	if err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("%d posted vouchers are unbalanced", invalid)
	}
	return nil
}
