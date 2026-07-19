package entity

import (
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// CommissionInvoice represents a کارمزد invoice
type CommissionInvoice struct {
    ID                      int64             `json:"id"`
    BranchID                int64             `json:"branch_id"`
    InvoiceNo              string            `json:"invoice_no"`
    PartyID                int64             `json:"party_id"`
    ProductionOrderID      int64             `json:"production_order_id"`
    LaborAmount            valueobject.Money `json:"labor_amount"`
    MachineIdlePenaltyAmount valueobject.Money `json:"machine_idle_penalty_amount"`
    WasteDebitAmount       valueobject.Money `json:"waste_debit_amount"`
    Discount               valueobject.Money `json:"discount"`
    TotalAmount            valueobject.Money `json:"total_amount"`
    TaxAmount              valueobject.Money `json:"tax_amount"`
    NetAmount              valueobject.Money `json:"net_amount"`
    Status                 string            `json:"status"` // Draft/Issued/PartiallySettled/Settled
    IssuedDate             time.Time         `json:"issued_date"`
    DueDate                time.Time         `json:"due_date"`
    CreatedBy              int64             `json:"created_by"`
    CreatedAt              time.Time         `json:"created_at"`
}

// NewCommissionInvoice creates a new commission invoice
func NewCommissionInvoice(invoiceNo string, partyID, productionOrderID int64, laborAmount valueobject.Money) *CommissionInvoice {
    return &CommissionInvoice{
        InvoiceNo:         invoiceNo,
        PartyID:           partyID,
        ProductionOrderID: productionOrderID,
        LaborAmount:       laborAmount,
        TotalAmount:       laborAmount,
        TaxAmount:         valueobject.Zero(),
        NetAmount:         laborAmount,
        Status:            "Draft",
        IssuedDate:        time.Now(),
        DueDate:           time.Now().AddDate(0, 0, 30),
        CreatedAt:         time.Now(),
    }
}

// CalculateNetAmount calculates the net amount
func (ci *CommissionInvoice) CalculateNetAmount() {
    ci.TotalAmount = ci.LaborAmount.Add(ci.MachineIdlePenaltyAmount).Subtract(ci.WasteDebitAmount).Subtract(ci.Discount)
    ci.TaxAmount = ci.TotalAmount.Multiply(0.09) // 9% VAT
    ci.NetAmount = ci.TotalAmount.Add(ci.TaxAmount)
}
