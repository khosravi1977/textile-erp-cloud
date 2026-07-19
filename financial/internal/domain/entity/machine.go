package entity

import (
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// Machine represents a production machine
type Machine struct {
    ID               int64              `json:"id"`
    BranchID         int64              `json:"branch_id"`
    Code             string             `json:"code"`
    Name             string             `json:"name"`
    Type             string             `json:"type"`
    BaseDowntimeRate valueobject.Money  `json:"base_downtime_rate"`
    IsActive         bool               `json:"is_active"`
    CreatedAt        time.Time          `json:"created_at"`
}

// MachineIdlePenalty represents a penalty for machine downtime
type MachineIdlePenalty struct {
    ID                int64              `json:"id"`
    ProductionOrderID int64              `json:"production_order_id"`
    MachineID         int64              `json:"machine_id"`
    PenaltyType       string             `json:"penalty_type"` // Hour/Day
    DurationValue     float64            `json:"duration_value"`
    RatePerUnit       valueobject.Money  `json:"rate_per_unit"`
    Amount            valueobject.Money  `json:"amount"`
    Reason            string             `json:"reason"`
    StartDate         time.Time          `json:"start_date"`
    EndDate           time.Time          `json:"end_date"`
    Status            string             `json:"status"` // Pending/Invoiced/Paid
    CreatedAt         time.Time          `json:"created_at"`
}

// NewMachineIdlePenalty creates a new penalty
func NewMachineIdlePenalty(productionOrderID, machineID int64, penaltyType string, duration float64, rate valueobject.Money, reason string) *MachineIdlePenalty {
    amount := rate.Multiply(duration)
    return &MachineIdlePenalty{
        ProductionOrderID: productionOrderID,
        MachineID:         machineID,
        PenaltyType:       penaltyType,
        DurationValue:     duration,
        RatePerUnit:       rate,
        Amount:            amount,
        Reason:            reason,
        StartDate:         time.Now(),
        Status:            "Pending",
        CreatedAt:         time.Now(),
    }
}

// WasteAllocation represents allocation of excess waste
type WasteAllocation struct {
    ID                int64              `json:"id"`
    ProductionOrderID int64              `json:"production_order_id"`
    ExcessWasteQty    float64            `json:"excess_waste_qty"`
    AllocatedToPartyID int64             `json:"allocated_to_party_id"`
    DebitAmount       valueobject.Money  `json:"debit_amount"`
    Basis             string             `json:"basis"`
    Status            string             `json:"status"` // Pending/Invoiced/Settled
    CreatedAt         time.Time          `json:"created_at"`
}

// NewWasteAllocation creates a new waste allocation
func NewWasteAllocation(productionOrderID, partyID int64, qty float64, amount valueobject.Money, basis string) *WasteAllocation {
    return &WasteAllocation{
        ProductionOrderID:  productionOrderID,
        ExcessWasteQty:     qty,
        AllocatedToPartyID: partyID,
        DebitAmount:        amount,
        Basis:              basis,
        Status:             "Pending",
        CreatedAt:          time.Now(),
    }
}
