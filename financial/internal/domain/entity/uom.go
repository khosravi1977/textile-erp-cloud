package entity

// UnitOfMeasure represents measurement units
type UnitOfMeasure struct {
    ID   int64  `json:"id"`
    Code string `json:"code"`
    Name string `json:"name"`
}

// NewUnitOfMeasure creates a new UOM
func NewUnitOfMeasure(code, name string) *UnitOfMeasure {
    return &UnitOfMeasure{
        Code: code,
        Name: name,
    }
}
