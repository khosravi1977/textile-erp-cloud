// Package financecore implements typed posting for bank and cash
// transactions received from the HesabYar mobile app and the ERP UI.
// Every economic event is stored exactly once in bank_transactions and,
// unless suppressed for ledger sources that already post their own vouchers,
// materialised as a balanced journal voucher plus party-balance effects.
package financecore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

// Transaction types accepted by the posting engine.
const (
	TypeCustomerReceipt  = "CUSTOMER_RECEIPT"
	TypeDirectExpense    = "DIRECT_EXPENSE"
	TypeSupplierPayment  = "SUPPLIER_PAYMENT"
	TypePayrollPayment   = "PAYROLL_PAYMENT"
	TypeInternalTransfer = "INTERNAL_TRANSFER"
	TypePettyCashFunding = "PETTY_CASH_FUNDING"
	TypePettyCashReturn  = "PETTY_CASH_RETURN"
	TypeLoanReceipt      = "LOAN_RECEIPT"
	TypeLoanRepayment    = "LOAN_REPAYMENT"
	TypeOwnerDeposit     = "OWNER_DEPOSIT"
	TypeOwnerWithdrawal  = "OWNER_WITHDRAWAL"
	TypeAssetPurchase    = "ASSET_PURCHASE"
	TypeBankFee          = "BANK_FEE"
	TypeCheckReceipt     = "CHECK_RECEIPT"
	TypeCheckPayment     = "CHECK_PAYMENT"
	TypeRefund           = "REFUND"
	TypeOtherReceipt     = "OTHER_RECEIPT"
	TypeOtherPayment     = "OTHER_PAYMENT"
)

// Sources allowed on bank transactions.
const (
	SourceHesabyar = "HESABYAR"
	SourceERP      = "ERP_MANUAL"
	SourceImport   = "IMPORT"
	SourceSystem   = "SYSTEM"
)

// Posting statuses.
const (
	PostingPending     = "PENDING"
	PostingPosted      = "POSTED"
	PostingFailed      = "FAILED"
	PostingNeedsReview = "NEEDS_REVIEW"
)

// Party roles implied by transaction types.
const (
	RoleCustomer        = "CUSTOMER"
	RoleSupplier        = "SUPPLIER"
	RoleEmployee        = "EMPLOYEE"
	RolePettyCashHolder = "PETTY_CASH_HOLDER"
	RoleOwner           = "OWNER"
	RoleOther           = "OTHER"
)

type glAccountRef struct {
	Code string
	Name string
	Type string
}

// canonicalAccounts mirrors the chart of accounts used by the workspace
// accounting engine so both paths share one chart per company.
var canonicalAccounts = map[string]glAccountRef{
	"cash":             {"1110", "صندوق", "Asset"},
	"bank":             {"1120", "بانک", "Asset"},
	"pettyCash":        {"1150", "تنخواه", "Asset"},
	"receivable":       {"1200", "حساب‌های دریافتنی", "Asset"},
	"checkReceivable":  {"1210", "اسناد دریافتنی", "Asset"},
	"fixedAsset":       {"1400", "دارایی‌های ثابت", "Asset"},
	"payable":          {"2100", "حساب‌های پرداختنی", "Liability"},
	"checkPayable":     {"2110", "اسناد پرداختنی", "Liability"},
	"clearing":         {"2190", "حساب واسط دریافت و پرداخت", "Liability"},
	"loan":             {"2200", "تسهیلات مالی", "Liability"},
	"ownerEquity":      {"3100", "سرمایه و مانده افتتاحیه", "Equity"},
	"ownerDrawing":     {"3250", "برداشت مالک", "Equity"},
	"sales":            {"4200", "درآمد فروش", "Income"},
	"otherIncome":      {"4900", "سایر درآمدها", "Income"},
	"operatingExpense": {"5900", "هزینه‌های عملیاتی", "Expense"},
	"bankFeeExpense":   {"5910", "هزینه کارمزد بانکی", "Expense"},
	"payrollExpense":   {"5920", "هزینه حقوق و دستمزد", "Expense"},
	"interestExpense":  {"5930", "هزینه مالی و بهره", "Expense"},
}

type typeRule struct {
	Direction       string // expected cash direction; "" = both allowed
	RequiresParty   bool
	ImpliedRole     string
	RequiresCounter bool // INTERNAL_TRANSFER destination bank account
	CashLegs        bool // false for check documents that never touch cash
}

// TypeRequiresParty reports whether a transaction type must carry an ERP
// party. Mobile ingestion uses it to park unmatched party-required events in
// the review queue instead of guessing a counterparty.
func TypeRequiresParty(transactionType string) bool {
	rule, ok := typeRules[transactionType]
	return ok && rule.RequiresParty
}

