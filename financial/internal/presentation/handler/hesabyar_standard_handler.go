package handler

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
	"github.com/lib/pq"
)

var standardTransactionTypes = map[string]bool{
	"CUSTOMER_RECEIPT": true, "CASH_SALE": true, "INTERNAL_TRANSFER": true,
	"LOAN_RECEIPT": true, "OWNER_DEPOSIT": true, "CHECK_RECEIPT": true,
	"REFUND": true, "OTHER_RECEIPT": true, "DIRECT_EXPENSE": true,
	"SUPPLIER_PAYMENT": true, "PAYROLL_PAYMENT": true, "PETTY_CASH_FUNDING": true,
	"LOAN_REPAYMENT": true, "ASSET_PURCHASE": true, "CHECK_PAYMENT": true,
	"OWNER_WITHDRAWAL": true, "BANK_FEE": true, "OTHER_PAYMENT": true,
}

type standardTransactionRequest struct {
	ExternalTransactionID string               `json:"external_transaction_id"`
	BankAccountID         any                  `json:"bank_account_id"`
	Direction             string               `json:"direction"`
	Amount                float64              `json:"amount"`
	TransactionType       string               `json:"transaction_type"`
	PartyID               *int64               `json:"party_id"`
	CategoryID            *int64               `json:"category_id"`
	SubcategoryID         *int64               `json:"subcategory_id"`
	CounterAccountID      any                  `json:"counter_account_id"`
	Description           string               `json:"description"`
	BankReference         string               `json:"bank_reference"`
	OccurredAt            string               `json:"occurred_at"`
	Allocations           []standardAllocation `json:"allocations"`
}

type standardAllocation struct {
	ReferenceType string  `json:"reference_type"`
	ReferenceID   string  `json:"reference_id"`
	Amount        float64 `json:"amount"`
}

func standardID(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func (h *APIHandler) HesabYarParties(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	rows, err := postgres.WithCompanySession(r.Context(), postgres.DB, companyID, func(q postgres.SessionQueryable) ([]map[string]any, error) {
		result, err := q.QueryContext(r.Context(), `
			SELECT p.id, p.name, p.is_active, p.updated_at,
			       COALESCE(array_agg(DISTINCT pr.role) FILTER (WHERE pr.role IS NOT NULL), '{}')
			FROM parties p
			LEFT JOIN party_roles pr ON pr.company_id=p.company_id AND pr.party_id=p.id
			WHERE p.company_id=$1
			GROUP BY p.id, p.name, p.is_active, p.updated_at
			ORDER BY p.name
		`, companyID)
		if err != nil {
			return nil, err
		}
		defer result.Close()
		items := make([]map[string]any, 0)
		for result.Next() {
			var id int64
			var name string
			var active bool
			var updatedAt time.Time
			var roles []string
			if err := result.Scan(&id, &name, &active, &updatedAt, pq.Array(&roles)); err != nil {
				return nil, err
			}
			items = append(items, map[string]any{
				"id": id, "name": name, "status": map[bool]string{true: "ACTIVE", false: "INACTIVE"}[active],
				"roles": roles, "updated_at": updatedAt,
			})
		}
		return items, result.Err()
	})
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"success": true, "rows": rows, "parties": rows, "total": len(rows)})
}

