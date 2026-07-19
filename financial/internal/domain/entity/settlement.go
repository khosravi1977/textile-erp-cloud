package entity

import (
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

type SettlementType string

const (
    SettlementCash             SettlementType = "Cash"
    SettlementCheck            SettlementType = "Check"
    SettlementProduct          SettlementType = "Product"
    SettlementMaterial         SettlementType = "Material"
    SettlementInternalTransfer SettlementType = "InternalTransfer"
)

type SettlementHeader struct {
    ID             int64             `json:"id"`
    PartyID        int64             `json:"party_id"`
    SettlementDate time.Time         `json:"settlement_date"`
    TotalAmount    valueobject.Money `json:"total_amount"`
    Status         string            `json:"status"`
    Lines          []SettlementLine  `json:"lines,omitempty"`
    CreatedBy      int64             `json:"created_by"`
    CreatedAt      time.Time         `json:"created_at"`
}

type SettlementLine struct {
    ID             int64              `json:"id"`
    SettlementID   int64              `json:"settlement_id"`
    SettlementType SettlementType     `json:"settlement_type"`
    Amount         valueobject.Money  `json:"amount"`
    CheckNo        string             `json:"check_no,omitempty"`
    CheckDueDate   *time.Time         `json:"check_due_date,omitempty"`
    BankName       string             `json:"bank_name,omitempty"`
    ItemID         *int64             `json:"item_id,omitempty"`
    Qty            *float64           `json:"qty,omitempty"`
    CreatedAt      time.Time          `json:"created_at"`
}

func NewSettlement(partyID int64, createdBy int64) *SettlementHeader {
    return &SettlementHeader{
        PartyID:        partyID,
        SettlementDate: time.Now(),
        TotalAmount:    valueobject.Zero(),
        Status:         "Draft",
        CreatedBy:      createdBy,
        CreatedAt:      time.Now(),
    }
}

func (s *SettlementHeader) AddCashLine(amount valueobject.Money) {
    line := SettlementLine{
        SettlementType: SettlementCash,
        Amount:         amount,
        CreatedAt:      time.Now(),
    }
    s.Lines = append(s.Lines, line)
    s.TotalAmount = s.TotalAmount.Add(amount)
}

func (s *SettlementHeader) AddCheckLine(amount valueobject.Money, checkNo, bankName string, dueDate time.Time) {
    line := SettlementLine{
        SettlementType: SettlementCheck,
        Amount:         amount,
        CheckNo:        checkNo,
        BankName:       bankName,
        CheckDueDate:   &dueDate,
        CreatedAt:      time.Now(),
    }
    s.Lines = append(s.Lines, line)
    s.TotalAmount = s.TotalAmount.Add(amount)
}