var typeRules = map[string]typeRule{
	TypeCustomerReceipt:  {Direction: "IN", RequiresParty: true, ImpliedRole: RoleCustomer, CashLegs: true},
	TypeDirectExpense:    {Direction: "OUT", CashLegs: true},
	TypeSupplierPayment:  {Direction: "OUT", RequiresParty: true, ImpliedRole: RoleSupplier, CashLegs: true},
	TypePayrollPayment:   {Direction: "OUT", RequiresParty: true, ImpliedRole: RoleEmployee, CashLegs: true},
	TypeInternalTransfer: {RequiresCounter: true, CashLegs: true},
	TypePettyCashFunding: {Direction: "OUT", RequiresParty: true, ImpliedRole: RolePettyCashHolder, CashLegs: true},
	TypePettyCashReturn:  {Direction: "IN", RequiresParty: true, ImpliedRole: RolePettyCashHolder, CashLegs: true},
	TypeLoanReceipt:      {Direction: "IN", CashLegs: true},
	TypeLoanRepayment:    {Direction: "OUT", CashLegs: true},
	TypeOwnerDeposit:     {Direction: "IN", CashLegs: true},
	TypeOwnerWithdrawal:  {Direction: "OUT", CashLegs: true},
	TypeAssetPurchase:    {Direction: "OUT", CashLegs: true},
	TypeBankFee:          {Direction: "OUT", CashLegs: true},
	TypeCheckReceipt:     {Direction: "IN", RequiresParty: true, ImpliedRole: RoleCustomer, CashLegs: false},
	TypeCheckPayment:     {Direction: "OUT", RequiresParty: true, ImpliedRole: RoleSupplier, CashLegs: false},
	TypeRefund:           {Direction: "IN", CashLegs: true},
	TypeOtherReceipt:     {Direction: "IN", CashLegs: true},
	TypeOtherPayment:     {Direction: "OUT", CashLegs: true},
}

// ValidTransactionTypes exposes the accepted enum (for validation and docs).
func ValidTransactionTypes() []string {
	types := make([]string, 0, len(typeRules))
	for name := range typeRules {
		types = append(types, name)
	}
	sort.Strings(types)
	return types
}

// AllocationInput attaches a receipt to a document (invoice).
type AllocationInput struct {
	DocumentType    string `json:"document_type"`
	DocumentID      string `json:"document_id"`
	AllocatedAmount int64  `json:"allocated_amount"`
}

// TransactionRequest is the HesabYar / ERP posting contract.
type TransactionRequest struct {
	ExternalTransactionID string            `json:"external_transaction_id"`
	IdempotencyKey        string            `json:"idempotency_key"`
	BankAccountID         int64             `json:"bank_account_id"`
	BankAccountName       string            `json:"bank_account_name"`
	Direction             string            `json:"direction"`
	Amount                int64             `json:"amount"`
	TransactionDate       string            `json:"transaction_date"` // ISO or Jalali yyyy/mm/dd
	TransactionTime       string            `json:"transaction_time"`
	TransactionType       string            `json:"transaction_type"`
	PartyID               int64             `json:"party_id"`
	CounterAccountID      int64             `json:"counter_account_id"`
	CounterAccountName    string            `json:"counter_account_name"`
	InterestAmount        int64             `json:"interest_amount"`
	Description           string            `json:"description"`
	BankReference         string            `json:"bank_reference"`
	Source                string            `json:"source"`
	RawSourceReference    string            `json:"raw_source_reference"`
	Allocations           []AllocationInput `json:"allocations"`
}

// PostResult reports the outcome of an idempotent posting attempt.
type PostResult struct {
	Status            string `json:"status"` // created | duplicate | needs_review
	BankTransactionID int64  `json:"bank_transaction_id"`
	VoucherID         int64  `json:"journal_voucher_id,omitempty"`
	PostingStatus     string `json:"posting_status"`
	ReviewReason      string `json:"review_reason,omitempty"`
}

// Service exposes the typed posting engine over the shared DB handle.
type Service struct {
	db *sql.DB
}

func New(db *sql.DB) *Service {
	return &Service{db: db}
}

type postingLeg struct {
	Account glAccountRef
	Party   bool // attach party_id to this line
	Debit   int64
	Credit  int64
}

// postingPlan converts a validated request into balanced journal legs.
// The cash account is substituted per bank account type at execution time.
func postingPlan(req TransactionRequest, rule typeRule) ([]postingLeg, error) {
	amount := req.Amount
	cash := canonicalAccounts["bank"]
	with := func(debit glAccountRef, credit glAccountRef, partyOn string) []postingLeg {
		partyDebit := partyOn == "debit"
		return []postingLeg{
			{Account: debit, Party: partyDebit, Debit: amount},
			{Account: credit, Party: !partyDebit, Credit: amount},
		}
	}
	switch req.TransactionType {
	case TypeCustomerReceipt:
		return with(cash, canonicalAccounts["receivable"], "credit"), nil
	case TypeSupplierPayment:
		return with(canonicalAccounts["payable"], cash, "debit"), nil
	case TypeDirectExpense, TypeOtherPayment, TypeAssetPurchase:
		var debit glAccountRef
		switch req.TransactionType {
		case TypeAssetPurchase:
			debit = canonicalAccounts["fixedAsset"]
		default:
			debit = canonicalAccounts["operatingExpense"]
		}
		return []postingLeg{
			{Account: debit, Debit: amount},
			{Account: cash, Credit: amount},
		}, nil
	case TypePayrollPayment:
		return []postingLeg{
			{Account: canonicalAccounts["payrollExpense"], Party: true, Debit: amount},
			{Account: cash, Credit: amount},
		}, nil
	case TypePettyCashFunding:
		return []postingLeg{
			{Account: canonicalAccounts["pettyCash"], Party: true, Debit: amount},
			{Account: cash, Credit: amount},
		}, nil
	case TypePettyCashReturn:
		return []postingLeg{
			{Account: cash, Debit: amount},
			{Account: canonicalAccounts["pettyCash"], Party: true, Credit: amount},
		}, nil
	case TypeLoanReceipt:
		return []postingLeg{
			{Account: cash, Debit: amount},
			{Account: canonicalAccounts["loan"], Party: true, Credit: amount},
		}, nil
	case TypeLoanRepayment:
		principal := amount - maxInt64(req.InterestAmount, 0)
		if principal < 0 {
			return nil, errors.New("کارمزد وام نمی‌تواند از مبلغ تراکنش بیشتر باشد")
		}
		legs := []postingLeg{{Account: canonicalAccounts["loan"], Party: true, Debit: principal}}
		if interest := maxInt64(req.InterestAmount, 0); interest > 0 {
			legs = append(legs, postingLeg{Account: canonicalAccounts["interestExpense"], Debit: interest})
		}
		return append(legs, postingLeg{Account: cash, Credit: amount}), nil
	case TypeOwnerDeposit:
		return []postingLeg{
			{Account: cash, Debit: amount},
			{Account: canonicalAccounts["ownerEquity"], Party: true, Credit: amount},
		}, nil
	case TypeOwnerWithdrawal:
		return []postingLeg{
			{Account: canonicalAccounts["ownerDrawing"], Party: true, Debit: amount},
			{Account: cash, Credit: amount},
		}, nil
	case TypeBankFee:
		return []postingLeg{
			{Account: canonicalAccounts["bankFeeExpense"], Debit: amount},
			{Account: cash, Credit: amount},
		}, nil
	case TypeCheckReceipt:
		return []postingLeg{
			{Account: canonicalAccounts["checkReceivable"], Party: true, Debit: amount},
			{Account: canonicalAccounts["receivable"], Party: true, Credit: amount},
		}, nil
	case TypeCheckPayment:
		return []postingLeg{
			{Account: canonicalAccounts["payable"], Party: true, Debit: amount},
			{Account: canonicalAccounts["checkPayable"], Party: true, Credit: amount},
		}, nil
	case TypeRefund, TypeOtherReceipt:
		return []postingLeg{
			{Account: cash, Debit: amount},
			{Account: canonicalAccounts["otherIncome"], Credit: amount},
		}, nil
	case TypeInternalTransfer:
		// direction OUT: money leaves bank_account_id toward counter account.
		if strings.EqualFold(req.Direction, "IN") {
			return []postingLeg{
				{Account: cash, Debit: amount},
				{Account: canonicalAccounts["clearing"], Credit: amount},
			}, nil
		}
		return []postingLeg{
			{Account: canonicalAccounts["clearing"], Debit: amount},
			{Account: cash, Credit: amount},
		}, nil
	default:
		return nil, fmt.Errorf("نوع تراکنش %s پشتیبانی نمی‌شود", req.TransactionType)
	}
}

