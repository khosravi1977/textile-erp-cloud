package entity

import "time"

// BOM represents Bill of Materials
type BOM struct {
    ID             int64      `json:"id"`
    ProductItemID  int64      `json:"product_item_id"`
    VersionNo      string     `json:"version_no"`
    StdWastePct    float64    `json:"std_waste_pct"`
    EffectiveFrom  time.Time  `json:"effective_from"`
    EffectiveTo    *time.Time `json:"effective_to,omitempty"`
    IsActive       bool       `json:"is_active"`
    Lines          []BOMLine  `json:"lines,omitempty"`
    CreatedAt      time.Time  `json:"created_at"`
}

// BOMLine represents a single line in BOM
type BOMLine struct {
    ID          int64   `json:"id"`
    BOMID       int64   `json:"bom_id"`
    InputItemID int64   `json:"input_item_id"`
    QtyPerUnit  float64 `json:"qty_per_unit"`
    Scrapable   bool    `json:"scrapable"`
}

// NewBOM creates a new BOM
func NewBOM(productItemID int64, versionNo string, stdWastePct float64) *BOM {
    return &BOM{
        ProductItemID: productItemID,
        VersionNo:     versionNo,
        StdWastePct:   stdWastePct,
        EffectiveFrom: time.Now(),
        IsActive:      true,
        CreatedAt:     time.Now(),
    }
}
