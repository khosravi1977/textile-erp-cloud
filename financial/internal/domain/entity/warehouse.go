package entity

import "time"

// WarehouseType defines types of warehouses
type WarehouseType string

const (
    WarehouseOwned  WarehouseType = "Owned"
    WarehouseCustody WarehouseType = "Custody"
    WarehouseProduction WarehouseType = "Production"
    WarehouseWaste   WarehouseType = "Waste"
)

// Warehouse represents a storage location
type Warehouse struct {
    ID       int64         `json:"id"`
    BranchID int64         `json:"branch_id"`
    Code     string        `json:"code"`
    Name     string        `json:"name"`
    Type     WarehouseType `json:"type"`
    IsActive bool          `json:"is_active"`
    CreatedAt time.Time    `json:"created_at"`
}

// NewWarehouse creates a new warehouse
func NewWarehouse(branchID int64, code, name string, whType WarehouseType) *Warehouse {
    return &Warehouse{
        BranchID:  branchID,
        Code:      code,
        Name:      name,
        Type:      whType,
        IsActive:  true,
        CreatedAt: time.Now(),
    }
}