// partyBalanceEffect returns the signed delta for the party's AR/AP balance:
// positive increases debit (they hold/owe us), negative decreases it.
func partyBalanceEffect(req TransactionRequest) int64 {
	switch req.TransactionType {
	case TypeCustomerReceipt:
		return -req.Amount
	case TypeSupplierPayment:
		// Supplier balances are credit-based; encoded as negative debit.
		return req.Amount
	case TypePettyCashFunding:
		return req.Amount
	case TypePettyCashReturn:
		return -req.Amount
	case TypeLoanReceipt:
		return -req.Amount
	case TypeLoanRepayment:
		principal := req.Amount - maxInt64(req.InterestAmount, 0)
		return principal
	default:
		return 0
	}
}

func maxInt64(value, floor int64) int64 {
	if value > floor {
		return value
	}
	return floor
}

// Validate enforces the accounting rules per transaction type.
func Validate(req TransactionRequest) error {
	req.ExternalTransactionID = strings.TrimSpace(req.ExternalTransactionID)
	req.TransactionType = strings.TrimSpace(req.TransactionType)
	req.Direction = strings.ToUpper(strings.TrimSpace(req.Direction))
	if req.ExternalTransactionID == "" {
		return errors.New("شناسه یکتای تراکنش (external_transaction_id) الزامی است")
	}
	if len(req.ExternalTransactionID) > 120 {
		return errors.New("شناسه یکتای تراکنش بیش از حد طولانی است")
	}
	if req.Amount <= 0 {
		return errors.New("مبلغ تراکنش باید مثبت باشد")
	}
	rule, ok := typeRules[req.TransactionType]
	if !ok {
		return fmt.Errorf("نوع تراکنش نامعتبر است؛ موارد مجاز: %s", strings.Join(ValidTransactionTypes(), ", "))
	}
	if req.Direction != "IN" && req.Direction != "OUT" {
		return errors.New("جهت تراکنش باید IN یا OUT باشد")
	}
	if rule.Direction != "" && req.Direction != rule.Direction {
		return fmt.Errorf("تراکنش %s فقط جهت %s می‌پذیرد", req.TransactionType, rule.Direction)
	}
	if rule.RequiresParty && req.PartyID <= 0 {
		return fmt.Errorf("برای تراکنش %s انتخاب طرف حساب الزامی است", req.TransactionType)
	}
	if rule.RequiresCounter && req.CounterAccountID <= 0 && strings.TrimSpace(req.CounterAccountName) == "" {
		return errors.New("برای انتقال داخلی انتخاب حساب مقصد الزامی است")
	}
	if rule.RequiresCounter && req.CounterAccountID > 0 && req.CounterAccountID == req.BankAccountID {
		return errors.New("حساب مبدأ و مقصد انتقال داخلی نباید یکسان باشند")
	}
	if req.TransactionType == TypeLoanRepayment && req.InterestAmount < 0 {
		return errors.New("کارمزد وام نمی‌تواند منفی باشد")
	}
	if req.InterestAmount > req.Amount {
		return errors.New("کارمزد وام بیشتر از مبلغ تراکنش است")
	}
	return validateAllocations(req)
}

