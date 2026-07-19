package entity

import (
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

type OwnershipType string

const (
    OwnershipOwned  OwnershipType = "Owned"
    OwnershipCustody OwnershipType = "Custody"
)

type InventoryLot struct {
    ID             int64              `json:"id"`
    ItemID         int64              `json:"item_id"`
    WarehouseID    int64              `json:"warehouse_id"`
    OwnerPartyID   *int64             `json:"owner_party_id,omitempty"`
    LotNo          string             `json:"lot_no"`
    ReceiptDate    time.Time          `json:"receipt_date"`
    ExpiryDate     *time.Time         `json:"expiry_date,omitempty"`
    QtyOnHand      float64            `json:"qty_on_hand"`
    QtyReserved    float64            `json:"qty_reserved"`
    UnitCost       valueobject.Money  `json:"unit_cost"`
    AgreedPrice    *valueobject.Money `json:"agreed_price,omitempty"`
    OwnershipType  OwnershipType      `json:"ownership_type"`
    Status         string             `json:"status"`
    CreatedAt      time.Time          `json:"created_at"`
}

func (lot *InventoryLot) AvailableQty() float64 {
    return lot.QtyOnHand - lot.QtyReserved
}

func (lot *InventoryLot) Reserve(qty float64) error {
    if lot.AvailableQty() < qty {
        return ErrInsufficientQuantity
    }
    lot.QtyReserved += qty
    return nil
}

func (lot *InventoryLot) Issue(qty float64) error {
    if lot.AvailableQty() < qty {
        return ErrInsufficientQuantity
    }
    lot.QtyOnHand -= qty
    return nil
}

// Entity errors
type entityError string

func (e entityError) Error() string {
    return string(e)
}

var ErrInsufficientQuantity = entityError("insufficient quantity in lot")
