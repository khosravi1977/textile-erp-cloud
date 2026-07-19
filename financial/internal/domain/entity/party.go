package entity

import "time"

type PartyType string

const (
    PartyCustomer   PartyType = "Customer"
    PartySupplier   PartyType = "Supplier"
    PartyContractor PartyType = "Contractor"
    PartyInternal   PartyType = "Internal"
)

type Party struct {
    ID        int64     `json:"id"`
    Code      string    `json:"code"`
    Name      string    `json:"name"`
    Type      PartyType `json:"type"`
    NationalID string   `json:"national_id"`
    TaxID     string    `json:"tax_id"`
    Mobile    string    `json:"mobile"`
    Phone     string    `json:"phone"`
    Address   string    `json:"address"`
    IsActive  bool      `json:"is_active"`
    CreatedAt time.Time `json:"created_at"`
}

func NewParty(code, name string, partyType PartyType) *Party {
    return &Party{
        Code:      code,
        Name:      name,
        Type:      partyType,
        IsActive:  true,
        CreatedAt: time.Now(),
    }
}