func validateAllocations(req TransactionRequest) error {
	if len(req.Allocations) == 0 {
		return nil
	}
	if req.TransactionType != TypeCustomerReceipt {
		return errors.New("تخصیص به فاکتور فقط برای وصول مشتری مجاز است")
	}
	var total int64
	for _, allocation := range req.Allocations {
		if allocation.AllocatedAmount <= 0 {
			return errors.New("مبلغ تخصیص باید مثبت باشد")
		}
		allowed := map[string]bool{"INVOICE": true, "UNALLOCATED_CREDIT": true, "OTHER_DOCUMENT": true}
		if !allowed[strings.TrimSpace(allocation.DocumentType)] {
			return errors.New("نوع سند تخصیص نامعتبر است")
		}
		total += allocation.AllocatedAmount
	}
	if total > req.Amount {
		return errors.New("مجموع تخصیص‌ها بیشتر از مبلغ وصول است")
	}
	return nil
}

// PostTransaction validates and persists a typed transaction atomically.
// WithGL=false keeps the canonical bank_transactions record (plus party
// balances and allocations) without a journal voucher; callers whose ledger
// is derived from another source (workspace state) use this to avoid
// double-posting.
func (s *Service) PostTransaction(
	ctx context.Context,
	companyID, userID int64,
	req TransactionRequest,
	withGL bool,
) (PostResult, error) {
	if err := Validate(req); err != nil {
		return PostResult{}, err
	}
	req.TransactionType = strings.TrimSpace(req.TransactionType)
	req.ExternalTransactionID = strings.TrimSpace(req.ExternalTransactionID)
	if strings.TrimSpace(req.Source) == "" {
		req.Source = SourceHesabyar
	}
	req.Direction = strings.ToUpper(strings.TrimSpace(req.Direction))
	rule := typeRules[req.TransactionType]

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PostResult{}, err
	}
	defer tx.Rollback()
	// Tenant scoping for RLS-guarded tables.
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.company_id', $1, false)", strconv.FormatInt(companyID, 10)); err != nil {
		return PostResult{}, err
	}

	// Idempotency: the same external id must never post twice.
	var existingID int64
	var existingStatus, existingPosting string
	err = tx.QueryRowContext(ctx, `
		SELECT id, status, posting_status
		FROM bank_transactions
		WHERE company_id=$1 AND external_transaction_id=$2
	`, companyID, req.ExternalTransactionID).Scan(&existingID, &existingStatus, &existingPosting)
	if err == nil {
		return PostResult{
			Status: "duplicate", BankTransactionID: existingID,
			PostingStatus: existingPosting,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PostResult{}, err
	}

	bankAccountID, cashType, err := ensureBankAccount(ctx, tx, companyID, req.BankAccountID, req.BankAccountName)
	if err != nil {
		return PostResult{}, err
	}
	req.BankAccountID = bankAccountID

	counterAccountID, err := ensureCounterAccount(ctx, tx, companyID, req)
	if err != nil {
		return PostResult{}, err
	}
	if counterAccountID > 0 && counterAccountID == req.BankAccountID {
		return PostResult{}, errors.New("حساب مبدأ و مقصد انتقال داخلی نباید یکسان باشند")
	}

	if rule.ImpliedRole != "" && req.PartyID > 0 {
		if err := ensurePartyRole(ctx, tx, companyID, req.PartyID, rule.ImpliedRole); err != nil {
			return PostResult{}, err
		}
	}
	postingStatus := PostingPosted

	occurredAt, err := AccountingDate(req.TransactionDate)
	if err != nil {
		return PostResult{}, fmt.Errorf("تاریخ تراکنش معتبر نیست: %w", err)
	}
	if strings.TrimSpace(req.TransactionTime) == "" {
		req.TransactionTime = AccountingClock(req.TransactionDate)
	}

	var voucherID int64
	if withGL && postingStatus == PostingPosted {
		legs, err := postingPlan(req, rule)
		if err != nil {
			return PostResult{}, err
		}
		// Substitute the cash leg account by the bank account's type.
		cash := canonicalAccounts["bank"]
		if cashType == "CASH" {
			cash = canonicalAccounts["cash"]
		}
		for index := range legs {
			if legs[index].Account == canonicalAccounts["bank"] {
				legs[index].Account = cash
			}
		}
		voucherID, err = insertVoucher(ctx, tx, companyID, userID, req, occurredAt, legs)
		if err != nil {
			return PostResult{}, err
		}
	}

	var transactionID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO bank_transactions(
			company_id, external_transaction_id, idempotency_key,
			bank_account_id, direction, amount,
			transaction_date, transaction_time, transaction_type,
			party_id, counter_account_id,
			interest_amount, description, bank_reference,
			source, raw_source_reference,
			status, posting_status, journal_voucher_id, created_by
		)
		VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7,NULLIF($8,''),$9,NULLIF($10,0),NULLIF($11,0),NULLIF($12,0),$13,NULLIF($14,''),$15,NULLIF($16,''),'ACTIVE',$17,NULLIF($18,0),$19)
		RETURNING id
	`, companyID, req.ExternalTransactionID, req.IdempotencyKey,
		req.BankAccountID, req.Direction, req.Amount,
		occurredAt, req.TransactionTime, req.TransactionType,
		req.PartyID, counterAccountID,
		req.InterestAmount, truncate(req.Description, 400), truncate(req.BankReference, 120),
		req.Source, truncate(req.RawSourceReference, 200),
		postingStatus, voucherID, nullableUser(userID),
	).Scan(&transactionID)
	if err != nil {
		return PostResult{}, err
	}

	if postingStatus == PostingPosted && req.PartyID > 0 {
		if err := applyPartyBalance(ctx, tx, companyID, req); err != nil {
			return PostResult{}, err
		}
	}
	if err := saveAllocations(ctx, tx, companyID, userID, transactionID, req); err != nil {
		return PostResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return PostResult{}, err
	}
	return PostResult{
		Status: "created", BankTransactionID: transactionID,
		VoucherID: voucherID, PostingStatus: postingStatus,
	}, nil
}

// QueueReviewEntry records a legacy/unresolved event for manual review
// without posting anything (used when a party-required legacy transaction
// cannot be matched to a party by name).
func (s *Service) QueueReviewEntry(ctx context.Context, companyID int64, sourceRef, reason string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		if _, err := q.ExecContext(ctx, `
			INSERT INTO migration_review_queue(company_id, source_table, source_ref, reason, payload)
			VALUES($1,'bank_transactions',$2,$3,$4)
			ON CONFLICT (company_id, source_table, source_ref) DO NOTHING
		`, companyID, truncate(sourceRef, 120), truncate(reason, 200), raw); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	return err
}

// ReverseTransaction voids a posted transaction with a mirrored voucher so
// the original document is never deleted.
func (s *Service) ReverseTransaction(ctx context.Context, companyID, userID, transactionID int64, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.company_id', $1, false)", strconv.FormatInt(companyID, 10)); err != nil {
		return err
	}
	var externalID, txnType, direction, status string
	var amount, partyID, voucherID int64
	var occurredAt time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT external_transaction_id, transaction_type, direction, amount,
		       COALESCE(party_id,0), COALESCE(journal_voucher_id,0), status, transaction_date
		FROM bank_transactions
		WHERE company_id=$1 AND id=$2
		FOR UPDATE
	`, companyID, transactionID).Scan(&externalID, &txnType, &direction, &amount, &partyID, &voucherID, &status, &occurredAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("تراکنش موردنظر پیدا نشد")
		}
		return err
	}
	if status != "ACTIVE" {
		return errors.New("فقط تراکنش فعال قابل ابطال است")
	}
	if voucherID > 0 {
		if err := reverseVoucher(ctx, tx, companyID, userID, voucherID, reason); err != nil {
			return err
		}
	}
	if partyID > 0 {
		req := TransactionRequest{
			TransactionType: txnType,
			Amount:          amount,
			Direction:       direction,
			PartyID:         partyID,
		}
		if err := applyPartyBalance(ctx, tx, companyID, req, true); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE bank_transactions
		SET status='VOIDED', updated_at=CURRENT_TIMESTAMP
		WHERE company_id=$1 AND id=$2 AND status='ACTIVE'
	`, companyID, transactionID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("تراکنش در همین لحظه توسط کاربر دیگری تغییر کرد")
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO migration_review_queue(company_id, source_table, source_ref, reason, payload)
		VALUES($1,'bank_transactions',$2,$3,$4)
		ON CONFLICT (company_id, source_table, source_ref) DO NOTHING
	`, companyID, externalID, "voided: "+truncate(reason, 150), fmt.Sprintf(`{"transaction_id":%d,"amount":%d,"type":"%s"}`, transactionID, amount, txnType))
	return tx.Commit()
}

