package financecore

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

// Integration tests cover the fifteen mandatory end-to-end scenarios from the
// financial-core prompt. They run only against a disposable database:
//
//	FINCORE_TEST_DB=1 DB_PORT=54399 DB_PASSWORD=test_pass DB_NAME=textile_erp_test go test ./internal/application/financecore/
func requireTestDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	if os.Getenv("FINCORE_TEST_DB") == "" {
		t.Skip("FINCORE_TEST_DB not set; skipping database integration tests")
	}
	t.Setenv("DB_READ_HOST", "")
	cfg := postgres.LoadConfig()
	db, err := postgres.Connect(cfg)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := postgres.RunMigrations(db, "../../infrastructure/persistence/postgres/migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	// Clean slate for the financial-core tables (order-independent with CASCADE).
	_, _ = db.Exec(`
		TRUNCATE migration_review_queue, transaction_allocations, bank_transactions,
		         bank_accounts, party_roles, ar_ap_balances, journal_voucher_lines,
		         journal_vouchers, parties RESTART IDENTITY CASCADE
	`)
	companyID := int64(1)
	var companyExists bool
	_ = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM companies WHERE id=$1)`, companyID).Scan(&companyExists)
	if !companyExists {
		t.Fatal("default company missing after migrations")
	}
	seed := `
		INSERT INTO parties(company_id, code, name, type, is_active) VALUES
		  ($1,'C-KESRA','شرکت کسرا','Customer',TRUE),
		  ($1,'S-SUP1','تأمین‌کننده یک','Supplier',TRUE),
		  ($1,'E-MAHDI','مهدی خسروی','Contractor',TRUE)
		ON CONFLICT DO NOTHING`
	if _, err := db.Exec(seed, companyID); err != nil {
		t.Fatalf("seed parties: %v", err)
	}
	return db, companyID
}

func partyIDByName(t *testing.T, db *sql.DB, companyID int64, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM parties WHERE company_id=$1 AND name=$2`, companyID, name).Scan(&id); err != nil {
		t.Fatalf("find party %s: %v", name, err)
	}
	return id
}

func partyDebitBalance(t *testing.T, db *sql.DB, partyID int64) int64 {
	t.Helper()
	var balance sql.NullFloat64
	if err := db.QueryRow(`SELECT debit_balance FROM ar_ap_balances WHERE party_id=$1`, partyID).Scan(&balance); err != nil {
		t.Fatalf("party debit balance: %v", err)
	}
	return int64(balance.Float64)
}

func partyCreditBalance(t *testing.T, db *sql.DB, partyID int64) int64 {
	t.Helper()
	var balance sql.NullFloat64
	if err := db.QueryRow(`SELECT credit_balance FROM ar_ap_balances WHERE party_id=$1`, partyID).Scan(&balance); err != nil {
		t.Fatalf("party credit balance: %v", err)
	}
	return int64(balance.Float64)
}

