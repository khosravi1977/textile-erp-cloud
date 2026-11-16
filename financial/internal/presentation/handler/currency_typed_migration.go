package handler

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
	"github.com/erpsystem/textile-erp/internal/platform/monetary"
)

func migrateTypedRialRow(ctx context.Context, tx *sql.Tx, companyID, revision int64, row map[string]any) error {
	id, err := rialInteger(row["id"])
	if err != nil {
		return err
	}
	if stringValue(row["currency"]) == monetary.Rial {
		return nil
	}
	if !legacyTypedToman(row) {
		_, err = tx.ExecContext(ctx, "UPDATE bank_transactions SET currency='IRR',source_currency='IRR',source_amount=amount WHERE company_id=$1 AND id=$2 AND currency IS NULL", companyID, id)
		return err
	}
	oldAmount, err := rialInteger(row["amount"])
	if err != nil {
		return err
	}
	amount, err := monetary.ToRials(oldAmount, monetary.Toman)
	if err != nil {
		return err
	}
	interest, err := rialInteger(row["interest_amount"])
	if err != nil {
		return err
	}
	newInterest, err := monetary.ToRials(interest, monetary.Toman)
	if err != nil {
		return err
	}
	voucherID, err := rialInteger(row["journal_voucher_id"])
	if err != nil {
		return err
	}
	newVoucherID := voucherID
	active := stringValue(row["status"]) == "ACTIVE" && stringValue(row["posting_status"]) == "POSTED"
	if voucherID > 0 && active {
		entry, err := typedCurrencyEntry(ctx, tx, companyID, id, voucherID)
		if err != nil {
			return err
		}
		branchID, err := ensureLedgerBranch(ctx, tx, companyID)
		if err != nil {
			return err
		}
		if err := financecore.ReverseCurrencyVoucher(ctx, tx, companyID, voucherID); err != nil {
			return err
		}
		for i := range entry.Lines {
			if math.Abs(entry.Lines[i].Debit) > float64(monetary.MaxAmount/10) || math.Abs(entry.Lines[i].Credit) > float64(monetary.MaxAmount/10) {
				return fmt.Errorf("typed voucher exceeds safe rial range")
			}
			entry.Lines[i].Debit *= 10
			entry.Lines[i].Credit *= 10
		}
		if err := insertLedgerEntry(ctx, tx, companyID, 0, branchID, revision, entry, "N", ledgerHash(entry)); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT id FROM journal_vouchers WHERE company_id=$1 AND source_reference=$2 AND workspace_revision=$3 ORDER BY id DESC LIMIT 1", companyID, entry.Key, revision).Scan(&newVoucherID); err != nil {
			return err
		}
	}
	partyID, err := rialInteger(row["party_id"])
	if err != nil {
		return err
	}
	if active && partyID > 0 {
		req := financecore.TransactionRequest{TransactionType: stringValue(row["transaction_type"]), Direction: stringValue(row["direction"]), Amount: amount - oldAmount, InterestAmount: newInterest - interest, PartyID: partyID, Currency: monetary.Rial}
		if err := financecore.ApplyCurrencyPartyDelta(ctx, tx, companyID, req); err != nil {
			return err
		}
	}
	var invalidAllocation bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM transaction_allocations WHERE company_id=$1 AND bank_transaction_id=$2 AND ABS(allocated_amount)>$3)", companyID, id, monetary.MaxAmount/10).Scan(&invalidAllocation); err != nil {
		return err
	}
	if invalidAllocation {
		return fmt.Errorf("allocation exceeds safe rial range")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE transaction_allocations SET allocated_amount=allocated_amount*10 WHERE company_id=$1 AND bank_transaction_id=$2", companyID, id); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE bank_transactions SET amount=$3,interest_amount=NULLIF($4,0),currency='IRR',source_currency='IRT',source_amount=$5,journal_voucher_id=NULLIF($6,0),updated_at=CURRENT_TIMESTAMP WHERE company_id=$1 AND id=$2 AND currency IS NULL`, companyID, id, amount, newInterest, oldAmount, newVoucherID)
	return err
}

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