// ResolvePartyByName finds a party by exact (case-insensitive) name within
// the company. It never invents parties.
func (s *Service) ResolvePartyByName(ctx context.Context, companyID int64, name string) (int64, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false, nil
	}
	var id int64
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		return struct{}{}, q.QueryRowContext(ctx, `
			SELECT id FROM parties
			WHERE company_id=$1 AND lower(name)=lower($2) AND is_active=TRUE
			ORDER BY id LIMIT 1
		`, companyID, name).Scan(&id)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// Party exposes the party sync payload for HesabYar.
type Party struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Type  string   `json:"type"`
	Roles []string `json:"roles"`
}

// ListParties returns parties with their roles, optionally filtered by role.
func (s *Service) ListParties(ctx context.Context, companyID int64, role string) ([]Party, error) {
	return postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) ([]Party, error) {
		var rows *sql.Rows
		var err error
		if strings.TrimSpace(role) != "" {
			rows, err = q.QueryContext(ctx, `
				SELECT p.id, p.name, p.type, COALESCE(array_agg(pr.role) FILTER (WHERE pr.role IS NOT NULL), '{}')
				FROM parties p
				LEFT JOIN party_roles pr ON pr.party_id=p.id
				WHERE p.company_id=$1 AND p.is_active=TRUE AND EXISTS (
					SELECT 1 FROM party_roles x WHERE x.party_id=p.id AND x.role=$2
				)
				GROUP BY p.id, p.name, p.type
				ORDER BY p.name
			`, companyID, strings.ToUpper(strings.TrimSpace(role)))
		} else {
			rows, err = q.QueryContext(ctx, `
				SELECT p.id, p.name, p.type, COALESCE(array_agg(pr.role) FILTER (WHERE pr.role IS NOT NULL), '{}')
				FROM parties p
				LEFT JOIN party_roles pr ON pr.party_id=p.id
				WHERE p.company_id=$1 AND p.is_active=TRUE
				GROUP BY p.id, p.name, p.type
				ORDER BY p.name
			`, companyID)
		}
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		parties := make([]Party, 0)
		for rows.Next() {
			var item Party
			var roles []byte
			if err := rows.Scan(&item.ID, &item.Name, &item.Type, &roles); err != nil {
				return nil, err
			}
			item.Roles = parseTextArray(string(roles))
			parties = append(parties, item)
		}
		return parties, rows.Err()
	})
}

// LedgerEntry is one line of a party ledger view.
type LedgerEntry struct {
	Date        string `json:"date"`
	Description string `json:"description"`
	DocumentNo  string `json:"document_no"`
	Debit       int64  `json:"debit"`
	Credit      int64  `json:"credit"`
}

