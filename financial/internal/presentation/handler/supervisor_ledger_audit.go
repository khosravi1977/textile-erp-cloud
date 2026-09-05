package handler

import (
	"context"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

// Compare the persisted workspace journal (including reversal entries) with the
// current document-derived net balances. Typed/manual journals are deliberately
// outside this partition and must not be mistaken for duplicate workspace rows.
func supervisorPersistedLedger(ctx context.Context, companyID int64, state map[string]any) (bool, error) {
	expectedEntries, err := deriveWorkspaceLedger(state)
	if err != nil {
		return false, err
	}
	expected := map[string]float64{}
	for _, entry := range expectedEntries {
		for _, line := range entry.Lines {
			expected[line.AccountCode+"|"+line.Party] += line.Debit - line.Credit
		}
	}
	return postgres.WithCompanySession(ctx, postgres.DB, companyID, func(q postgres.SessionQueryable) (bool, error) {
		rows, err := q.QueryContext(ctx, `SELECT a.code,COALESCE(p.name,''),SUM(l.debit-l.credit) FROM journal_vouchers v JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id JOIN accounts a ON a.id=l.account_id AND a.company_id=l.company_id LEFT JOIN parties p ON p.id=l.party_id AND p.company_id=l.company_id WHERE v.company_id=$1 AND v.status='Posted' AND v.external_key LIKE 'WS:%' GROUP BY a.code,p.name`, companyID)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var code, party string
			var net float64
			if err := rows.Scan(&code, &party, &net); err != nil {
				return false, err
			}
			expected[code+"|"+party] -= net
		}
		if err := rows.Err(); err != nil {
			return false, err
		}
		for _, net := range expected {
			if !amountsEqual(net, 0) {
				return false, nil
			}
		}
		return true, nil
	})
}

func supervisorHasPersistedLedger(ctx context.Context, companyID int64) (bool, error) {
	return postgres.WithCompanySession(ctx, postgres.DB, companyID, func(q postgres.SessionQueryable) (bool, error) {
		var exists bool
		err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM journal_vouchers WHERE company_id=$1 AND external_key LIKE 'WS:%')`, companyID).Scan(&exists)
		return exists, err
	})
}
