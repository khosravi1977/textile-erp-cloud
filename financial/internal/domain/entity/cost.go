package entity

import (
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// CostCategory defines types of costs
type CostCategory string

const (
    CostRawMaterial  CostCategory = "RawMaterial"
    CostLabor        CostCategory = "Labor"
    CostUtility      CostCategory = "Utility"
    CostMaintenance  CostCategory = "Maintenance"
    CostTransport    CostCategory = "Transport"
    CostAdministrative CostCategory = "Administrative"
    CostOther        CostCategory = "Other"
)

// Cost represents an expense record
type Cost struct {
    ID          int64              `json:"id"`
    BranchID    int64              `json:"branch_id"`
    Category    CostCategory       `json:"category"`
    Description string             `json:"description"`
    Amount      valueobject.Money  `json:"amount"`
    PartyID     *int64             `json:"party_id,omitempty"`
    InvoiceNo   string             `json:"invoice_no,omitempty"`
    CostDate    time.Time          `json:"cost_date"`
    CreatedBy   int64              `json:"created_by"`
    CreatedAt   time.Time          `json:"created_at"`
}

// NewCost creates a new cost record
func NewCost(branchID int64, category CostCategory, description string, amount valueobject.Money) *Cost {
    return &Cost{
        BranchID:    branchID,
        Category:    category,
        Description: description,
        Amount:      amount,
        CostDate:    time.Now(),
        CreatedAt:   time.Now(),
    }
}

// CostSummary represents cost report
type CostSummary struct {
    TotalCosts        valueobject.Money `json:"total_costs"`
    ByCategory        map[CostCategory]valueobject.Money `json:"by_category"`
    PeriodStart       time.Time         `json:"period_start"`
    PeriodEnd         time.Time         `json:"period_end"`
    TransactionCount  int               `json:"transaction_count"`
}
