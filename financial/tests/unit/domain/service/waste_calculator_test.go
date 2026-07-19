package service_test

import (
    "testing"
    "github.com/erpsystem/textile-erp/internal/domain/service"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

func TestWasteCalculator_RealScenario(t *testing.T) {
    calc := service.NewWasteCalculator()
    agreedPrice := valueobject.NewMoney(200000) // 200,000 Toman per kg
    
    // Scenario: 1000 kg yarn input, 950 kg fabric output, 3% standard waste
    result := calc.Calculate(
        1000,   // totalInput (kg)
        950,    // goodOutput (kg)
        0.03,   // stdWasteRate (3%)
        agreedPrice,
    )
    
    // Expected:
    // Standard waste: 1000 * 0.03 = 30 kg
    // Actual waste: 1000 - 950 = 50 kg
    // Excess waste: 50 - 30 = 20 kg
    
    if result.StdWasteQty != 30 {
        t.Errorf("Expected std waste 30, got %.2f", result.StdWasteQty)
    }
    
    if result.ExcessWasteQty != 20 {
        t.Errorf("Expected excess waste 20, got %.2f", result.ExcessWasteQty)
    }
    
    // Amounts (in Toman)
    // Std waste amount: 30 * 200,000 = 6,000,000 Toman
    expectedStdAmount := valueobject.NewMoney(6000000)
    if result.StdWasteAmount != expectedStdAmount {
        t.Errorf("Expected std amount %s, got %s", expectedStdAmount.String(), result.StdWasteAmount.String())
    }
    
    // Excess waste amount: 20 * 200,000 = 4,000,000 Toman
    expectedExcessAmount := valueobject.NewMoney(4000000)
    if result.ExcessWasteAmount != expectedExcessAmount {
        t.Errorf("Expected excess amount %s, got %s", expectedExcessAmount.String(), result.ExcessWasteAmount.String())
    }
    
    t.Logf("✓ Waste calculation: %s", result.Description)
}
