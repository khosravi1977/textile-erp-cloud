package entity

import "time"

// ItemType defines types of items
type ItemType string

const (
    ItemYarn       ItemType = "Yarn"
    ItemFabric     ItemType = "Fabric"
    ItemWaste      ItemType = "Waste"
    ItemRawMaterial ItemType = "RawMaterial"
    ItemService    ItemType = "Service"
)

// Item represents a product or material
type Item struct {
    ID             int64     `json:"id"`
    Code           string    `json:"code"`
    Name           string    `json:"name"`
    Type           ItemType  `json:"type"`
    UomID          int64     `json:"uom_id"`
    CategoryID     int64     `json:"category_id"`
    IsInventory    bool      `json:"is_inventory"`
    ConversionRate float64   `json:"conversion_rate"`
    StdWastageRate float64   `json:"std_wastage_rate"`
    UnitWeight     float64   `json:"unit_weight"`
    IsActive       bool      `json:"is_active"`
    CreatedAt      time.Time `json:"created_at"`
}

// NewItem creates a new item
func NewItem(code, name string, itemType ItemType) *Item {
    return &Item{
        Code:        code,
        Name:        name,
        Type:        itemType,
        IsInventory: true,
        IsActive:    true,
        CreatedAt:   time.Now(),
    }
}