func (h *APIHandler) HesabYarStandardTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req standardTransactionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid transaction payload")
		return
	}
	req.ExternalTransactionID = strings.TrimSpace(req.ExternalTransactionID)
	req.Direction = strings.ToUpper(strings.TrimSpace(req.Direction))
	req.TransactionType = strings.ToUpper(strings.TrimSpace(req.TransactionType))
	req.BankReference = strings.TrimSpace(req.BankReference)
	req.Description = strings.TrimSpace(req.Description)
	bankAccountID := standardID(req.BankAccountID)
	counterAccountID := standardID(req.CounterAccountID)
	if req.ExternalTransactionID == "" || bankAccountID == "" || req.Amount <= 0 ||
		(req.Direction != "IN" && req.Direction != "OUT") || !standardTransactionTypes[req.TransactionType] {
		RespondError(w, http.StatusBadRequest, "external_transaction_id, bank_account_id, direction, amount and transaction_type are required")
		return
	}
	if err := validateStandardDirection(req.Direction, req.TransactionType); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.TransactionType == "INTERNAL_TRANSFER" && (counterAccountID == "" || counterAccountID == bankAccountID) {
		RespondError(w, http.StatusBadRequest, "A different counter account is required for an internal transfer")
		return
	}
	if req.TransactionType == "PETTY_CASH_FUNDING" && req.PartyID == nil {
		RespondError(w, http.StatusBadRequest, "A petty cash holder is required")
		return
	}
	if req.TransactionType == "CUSTOMER_RECEIPT" && req.PartyID == nil {
		RespondError(w, http.StatusBadRequest, "A customer party is required")
		return
	}
	if req.TransactionType == "SUPPLIER_PAYMENT" && req.PartyID == nil {
		RespondError(w, http.StatusBadRequest, "A supplier party is required")
		return
	}
	if req.TransactionType == "PAYROLL_PAYMENT" && req.PartyID == nil {
		RespondError(w, http.StatusBadRequest, "An employee party is required")
		return
	}
	allocationTotal := 0.0
	for _, allocation := range req.Allocations {
		if strings.TrimSpace(allocation.ReferenceID) == "" || allocation.Amount <= 0 {
			RespondError(w, http.StatusBadRequest, "Each allocation needs a reference_id and positive amount")
			return
		}
		allocationTotal += allocation.Amount
	}
	if allocationTotal > req.Amount+0.005 {
		RespondError(w, http.StatusBadRequest, "Allocations cannot exceed transaction amount")
		return
	}

	companyID, userID := requestctx.CompanyID(r.Context()), requestctx.UserID(r.Context())
	var response map[string]any
	err := postgres.WithCompanyTx(r.Context(), postgres.DB, companyID, func(tx *sql.Tx) error {
		var existingID int64
		var existingStatus string
		var existingVoucher sql.NullInt64
		err := tx.QueryRowContext(r.Context(), `
			SELECT id, status, posting_voucher_id
			FROM bank_transactions
			WHERE company_id=$1 AND external_transaction_id=$2
			FOR UPDATE
		`, companyID, req.ExternalTransactionID).Scan(&existingID, &existingStatus, &existingVoucher)
		if err == nil {
			response = map[string]any{
				"success": true, "status": "duplicate", "erp_transaction_id": existingID,
				"transaction_id": existingID, "posting_type": req.TransactionType,
			}
			if existingVoucher.Valid {
				response["voucher_id"] = existingVoucher.Int64
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}

		if req.PartyID != nil {
			if err := validateStandardParty(r, tx, companyID, *req.PartyID, req.TransactionType); err != nil {
				return err
			}
		}
		occurredAt := time.Now()
		if strings.TrimSpace(req.OccurredAt) != "" {
			if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.OccurredAt)); err == nil {
				occurredAt = parsed
			} else if parsed, err := time.Parse("2006-01-02", strings.TrimSpace(req.OccurredAt)); err == nil {
				occurredAt = parsed
			} else {
				return fmt.Errorf("occurred_at is invalid")
			}
		}
		branchID, err := ensureLedgerBranch(r.Context(), tx, companyID)
		if err != nil {
			return err
		}
		bank := glAccount{Code: standardBankCode(bankAccountID), Name: "Hesab banki " + bankAccountID, Type: "Asset"}
		if err := ensureStandardAccount(r, tx, companyID, bank); err != nil {
			return err
		}
		counter, err := standardCounterAccount(r, tx, companyID, req, counterAccountID)
		if err != nil {
			return err
		}
		partyName, err := standardPartyName(r, tx, companyID, req.PartyID)
		if err != nil {
			return err
		}
		lines := []ledgerLine{}
		if req.Direction == "IN" {
			lines = []ledgerLine{debitLine(bank, req.Amount, partyName, "HesabYar receipt"), creditLine(counter, req.Amount, partyName, "HesabYar receipt")}
		} else {
			lines = []ledgerLine{debitLine(counter, req.Amount, partyName, "HesabYar payment"), creditLine(bank, req.Amount, partyName, "HesabYar payment")}
		}
		voucherID, err := insertStandardVoucher(r, tx, companyID, userID, branchID, req, occurredAt, lines)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(req)
		var transactionID int64
		err = tx.QueryRowContext(r.Context(), `
			INSERT INTO bank_transactions(
				company_id, external_transaction_id, bank_account_id, direction, amount,
				transaction_type, party_id, category_id, subcategory_id, counter_account_id,
				description, bank_reference, occurred_at, status, source, posting_voucher_id,
				raw_payload, created_by, updated_at
			) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'SYNCED','hesabyar',$14,$15,$16,CURRENT_TIMESTAMP)
			RETURNING id
		`, companyID, req.ExternalTransactionID, bankAccountID, req.Direction, req.Amount,
			req.TransactionType, req.PartyID, req.CategoryID, req.SubcategoryID, nullString(counterAccountID),
			req.Description, req.BankReference, occurredAt, voucherID, raw, nullUserID(userID)).Scan(&transactionID)
		if err != nil {
			return err
		}
		for _, allocation := range req.Allocations {
			if _, err := tx.ExecContext(r.Context(), `
				INSERT INTO transaction_allocations(company_id, bank_transaction_id, reference_type, reference_id, amount)
				VALUES($1,$2,$3,$4,$5)
				ON CONFLICT (company_id, bank_transaction_id, reference_type, reference_id)
				DO UPDATE SET amount=EXCLUDED.amount
			`, companyID, transactionID, strings.TrimSpace(strings.ToUpper(allocation.ReferenceType)), strings.TrimSpace(allocation.ReferenceID), allocation.Amount); err != nil {
				return err
			}
		}
		response = map[string]any{
			"success": true, "status": "synced", "erp_transaction_id": transactionID,
			"transaction_id": transactionID, "posting_type": req.TransactionType,
			"party_id": req.PartyID, "voucher_id": voucherID,
		}
		return nil
	})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "must have") {
			status = http.StatusBadRequest
		}
		RespondError(w, status, err.Error())
		return
	}
	RespondJSON(w, http.StatusCreated, response)
}