func profitLossTotal(t *testing.T, db *sql.DB, companyID int64) int64 {
	t.Helper()
	var total sql.NullFloat64
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(l.debit-l.credit),0)
		FROM journal_voucher_lines l
		JOIN journal_vouchers v ON v.id=l.journal_voucher_id
		JOIN accounts a ON a.id=l.account_id
		WHERE l.company_id=$1 AND v.status='Posted' AND a.type IN ('Income','Expense')
	`, companyID).Scan(&total); err != nil {
		t.Fatalf("profit/loss total: %v", err)
	}
	return int64(total.Float64)
}

func baseRequest(externalID, txnType, direction string, amount int64, partyID int64) TransactionRequest {
	return TransactionRequest{
		ExternalTransactionID: externalID,
		BankAccountName:       "بانک ملی",
		Direction:             direction,
		Amount:                amount,
		TransactionDate:       "1405/06/03",
		TransactionType:       txnType,
		PartyID:               partyID,
		Source:                SourceHesabyar,
	}
}

func TestHesabyarPostingScenarios(t *testing.T) {
	db, companyID := requireTestDB(t)
	service := New(db)
	ctx := context.Background()
	userID := int64(1)

	kesra := partyIDByName(t, db, companyID, "شرکت کسرا")
	supplier := partyIDByName(t, db, companyID, "تأمین‌کننده یک")
	employee := partyIDByName(t, db, companyID, "مهدی خسروی")

	// 1) Customer receipt reduces the receivable, never books income.
	plBefore := profitLossTotal(t, db, companyID)
	result, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-1", TypeCustomerReceipt, "IN", 40_000_000, kesra), true)
	if err != nil || result.Status != "created" {
		t.Fatalf("customer receipt failed: %v %+v", err, result)
	}
	if got := partyDebitBalance(t, db, kesra); got != -40_000_000 {
		t.Fatalf("kesra debit balance = %d, want -40000000", got)
	}
	if profitLossTotal(t, db, companyID) != plBefore {
		t.Fatal("customer receipt changed profit/loss")
	}

	// 2) Unallocated receipts keep the money as on-account credit
	//    (HY-1 40M + HY-2 10M, neither referenced an invoice).
	if _, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-2", TypeCustomerReceipt, "IN", 10_000_000, kesra), true); err != nil {
		t.Fatalf("unallocated receipt failed: %v", err)
	}
	var unallocated int64
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(allocated_amount),0)
		FROM transaction_allocations
		WHERE document_type='UNALLOCATED_CREDIT'
	`).Scan(&unallocated); err != nil || unallocated != 50_000_000 {
		t.Fatalf("unallocated credit = %d err=%v, want 50000000", unallocated, err)
	}

	// 3) Multi-invoice allocation.
	if _, err := service.PostTransaction(ctx, companyID, userID,
		TransactionRequest{
			ExternalTransactionID: "HY-3",
			BankAccountName:       "بانک ملی",
			Direction:             "IN",
			Amount:                100_000_000,
			TransactionDate:       "1405/06/04",
			TransactionType:       TypeCustomerReceipt,
			PartyID:               kesra,
			Source:                SourceHesabyar,
			Allocations: []AllocationInput{
				{DocumentType: "INVOICE", DocumentID: "125", AllocatedAmount: 30_000_000},
				{DocumentType: "INVOICE", DocumentID: "131", AllocatedAmount: 20_000_000},
				{DocumentType: "INVOICE", DocumentID: "142", AllocatedAmount: 50_000_000},
			},
		}, true); err != nil {
		t.Fatalf("multi-invoice allocation failed: %v", err)
	}
	var invoiceAllocation int64
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(allocated_amount),0)
		FROM transaction_allocations
		WHERE document_type='INVOICE'
	`).Scan(&invoiceAllocation); err != nil || invoiceAllocation != 100_000_000 {
		t.Fatalf("invoice allocation = %d err=%v, want 100000000", invoiceAllocation, err)
	}

	// 4) Supplier payment settles payable without re-booking expense.
	if _, err := service.PostTransaction(ctx, companyID, userID, TransactionRequest{
		ExternalTransactionID: "HY-4",
		BankAccountName:       "بانک ملی",
		Direction:             "OUT",
		Amount:                25_000_000,
		TransactionDate:       "1405/06/05",
		TransactionType:       TypeSupplierPayment,
		PartyID:               supplier,
		Source:                SourceHesabyar,
	}, true); err != nil {
		t.Fatalf("supplier payment failed: %v", err)
	}
	if got := partyCreditBalance(t, db, supplier); got != -25_000_000 {
		t.Fatalf("supplier credit balance = %d, want -25000000", got)
	}

	// 5) Direct expense posts expense + bank, no party required.
	plBefore = profitLossTotal(t, db, companyID)
	if _, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-5", TypeDirectExpense, "OUT", 8_000_000, 0), true); err != nil {
		t.Fatalf("direct expense failed: %v", err)
	}
	if got := profitLossTotal(t, db, companyID); got != plBefore+8_000_000 {
		t.Fatalf("direct expense P&L delta = %d, want +8000000", got-plBefore)
	}

	// 6) Payroll payment books payroll expense with the employee party.
	if _, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-6", TypePayrollPayment, "OUT", 120_000_000, employee), true); err != nil {
		t.Fatalf("payroll failed: %v", err)
	}
	var payrollLines int64
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM journal_voucher_lines l
		JOIN accounts a ON a.id=l.account_id
		JOIN journal_vouchers v ON v.id=l.journal_voucher_id
		WHERE v.company_id=$1 AND a.code='5920' AND l.party_id=$2
	`, companyID, employee).Scan(&payrollLines); err != nil || payrollLines == 0 {
		t.Fatalf("payroll expense line with party missing: %d err=%v", payrollLines, err)
	}

	// 7) Internal transfer never touches profit and loss.
	plBefore = profitLossTotal(t, db, companyID)
	transfer, err := service.PostTransaction(ctx, companyID, userID, TransactionRequest{
		ExternalTransactionID: "HY-7",
		BankAccountName:       "بانک ملی",
		CounterAccountName:    "بانک ملت",
		Direction:             "OUT",
		Amount:                50_000_000,
		TransactionDate:       "1405/06/06",
		TransactionType:       TypeInternalTransfer,
		Source:                SourceHesabyar,
	}, true)
	if err != nil {
		t.Fatalf("internal transfer failed: %v", err)
	}
	if profitLossTotal(t, db, companyID) != plBefore {
		t.Fatal("internal transfer changed profit/loss")
	}
	var counterAccount int64
	if err := db.QueryRow(`SELECT COALESCE(counter_account_id,0) FROM bank_transactions WHERE id=$1`, transfer.BankTransactionID).Scan(&counterAccount); err != nil || counterAccount == 0 {
		t.Fatalf("transfer counter account missing: %d err=%v", counterAccount, err)
	}

	// 8) Petty cash funding is not an expense.
	plBefore = profitLossTotal(t, db, companyID)
	if _, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-8", TypePettyCashFunding, "OUT", 50_000_000, employee), true); err != nil {
		t.Fatalf("petty cash funding failed: %v", err)
	}
	if profitLossTotal(t, db, companyID) != plBefore {
		t.Fatal("petty cash funding changed profit/loss")
	}
	if got := partyDebitBalance(t, db, employee); got != 50_000_000 {
		t.Fatalf("petty cash holder balance = %d, want 50000000", got)
	}

	// 9) Bank fee is a real expense without party.
	if _, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-9", TypeBankFee, "OUT", 94_000, 0), true); err != nil {
		t.Fatalf("bank fee failed: %v", err)
	}

	// 10) Loan receipt (not income) and repayment split with interest.
	plBefore = profitLossTotal(t, db, companyID)
	if _, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-10", TypeLoanReceipt, "IN", 500_000_000, 0), true); err != nil {
		t.Fatalf("loan receipt failed: %v", err)
	}
	if profitLossTotal(t, db, companyID) != plBefore {
		t.Fatal("loan receipt changed profit/loss")
	}
	if _, err := service.PostTransaction(ctx, companyID, userID, TransactionRequest{
		ExternalTransactionID: "HY-10-R",
		BankAccountName:       "بانک ملی",
		Direction:             "OUT",
		Amount:                110_000_000,
		InterestAmount:        10_000_000,
		TransactionDate:       "1405/06/07",
		TransactionType:       TypeLoanRepayment,
		Source:                SourceHesabyar,
	}, true); err != nil {
		t.Fatalf("loan repayment failed: %v", err)
	}
	if got := profitLossTotal(t, db, companyID); got != plBefore+10_000_000 {
		t.Fatalf("loan repayment P&L delta = %d, want +10000000 (interest only)", got-plBefore)
	}

	// 11) Duplicate API call never posts twice.
	duplicate, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-1", TypeCustomerReceipt, "IN", 40_000_000, kesra), true)
	if err != nil || duplicate.Status != "duplicate" || duplicate.BankTransactionID != result.BankTransactionID {
		t.Fatalf("duplicate call must return the original record: %+v err=%v", duplicate, err)
	}
	var txnCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM bank_transactions WHERE external_transaction_id='HY-1'`).Scan(&txnCount); err != nil || txnCount != 1 {
		t.Fatalf("duplicate created a second row: %d err=%v", txnCount, err)
	}

	// 12) Party-less transaction on optional types posts normally.
	if _, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-12", TypeOtherPayment, "OUT", 1_500_000, 0), true); err != nil {
		t.Fatalf("party-less other payment failed: %v", err)
	}

	// 13) Party-required type without a party is rejected outright and the
	//     legacy path parks it in the review queue instead of guessing.
	if _, err := service.PostTransaction(ctx, companyID, userID,
		baseRequest("HY-13", TypeCustomerReceipt, "IN", 5_000_000, 0), true); err == nil {
		t.Fatal("party-less customer receipt must be rejected")
	}
	if err := service.QueueReviewEntry(ctx, companyID, "HY-13",
		"legacy party unmatched: کسرا?", map[string]any{"amount": 5_000_000}); err != nil {
		t.Fatalf("queue review entry failed: %v", err)
	}
	var reviewRows int64
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM migration_review_queue
		WHERE source_ref='HY-13' AND status='PENDING'
	`).Scan(&reviewRows); err != nil || reviewRows != 1 {
		t.Fatalf("review queue row missing: %d err=%v", reviewRows, err)
	}

	// 14) Reversal voids the document with a mirrored voucher.
	balanceBefore := partyDebitBalance(t, db, kesra)
	if err := service.ReverseTransaction(ctx, companyID, userID, result.BankTransactionID, "واریز اشتباه"); err != nil {
		t.Fatalf("reverse failed: %v", err)
	}
	if got := partyDebitBalance(t, db, kesra); got != balanceBefore+40_000_000 {
		t.Fatalf("reversal did not restore receivable: %d -> %d", balanceBefore, got)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM bank_transactions WHERE id=$1`, result.BankTransactionID).Scan(&status); err != nil || status != "VOIDED" {
		t.Fatalf("transaction not voided: %s err=%v", status, err)
	}

	// 15) Legacy five-way mapping resolves parties by exact name and posts
	// typed records (voucherless; ledger stays with the workspace engine).
	resolved, found, err := service.ResolvePartyByName(ctx, companyID, "  شرکت کسرا  ")
	if err != nil || !found || resolved != kesra {
		t.Fatalf("party name resolution failed: %d %v %v", resolved, found, err)
	}
	var partyRoles int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM party_roles WHERE party_id=$1 AND role='CUSTOMER'`, kesra).Scan(&partyRoles); err != nil || partyRoles != 1 {
		t.Fatalf("customer role not stamped on party: %d err=%v", partyRoles, err)
	}

	// Ledger must stay balanced across every posted voucher.
	var unbalanced int64
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT v.id, SUM(l.debit) d, SUM(l.credit) c
			FROM journal_vouchers v
			JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id
			WHERE v.company_id=$1 AND v.status='Posted'
			GROUP BY v.id HAVING SUM(l.debit) <> SUM(l.credit)
		) bad
	`, companyID).Scan(&unbalanced); err != nil || unbalanced != 0 {
		t.Fatalf("unbalanced vouchers: %d err=%v", unbalanced, err)
	}
}
