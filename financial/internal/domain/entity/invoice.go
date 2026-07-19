package entity

import (
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// InvoiceStatus defines invoice states
type InvoiceStatus string

const (
    InvoiceDraft          InvoiceStatus = "Draft"
    InvoiceIssued         InvoiceStatus = "Issued"
    InvoicePartiallyPaid  InvoiceStatus = "PartiallyPaid"
    InvoicePaid           InvoiceStatus = "Paid"
    InvoiceOverdue        InvoiceStatus = "Overdue"
    InvoiceCancelled      InvoiceStatus = "Cancelled"
)

// Invoice represents a sales invoice
type Invoice struct {
    ID           int64              `json:"id"`
    InvoiceNo    string             `json:"invoice_no"`
    CustomerID   int64              `json:"customer_id"`
    CustomerName string             `json:"customer_name"`
    IssueDate    time.Time          `json:"issue_date"`
    DueDate      time.Time          `json:"due_date"`
    Status       InvoiceStatus      `json:"status"`
    Lines        []InvoiceLine      `json:"lines"`
    Subtotal     valueobject.Money  `json:"subtotal"`
    Discount     valueobject.Money  `json:"discount"`
    TaxRate      float64            `json:"tax_rate"`
    TaxAmount    valueobject.Money  `json:"tax_amount"`
    TotalAmount  valueobject.Money  `json:"total_amount"`
    PaidAmount   valueobject.Money  `json:"paid_amount"`
    Balance      valueobject.Money  `json:"balance"`
    Notes        string             `json:"notes,omitempty"`
    CreatedBy    string             `json:"created_by"`
    CreatedAt    time.Time          `json:"created_at"`
}

// InvoiceLine represents a single line in an invoice
type InvoiceLine struct {
    ID          int64              `json:"id"`
    ItemID      int64              `json:"item_id"`
    ItemName    string             `json:"item_name"`
    Description string             `json:"description"`
    Qty         float64            `json:"qty"`
    Unit        string             `json:"unit"`
    UnitPrice   valueobject.Money  `json:"unit_price"`
    LineTotal   valueobject.Money  `json:"line_total"`
}

// NewInvoice creates a new invoice
func NewInvoice(invoiceNo string, customerID int64, customerName string) *Invoice {
    return &Invoice{
        InvoiceNo:    invoiceNo,
        CustomerID:   customerID,
        CustomerName: customerName,
        IssueDate:    time.Now(),
        DueDate:      time.Now().AddDate(0, 0, 30),
        Status:       InvoiceDraft,
        TaxRate:      0.09,
        CreatedAt:    time.Now(),
    }
}

// AddLine adds a line item to the invoice
func (inv *Invoice) AddLine(itemID int64, itemName, description string, qty float64, unit string, unitPrice valueobject.Money) {
    lineTotal := unitPrice.Multiply(qty)
    line := InvoiceLine{
        ID:          int64(len(inv.Lines) + 1),
        ItemID:      itemID,
        ItemName:    itemName,
        Description: description,
        Qty:         qty,
        Unit:        unit,
        UnitPrice:   unitPrice,
        LineTotal:   lineTotal,
    }
    inv.Lines = append(inv.Lines, line)
    inv.Recalculate()
}

// Recalculate recalculates invoice totals
func (inv *Invoice) Recalculate() {
    subtotal := valueobject.Zero()
    for _, line := range inv.Lines {
        subtotal = subtotal.Add(line.LineTotal)
    }
    inv.Subtotal = subtotal
    inv.TaxAmount = subtotal.Multiply(inv.TaxRate)
    inv.TotalAmount = subtotal.Add(inv.TaxAmount).Subtract(inv.Discount)
    inv.Balance = inv.TotalAmount.Subtract(inv.PaidAmount)
}

// AddPayment records a payment against the invoice
func (inv *Invoice) AddPayment(amount valueobject.Money) {
    inv.PaidAmount = inv.PaidAmount.Add(amount)
    inv.Recalculate()
    
    if inv.Balance.ToRials() <= 0 {
        inv.Status = InvoicePaid
    } else if inv.PaidAmount.ToRials() > 0 {
        inv.Status = InvoicePartiallyPaid
    }
}

// Issue marks the invoice as issued
func (inv *Invoice) Issue() {
    inv.Status = InvoiceIssued
}

// CheckOverdue checks if invoice is past due
func (inv *Invoice) CheckOverdue() {
    if inv.Status != InvoicePaid && inv.Status != InvoiceCancelled {
        if time.Now().After(inv.DueDate) {
            inv.Status = InvoiceOverdue
        }
    }
}

// InvoiceReport represents a sales report
type InvoiceReport struct {
    TotalInvoices   int                `json:"total_invoices"`
    TotalSales      valueobject.Money  `json:"total_sales"`
    TotalCollected  valueobject.Money  `json:"total_collected"`
    TotalOutstanding valueobject.Money `json:"total_outstanding"`
    OverdueCount    int                `json:"overdue_count"`
    PaidCount       int                `json:"paid_count"`
    PeriodStart     time.Time          `json:"period_start"`
    PeriodEnd       time.Time          `json:"period_end"`
}