// PartyLedger returns the voucher-line ledger for one party.
func (s *Service) PartyLedger(ctx context.Context, companyID, partyID int64) ([]LedgerEntry, error) {
	return postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) ([]LedgerEntry, error) {
		rows, err := q.QueryContext(ctx, `
			SELECT v.voucher_date::text, COALESCE(v.description,''), COALESCE(v.voucher_no,''),
			       COALESCE(l.debit,0), COALESCE(l.credit,0)
			FROM journal_voucher_lines l
			JOIN journal_vouchers v ON v.id=l.journal_voucher_id
			WHERE l.company_id=$1 AND l.party_id=$2 AND v.status='Posted'
			ORDER BY v.voucher_date, v.id
		`, companyID, partyID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		entries := make([]LedgerEntry, 0)
		for rows.Next() {
			var item LedgerEntry
			if err := rows.Scan(&item.Date, &item.Description, &item.DocumentNo, &item.Debit, &item.Credit); err != nil {
				return nil, err
			}
			entries = append(entries, item)
		}
		return entries, rows.Err()
	})
}

// BankTransaction is the list payload for the bank & cash ledger view.
type BankTransaction struct {
	ID              int64  `json:"id"`
	ExternalID      string `json:"external_transaction_id"`
	TransactionType string `json:"transaction_type"`
	Direction       string `json:"direction"`
	Amount          int64  `json:"amount"`
	TransactionDate string `json:"transaction_date"`
	BankAccountID   int64  `json:"bank_account_id"`
	BankAccountName string `json:"bank_account_name"`
	PartyID         int64  `json:"party_id"`
	PartyName       string `json:"party_name"`
	Description     string `json:"description"`
	Source          string `json:"source"`
	Status          string `json:"status"`
	PostingStatus   string `json:"posting_status"`
}

// ListTransactions returns typed transactions for the bank & cash view.
func (s *Service) ListTransactions(ctx context.Context, companyID int64, limit int) ([]BankTransaction, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) ([]BankTransaction, error) {
		rows, err := q.QueryContext(ctx, `
			SELECT t.id, t.external_transaction_id, t.transaction_type, t.direction,
			       t.amount, t.transaction_date::text, t.bank_account_id,
			       COALESCE(a.name,''), COALESCE(t.party_id,0), COALESCE(p.name,''),
			       COALESCE(t.description,''), t.source, t.status, t.posting_status
			FROM bank_transactions t
			LEFT JOIN bank_accounts a ON a.id=t.bank_account_id
			LEFT JOIN parties p ON p.id=t.party_id
			WHERE t.company_id=$1
			ORDER BY t.transaction_date DESC, t.id DESC
			LIMIT $2
		`, companyID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]BankTransaction, 0)
		for rows.Next() {
			var item BankTransaction
			if err := rows.Scan(&item.ID, &item.ExternalID, &item.TransactionType, &item.Direction,
				&item.Amount, &item.TransactionDate, &item.BankAccountID,
				&item.BankAccountName, &item.PartyID, &item.PartyName,
				&item.Description, &item.Source, &item.Status, &item.PostingStatus); err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, rows.Err()
	})
}

// BankAccount is a bank/cash account row for the sync API.
type BankAccount struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	AccountType    string `json:"account_type"`
	OpeningBalance int64  `json:"opening_balance"`
	IsActive       bool   `json:"is_active"`
}

// ListBankAccounts returns the company's bank and cash accounts.
func (s *Service) ListBankAccounts(ctx context.Context, companyID int64) ([]BankAccount, error) {
	return postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) ([]BankAccount, error) {
		rows, err := q.QueryContext(ctx, `
			SELECT id, name, account_type, opening_balance, is_active
			FROM bank_accounts
			WHERE company_id=$1
			ORDER BY id
		`, companyID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]BankAccount, 0)
		for rows.Next() {
			var item BankAccount
			if err := rows.Scan(&item.ID, &item.Name, &item.AccountType, &item.OpeningBalance, &item.IsActive); err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, rows.Err()
	})
}

