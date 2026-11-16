package financecore

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
	Currency              string            `json:"currency"`
	SourceCurrency        string            `json:"-"`
	SourceAmount          int64             `json:"-"`
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
