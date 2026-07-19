package entity

import (
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// WastageResponsibility defines who is responsible for wastage
type WastageResponsibility string

const (
    WastageByCustomer   WastageResponsibility = "Customer"
    WastageByContractor WastageResponsibility = "Contractor"
)

// CustomerCreditProfile represents credit information for a customer
type CustomerCreditProfile struct {
    ID                    int64                 `json:"id"`
    PartyID               int64                 `json:"party_id"`
    CreditLimit           valueobject.Money     `json:"credit_limit"`
    CreditDays            int                   `json:"credit_days"`
    StdWastageRate        float64               `json:"std_wastage_rate"`
    WastageResponsibility WastageResponsibility `json:"wastage_responsibility"`
    DowntimeRate          valueobject.Money     `json:"downtime_rate"`
    BaseScore             int                   `json:"base_score"`
    RiskGroup             string                `json:"risk_group"`
    IsBlocked             bool                  `json:"is_blocked"`
    BlockReason           string                `json:"block_reason,omitempty"`
    LastScoreUpdate       time.Time             `json:"last_score_update"`
    CreatedAt             time.Time             `json:"created_at"`
}

// NewCustomerCreditProfile creates a new credit profile
func NewCustomerCreditProfile(partyID int64, creditLimit valueobject.Money) *CustomerCreditProfile {
    return &CustomerCreditProfile{
        PartyID:     partyID,
        CreditLimit: creditLimit,
        CreditDays:  30,
        RiskGroup:   "Medium",
        IsBlocked:   false,
        CreatedAt:   time.Now(),
    }
}

// Block blocks the customer from new orders
func (cp *CustomerCreditProfile) Block(reason string) {
    cp.IsBlocked = true
    cp.BlockReason = reason
}

// Unblock removes block
func (cp *CustomerCreditProfile) Unblock() {
    cp.IsBlocked = false
    cp.BlockReason = ""
}