func ensureBankAccount(ctx context.Context, tx *sql.Tx, companyID, requestedID int64, name string) (int64, string, error) {
	if requestedID > 0 {
		var id int64
		var accountType string
		err := tx.QueryRowContext(ctx, `
			SELECT id, account_type FROM bank_accounts
			WHERE company_id=$1 AND id=$2 AND is_active=TRUE
		`, companyID, requestedID).Scan(&id, &accountType)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", errors.New("حساب بانکی موردنظر پیدا نشد")
		}
		return id, accountType, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, "", errors.New("شناسه یا نام حساب بانکی الزامی است")
	}
	var id int64
	var accountType string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO bank_accounts(company_id, name, account_type, source)
		VALUES($1,$2,'BANK','MOBILE_IMPORT')
		ON CONFLICT (company_id, name) DO UPDATE SET updated_at=CURRENT_TIMESTAMP
		RETURNING id, account_type
	`, companyID, name).Scan(&id, &accountType)
	return id, accountType, err
}

func ensureCounterAccount(ctx context.Context, tx *sql.Tx, companyID int64, req TransactionRequest) (int64, error) {
	if !typeRules[req.TransactionType].RequiresCounter {
		return 0, nil
	}
	if req.CounterAccountID > 0 {
		var id int64
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM bank_accounts
			WHERE company_id=$1 AND id=$2 AND is_active=TRUE
		`, companyID, req.CounterAccountID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("حساب مقصد انتقال پیدا نشد")
		}
		return id, err
	}
	name := strings.TrimSpace(req.CounterAccountName)
	if name == "" {
		return 0, errors.New("حساب مقصد انتقال الزامی است")
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO bank_accounts(company_id, name, account_type, source)
		VALUES($1,$2,'BANK','MOBILE_IMPORT')
		ON CONFLICT (company_id, name) DO UPDATE SET updated_at=CURRENT_TIMESTAMP
		RETURNING id
	`, companyID, name).Scan(&id)
	return id, err
}

func ensurePartyRole(ctx context.Context, tx *sql.Tx, companyID, partyID int64, role string) error {
	if strings.TrimSpace(role) == "" {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM parties WHERE id=$1 AND company_id=$2)
	`, partyID, companyID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errors.New("طرف حساب موردنظر در این شرکت وجود ندارد")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO party_roles(party_id, role)
		VALUES($1,$2)
		ON CONFLICT (party_id, role) DO NOTHING
	`, partyID, role)
	return err
}

// applyPartyBalance updates ar_ap_balances. Supplier credit balances are
// stored in credit_balance, everything else in debit_balance.
func applyPartyBalance(ctx context.Context, tx *sql.Tx, companyID int64, req TransactionRequest, reverse ...bool) error {
	effect := partyBalanceEffect(req)
	for _, item := range reverse {
		if item {
			effect = -effect
		}
	}
	if effect == 0 || req.PartyID <= 0 {
		return nil
	}
	deltaDebit, deltaCredit := effect, int64(0)
	if req.TransactionType == TypeSupplierPayment {
		// Supplier payments reduce what we owe (credit side).
		deltaDebit, deltaCredit = 0, -effect
	}
	var rowID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM ar_ap_balances
		WHERE party_id=$1 AND company_id=$2 AND sub_project_id IS NULL
		ORDER BY id LIMIT 1
		FOR UPDATE
	`, req.PartyID, companyID).Scan(&rowID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ar_ap_balances(company_id, party_id, debit_balance, credit_balance, last_recalc_at)
			VALUES($1,$2,$3,$4,CURRENT_TIMESTAMP)
		`, companyID, req.PartyID, deltaDebit, deltaCredit)
		return err
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE ar_ap_balances
		SET debit_balance = debit_balance + $3,
		    credit_balance = credit_balance + $4,
		    last_recalc_at = CURRENT_TIMESTAMP
		WHERE id=$1 AND company_id=$2
	`, rowID, companyID, deltaDebit, deltaCredit)
	return err
}

func saveAllocations(ctx context.Context, tx *sql.Tx, companyID, userID, transactionID int64, req TransactionRequest) error {
	if req.TransactionType != TypeCustomerReceipt || req.PartyID <= 0 {
		return nil
	}
	allocated := int64(0)
	for _, allocation := range req.Allocations {
		documentType := strings.TrimSpace(allocation.DocumentType)
		if documentType == "" {
			documentType = "OTHER_DOCUMENT"
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO transaction_allocations(
				company_id, bank_transaction_id, document_type,
				document_id, party_id, allocated_amount, created_by
			)
			VALUES($1,$2,$3,NULLIF($4,''),$5,$6,NULLIF($7,0))
		`, companyID, transactionID, documentType,
			truncate(allocation.DocumentID, 60), req.PartyID,
			allocation.AllocatedAmount, nullableUser(userID))
		if err != nil {
			return err
		}
		allocated += allocation.AllocatedAmount
	}
	if len(req.Allocations) == 0 || allocated < req.Amount {
		// Remainder stays as an on-account (علی‌الحساب) credit for the party.
		remainder := req.Amount - allocated
		if remainder <= 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO transaction_allocations(
				company_id, bank_transaction_id, document_type,
				document_id, party_id, allocated_amount, created_by
			)
			VALUES($1,$2,'UNALLOCATED_CREDIT',NULL,$3,$4,NULLIF($5,0))
		`, companyID, transactionID, req.PartyID, remainder, nullableUser(userID))
		return err
	}
	return nil
}

func insertVoucher(
	ctx context.Context,
	tx *sql.Tx,
	companyID, userID int64,
	req TransactionRequest,
	occurredAt time.Time,
	legs []postingLeg,
) (int64, error) {
	branchID, err := ensureBranch(ctx, tx, companyID)
	if err != nil {
		return 0, err
	}
	externalKey := "FC:" + req.ExternalTransactionID
	var voucherID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO journal_vouchers(
			company_id, branch_id, voucher_no, voucher_date, description,
			source_doc_type, status, created_by, posted_at,
			external_key, source_reference
		)
		VALUES($1,$2,$3,$4,$5,'BankTransaction','Draft',$6,NULL,$7,$8)
		ON CONFLICT (company_id, external_key) WHERE external_key IS NOT NULL
		DO NOTHING
		RETURNING id
	`, companyID, branchID, "FC-"+truncate(req.ExternalTransactionID, 40), occurredAt,
		truncate(req.Description, 200), nullableUser(userID),
		externalKey, truncate(req.ExternalTransactionID, 160)).Scan(&voucherID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM journal_vouchers
			WHERE company_id=$1 AND external_key=$2
		`, companyID, externalKey).Scan(&voucherID)
		return voucherID, err
	}
	if err != nil {
		return 0, err
	}
	for index, leg := range legs {
		accountID, err := ensureAccount(ctx, tx, companyID, leg.Account)
		if err != nil {
			return 0, err
		}
		var partyID any
		if leg.Party && req.PartyID > 0 {
			partyID = req.PartyID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO journal_voucher_lines(
				company_id, journal_voucher_id, account_id, party_id,
				debit, credit, description, line_no
			)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		`, companyID, voucherID, accountID, partyID, leg.Debit, leg.Credit,
			truncate(req.Description, 200), index+1); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE journal_vouchers
		SET status='Posted', posted_at=CURRENT_TIMESTAMP
		WHERE company_id=$1 AND id=$2 AND status='Draft'
	`, companyID, voucherID); err != nil {
		return 0, err
	}
	return voucherID, nil
}

