package handler

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
)

func correctTypedRialSource(ctx context.Context, tx *sql.Tx, company, revision int64, row, original map[string]any, match sourceVerifiedAmount, originalAllocations map[int64]map[string]any) error {
	id, err := rialInteger(row["id"])
	if err != nil {
		return err
	}
	current, err := rialInteger(row["amount"])
	if err != nil {
		return err
	}
	source, err := rialInteger(row["source_amount"])
	if err != nil {
		return err
	}
	prior, err := rialInteger(original["amount"])
	if err != nil {
		return err
	}
	if original == nil || source != match.Amount || prior != match.Amount || current != match.Amount*10 || stringValue(row["source_currency"]) != "IRT" || stringValue(row["currency"]) != "IRR" {
		return fmt.Errorf("typed %d differs from verified prior conversion", id)
	}
	interest, err := rialInteger(row["interest_amount"])
	if err != nil {
		return err
	}
	priorInterest, err := rialInteger(original["interest_amount"])
	if err != nil || interest != priorInterest*10 {
		return fmt.Errorf("typed %d interest was changed after conversion", id)
	}
	voucher, err := rialInteger(row["journal_voucher_id"])
	if err != nil {
		return err
	}
	active := stringValue(row["status"]) == "ACTIVE" && stringValue(row["posting_status"]) == "POSTED"
	newVoucher := voucher
	if active && voucher > 0 {
		entry, err := typedCurrencyEntry(ctx, tx, company, id, voucher)
		if err != nil {
			return err
		}
		entry.Key = fmt.Sprintf("rial-source-verified:typed:%d", id)
		entry.Description = "اصلاح مستند مبلغ ریالی حسابیار بر اساس فایل منبع"
		for i := range entry.Lines {
			debit, err := rialInteger(entry.Lines[i].Debit)
			if err != nil || debit%10 != 0 {
				return fmt.Errorf("typed %d debit is not an exact prior conversion", id)
			}
			credit, err := rialInteger(entry.Lines[i].Credit)
			if err != nil || credit%10 != 0 {
				return fmt.Errorf("typed %d credit is not an exact prior conversion", id)
			}
			entry.Lines[i].Debit, entry.Lines[i].Credit = float64(debit/10), float64(credit/10)
		}
		if err := financecore.ReverseCurrencyVoucher(ctx, tx, company, voucher); err != nil {
			return err
		}
		branch, err := ensureLedgerBranch(ctx, tx, company)
		if err != nil {
			return err
		}
		if err := insertLedgerEntry(ctx, tx, company, 0, branch, revision, entry, "N", ledgerHash(entry)); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT id FROM journal_vouchers WHERE company_id=$1 AND source_reference=$2 AND workspace_revision=$3 ORDER BY id DESC LIMIT 1", company, entry.Key, revision).Scan(&newVoucher); err != nil {
			return err
		}
	}
	party, err := rialInteger(row["party_id"])
	if err != nil {
		return err
	}
	if active && party > 0 {
		req := financecore.TransactionRequest{TransactionType: stringValue(row["transaction_type"]), Direction: stringValue(row["direction"]), Amount: match.Amount - current, InterestAmount: priorInterest - interest, PartyID: party, Currency: "IRR"}
		if err := financecore.ApplyCurrencyPartyDelta(ctx, tx, company, req); err != nil {
			return err
		}
	}
	allocations, err := tx.QueryContext(ctx, "SELECT id,allocated_amount FROM transaction_allocations WHERE company_id=$1 AND bank_transaction_id=$2", company, id)
	if err != nil {
		return err
	}
	type allocation struct{ id, amount int64 }
	updates := []allocation{}
	for allocations.Next() {
		var a allocation
		if err := allocations.Scan(&a.id, &a.amount); err != nil {
			allocations.Close()
			return err
		}
		old := originalAllocations[a.id]
		value, err := rialInteger(old["allocated_amount"])
		if old == nil || err != nil || a.amount != value*10 {
			allocations.Close()
			return fmt.Errorf("allocation %d was changed after conversion", a.id)
		}
		updates = append(updates, allocation{a.id, value})
	}
	err = allocations.Err()
	allocations.Close()
	if err != nil {
		return err
	}
	for _, a := range updates {
		if _, err := tx.ExecContext(ctx, "UPDATE transaction_allocations SET allocated_amount=$3 WHERE company_id=$1 AND id=$2", company, a.id, a.amount); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE bank_transactions SET amount=$3,interest_amount=NULLIF($4,0),currency='IRR',source_currency='IRR',source_amount=$3,journal_voucher_id=NULLIF($5,0),updated_at=CURRENT_TIMESTAMP WHERE company_id=$1 AND id=$2`, company, id, match.Amount, priorInterest, newVoucher)
	return err
}