func validateStandardDirection(direction, transactionType string) error {
	incoming := map[string]bool{"CUSTOMER_RECEIPT": true, "CASH_SALE": true, "INTERNAL_TRANSFER": true, "LOAN_RECEIPT": true, "OWNER_DEPOSIT": true, "CHECK_RECEIPT": true, "REFUND": true, "OTHER_RECEIPT": true}
	outgoing := map[string]bool{"DIRECT_EXPENSE": true, "SUPPLIER_PAYMENT": true, "PAYROLL_PAYMENT": true, "INTERNAL_TRANSFER": true, "PETTY_CASH_FUNDING": true, "LOAN_REPAYMENT": true, "ASSET_PURCHASE": true, "CHECK_PAYMENT": true, "OWNER_WITHDRAWAL": true, "BANK_FEE": true, "OTHER_PAYMENT": true}
	if (direction == "IN" && !incoming[transactionType]) || (direction == "OUT" && !outgoing[transactionType]) {
		return fmt.Errorf("transaction_type %s is not valid for direction %s", transactionType, direction)
	}
	return nil
}

func validateStandardParty(r *http.Request, tx *sql.Tx, companyID, partyID int64, transactionType string) error {
	var active bool
	if err := tx.QueryRowContext(r.Context(), `SELECT is_active FROM parties WHERE company_id=$1 AND id=$2`, companyID, partyID).Scan(&active); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("party_id %d was not found", partyID)
		}
		return err
	}
	if !active {
		return fmt.Errorf("party_id %d is inactive", partyID)
	}
	role := map[string]string{
		"CUSTOMER_RECEIPT": "CUSTOMER", "SUPPLIER_PAYMENT": "SUPPLIER",
		"PAYROLL_PAYMENT": "EMPLOYEE", "PETTY_CASH_FUNDING": "PETTY_CASH_HOLDER",
	}[transactionType]
	if role == "" {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM party_roles WHERE company_id=$1 AND party_id=$2 AND role=$3)`, companyID, partyID, role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("party_id %d must have role %s", partyID, role)
	}
	return nil
}

func standardPartyName(r *http.Request, tx *sql.Tx, companyID int64, partyID *int64) (string, error) {
	if partyID == nil {
		return "", nil
	}
	var name string
	err := tx.QueryRowContext(r.Context(), `SELECT name FROM parties WHERE company_id=$1 AND id=$2`, companyID, *partyID).Scan(&name)
	return name, err
}

func standardBankCode(accountID string) string {
	sum := sha256.Sum256([]byte(accountID))
	return "HYB" + strings.ToUpper(hex.EncodeToString(sum[:]))[:14]
}

func standardCounterAccount(r *http.Request, tx *sql.Tx, companyID int64, req standardTransactionRequest, counterID string) (glAccount, error) {
	if req.TransactionType == "INTERNAL_TRANSFER" {
		account := glAccount{Code: standardBankCode(counterID), Name: "Hesab banki " + counterID, Type: "Asset"}
		return account, ensureStandardAccount(r, tx, companyID, account)
	}
	key := req.TransactionType
	code, name := "HYX"+strings.ToUpper(hex.EncodeToString(sha256Bytes(key)))[:12], "Hesab vaset "+key
	switch req.TransactionType {
	case "CUSTOMER_RECEIPT", "CHECK_RECEIPT":
		code, name = "1200", "Accounts Receivable"
	case "SUPPLIER_PAYMENT", "PAYROLL_PAYMENT":
		code, name = "2100", "Accounts Payable"
	case "DIRECT_EXPENSE":
		code, name = "5900", "Direct Expense"
	case "BANK_FEE":
		code, name = "5910", "Bank Fee"
	case "PETTY_CASH_FUNDING":
		code, name = "1110", "Petty Cash"
	case "OWNER_DEPOSIT", "OWNER_WITHDRAWAL":
		code, name = "3100", "Owner Equity"
	case "LOAN_RECEIPT", "LOAN_REPAYMENT":
		code, name = "2200", "Loan Account"
	case "ASSET_PURCHASE":
		code, name = "1400", "Fixed Assets"
	case "CASH_SALE", "REFUND", "OTHER_RECEIPT":
		code, name = "4900", "Other Income"
	case "CHECK_PAYMENT", "OTHER_PAYMENT":
		code, name = "5900", "Other Expense"
	}
	account := glAccount{Code: code, Name: name, Type: map[string]string{"1200": "Asset", "2100": "Liability", "3100": "Equity", "2200": "Liability", "4900": "Income"}[code]}
	if account.Type == "" {
		account.Type = "Expense"
	}
	return account, ensureStandardAccount(r, tx, companyID, account)
}

func sha256Bytes(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func ensureStandardAccount(r *http.Request, tx *sql.Tx, companyID int64, account glAccount) error {
	_, err := tx.ExecContext(r.Context(), `
		INSERT INTO accounts(company_id, code, name, type, is_detail, is_active)
		VALUES($1,$2,$3,$4,true,true)
		ON CONFLICT (company_id, code) DO UPDATE SET name=EXCLUDED.name, type=EXCLUDED.type, is_active=true
	`, companyID, account.Code, account.Name, account.Type)
	return err
}

func insertStandardVoucher(r *http.Request, tx *sql.Tx, companyID, userID, branchID int64, req standardTransactionRequest, occurredAt time.Time, lines []ledgerLine) (int64, error) {
	rawKey := "HY:" + req.ExternalTransactionID
	var voucherID int64
	err := tx.QueryRowContext(r.Context(), `
		INSERT INTO journal_vouchers(company_id, branch_id, voucher_no, voucher_date, description, source_doc_type, status, created_by, posted_at, external_key, source_reference)
		VALUES($1,$2,$3,$4,$5,'HesabYar','Draft',$6,NULL,$7,$8)
		ON CONFLICT (company_id, external_key) WHERE external_key IS NOT NULL
		DO UPDATE SET description=EXCLUDED.description
		RETURNING id
	`, companyID, branchID, truncateText("HY-"+req.ExternalTransactionID, 50), occurredAt.Format("2006-01-02"),
		truncateText(req.Description, 200), nullUserID(userID), truncateText(rawKey, 160), truncateText(req.ExternalTransactionID, 160)).Scan(&voucherID)
	if err != nil {
		return 0, err
	}
	for index, line := range lines {
		accountID, err := ensureLedgerAccount(r.Context(), tx, companyID, line)
		if err != nil {
			return 0, err
		}
		partyID, err := ensureLedgerParty(r.Context(), tx, companyID, line.Party)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO journal_voucher_lines(company_id, journal_voucher_id, account_id, party_id, debit, credit, description, line_no)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (journal_voucher_id, line_no) WHERE line_no IS NOT NULL DO NOTHING
		`, companyID, voucherID, accountID, partyID, moneyRound(line.Debit), moneyRound(line.Credit), truncateText(line.Description, 200), index+1); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE journal_vouchers SET status='Posted', posted_at=CURRENT_TIMESTAMP WHERE company_id=$1 AND id=$2 AND status='Draft'`, companyID, voucherID); err != nil {
		return 0, err
	}
	return voucherID, nil
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