func reverseVoucher(ctx context.Context, tx *sql.Tx, companyID, userID, voucherID int64, reason string) error {
	var source string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(source_reference,'') FROM journal_vouchers
		WHERE company_id=$1 AND id=$2
	`, companyID, voucherID).Scan(&source); err != nil {
		return err
	}
	externalKey := fmt.Sprintf("FC-R-%d", voucherID)
	var reversalID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO journal_vouchers(
			company_id, branch_id, voucher_no, voucher_date, description,
			source_doc_type, status, created_by, posted_at,
			external_key, source_reference, reversal_of
		)
		SELECT company_id, branch_id, 'FC-R-'||id, voucher_date,
		       'ابطال: '||COALESCE($3,''), 'BankTransactionReversal', 'Draft', $4, NULL,
		       $5, source_reference, id
		FROM journal_vouchers
		WHERE company_id=$1 AND id=$2
		ON CONFLICT (company_id, external_key) WHERE external_key IS NOT NULL
		DO NOTHING
		RETURNING id
	`, companyID, voucherID, truncate(reason, 150), nullableUser(userID), externalKey).Scan(&reversalID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // reversal already exists
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO journal_voucher_lines(
			company_id, journal_voucher_id, account_id, party_id,
			debit, credit, description, line_no
		)
		SELECT company_id, $3, account_id, party_id, credit, debit, description, line_no
		FROM journal_voucher_lines
		WHERE journal_voucher_id=$1 AND company_id=$2
	`, voucherID, companyID, reversalID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE journal_vouchers
		SET status='Posted', posted_at=CURRENT_TIMESTAMP
		WHERE company_id=$1 AND id=$2 AND status='Draft'
	`, companyID, reversalID)
	return err
}

func ensureBranch(ctx context.Context, tx *sql.Tx, companyID int64) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO branches(company_id, code, name, is_active)
		VALUES($1,'MAIN','شعبه اصلی',TRUE)
		ON CONFLICT (company_id, code) DO UPDATE SET is_active=TRUE
		RETURNING id
	`, companyID).Scan(&id)
	return id, err
}

func ensureAccount(ctx context.Context, tx *sql.Tx, companyID int64, account glAccountRef) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO accounts(company_id, code, name, type, is_detail, is_active)
		VALUES($1,$2,$3,$4,TRUE,TRUE)
		ON CONFLICT (company_id, code) DO UPDATE SET name=EXCLUDED.name, type=EXCLUDED.type, is_active=TRUE
		RETURNING id
	`, companyID, account.Code, account.Name, account.Type).Scan(&id)
	return id, err
}

// AccountingDate accepts ISO dates and Jalali yyyy/mm/dd values, with an
// optional trailing clock time as sent by the HesabYar app (1405/06/04 15:49).
func AccountingDate(value string) (time.Time, error) {
	value = strings.TrimSpace(strings.NewReplacer(
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
		"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
		"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
		"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
	).Replace(value))
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	if space := strings.IndexByte(value, ' '); space > 0 {
		value = strings.TrimSpace(value[:space])
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed, nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '-' || r == '.' })
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("قالب تاریخ %s پشتیبانی نمی‌شود", value)
	}
	year, errYear := strconv.Atoi(parts[0])
	month, errMonth := strconv.Atoi(parts[1])
	day, errDay := strconv.Atoi(parts[2])
	if errYear != nil || errMonth != nil || errDay != nil || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("قالب تاریخ %s پشتیبانی نمی‌شود", value)
	}
	if year >= 1700 {
		return time.Parse("2006-1-2", fmt.Sprintf("%d-%d-%d", year, month, day))
	}
	gy, gm, gd := jalaliToGregorian(year, month, day)
	return time.Date(gy, time.Month(gm), gd, 0, 0, 0, 0, time.UTC), nil
}

// AccountingClock extracts the HH:MM part of a timestamped value, if any, so
// the original SMS time survives alongside the converted transaction_date.
func AccountingClock(value string) string {
	value = strings.TrimSpace(strings.NewReplacer(
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
		"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
		"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
		"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
	).Replace(value))
	parts := strings.Fields(value)
	for _, part := range parts[1:] {
		if len(part) >= 4 && strings.IndexByte(part, ':') > 0 {
			return part
		}
	}
	return ""
}

func jalaliToGregorian(jy, jm, jd int) (int, int, int) {
	jy += 1595
	days := -355668 + (365 * jy) + ((jy / 33) * 8) + (((jy % 33) + 3) / 4) + jd
	if jm < 7 {
		days += (jm - 1) * 31
	} else {
		days += ((jm - 7) * 30) + 186
	}
	gy := 400 * (days / 146097)
	days %= 146097
	if days > 36524 {
		days--
		gy += 100 * (days / 36524)
		days %= 36524
		if days >= 365 {
			days++
		}
	}
	gy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		gy += (days - 1) / 365
		days = (days - 1) % 365
	}
	gd := days + 1
	monthDays := []int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if (gy%4 == 0 && gy%100 != 0) || gy%400 == 0 {
		monthDays[2] = 29
	}
	gm := 1
	for gm <= 12 && gd > monthDays[gm] {
		gd -= monthDays[gm]
		gm++
	}
	return gy, gm, gd
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func nullableUser(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func parseTextArray(value string) []string {
	value = strings.Trim(value, "{}")
	if value == "" {
		return []string{}
	}
	return strings.Split(value, ",")
}
