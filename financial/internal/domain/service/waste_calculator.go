package service

import (
    "fmt"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

type WasteCalculator struct{}

func NewWasteCalculator() *WasteCalculator {
    return &WasteCalculator{}
}

type WasteResult struct {
    StdWasteQty float64 `json:"std_waste_qty"`
    ExcessWasteQty float64 `json:"excess_waste_qty"`
    StdWasteAmount valueobject.Money `json:"std_waste_amount"`
    ExcessWasteAmount valueobject.Money `json:"excess_waste_amount"`
    Description string `json:"description"`
}

func (wc *WasteCalculator) Calculate(
    totalInput float64,
    goodOutput float64,
    stdWasteRate float64,
    agreedPrice valueobject.Money,
) WasteResult {
    // Standard waste is always calculated
    stdWaste := totalInput * stdWasteRate
    
    // Excess waste: actual total waste - standard waste
    actualWaste := totalInput - goodOutput
    excessWaste := actualWaste - stdWaste
    
    // If excess is negative (good output > expected), no excess waste
    if excessWaste < 0 {
        excessWaste = 0
    }
    
    // Calculate amounts
    stdWasteAmount := agreedPrice.Multiply(stdWaste)
    excessWasteAmount := agreedPrice.Multiply(excessWaste)
    
    // Build description
    desc := fmt.Sprintf(
        "Total Input: %.2f, Good Output: %.2f, Std Waste (%.1f%%): %.2f, Excess Waste: %.2f",
        totalInput, goodOutput, stdWasteRate*100, stdWaste, excessWaste,
    )
    
    return WasteResult{
        StdWasteQty:       stdWaste,
        ExcessWasteQty:    excessWaste,
        StdWasteAmount:    stdWasteAmount,
        ExcessWasteAmount: excessWasteAmount,
        Description:       desc,
    }
}

func (wc *WasteCalculator) AllocateExcessWaste(
    productionOrderID int64,
    partyID int64,
    result WasteResult,
    basis string,
) *entity.WasteAllocation {
    if result.ExcessWasteQty <= 0 {
        return nil
    }
    
    return entity.NewWasteAllocation(
        productionOrderID,
        partyID,
        result.ExcessWasteQty,
        result.ExcessWasteAmount,
        basis,
    )
}

