package entity

import "time"

// TaxInvoice represents a tax invoice for government system
type TaxInvoice struct {
    ID                   int64     `json:"id"`
    CommissionInvoiceID  int64     `json:"commission_invoice_id"`
    TaxUniqueCode        string    `json:"tax_unique_code"`
    SendStatus           string    `json:"send_status"` // NotSent/Sent/Accepted/Rejected
    SendAt               *time.Time `json:"send_at,omitempty"`
    ResponseJSON         string    `json:"response_json,omitempty"`
    CreatedAt            time.Time `json:"created_at"`
}

// AuditLog represents an audit trail entry
type AuditLog struct {
    ID         int64     `json:"id"`
    EntityName string    `json:"entity_name"`
    EntityID   int64     `json:"entity_id"`
    ActionType string    `json:"action_type"` // Create/Update/Delete
    BeforeData string    `json:"before_data,omitempty"` // JSON
    AfterData  string    `json:"after_data,omitempty"`  // JSON
    UserID     int64     `json:"user_id"`
    Timestamp  time.Time `json:"timestamp"`
    BranchID   int64     `json:"branch_id"`
    IPAddress  string    `json:"ip_address"`
}
