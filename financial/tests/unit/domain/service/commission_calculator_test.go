package service_test

import (
    "testing"
    "github.com/erpsystem/textile-erp/internal/domain/service"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

func TestCommissionCalculator_RealScenario(t *testing.T) {
    wasteCalc := service.NewWasteCalculator()
    downtimeCalc := service.NewDowntimeCalculator(10) // max 10 days
    
    commissionCalc := service.NewCommissionInvoiceCalculator(wasteCalc, downtimeCalc)
    
    // Prices from scenario
    agreedYarnPrice := valueobject.NewMoney(200000)   // 200,000 Toman/kg
    agreedFabricPrice := valueobject.NewMoney(250000) // 250,000 Toman/meter (or kg)
    
    // Calculate commission for the scenario:
    // Input: 1000 kg yarn
    // Output: 950 kg fabric
    // Standard waste rate: 3%
    // Downtime: 3 days
    // Downtime rate: 700,000 Toman/day
    
    calc := commissionCalc.CalculateFromScenario(
        1000,                    // yarnInput (kg)
        950,                     // fabricOutput (kg)
        agreedYarnPrice,         // agreedYarnPrice
        agreedFabricPrice,       // agreedFabricPrice
        0.03,                    // stdWasteRate (3%)
        3,                       // downtimeDays
        valueobject.NewMoney(700000), // downtimeRatePerDay
    )
    
    // Expected calculations:
    // Labor: 950 * 250,000 = 237,500,000 Toman
    expectedLabor := valueobject.NewMoney(237500000)
    if calc.LaborAmount != expectedLabor {
        t.Errorf("Expected labor %s, got %s", expectedLabor.String(), calc.LaborAmount.String())
    }
    
    // Downtime penalty: 3 * 700,000 = 2,100,000 Toman
    expectedDowntime := valueobject.NewMoney(2100000)
    if calc.MachineIdlePenalty != expectedDowntime {
        t.Errorf("Expected downtime %s, got %s", expectedDowntime.String(), calc.MachineIdlePenalty.String())
    }
    
    // Waste debit (excess): 20 kg * 200,000 = 4,000,000 Toman
    expectedWaste := valueobject.NewMoney(4000000)
    if calc.WasteDebitAmount != expectedWaste {
        t.Errorf("Expected waste debit %s, got %s", expectedWaste.String(), calc.WasteDebitAmount.String())
    }
    
    // Total before tax: 237,500,000 + 2,100,000 - 4,000,000 = 235,600,000 Toman
    expectedTotal := valueobject.NewMoney(235600000)
    if calc.TotalBeforeTax != expectedTotal {
        t.Errorf("Expected total %s, got %s", expectedTotal.String(), calc.TotalBeforeTax.String())
    }
    
    // Tax (9%): 235,600,000 * 0.09 = 21,204,000 Toman
    expectedTax := valueobject.NewMoney(21204000)
    if calc.TaxAmount != expectedTax {
        t.Errorf("Expected tax %s, got %s", expectedTax.String(), calc.TaxAmount.String())
    }
    
    // Net: 235,600,000 + 21,204,000 = 256,804,000 Toman
    expectedNet := valueobject.NewMoney(256804000)
    if calc.NetAmount != expectedNet {
        t.Errorf("Expected net %s, got %s", expectedNet.String(), calc.NetAmount.String())
    }
    
    // Print the full calculation
    t.Log("\n" + commissionCalc.PrintCalculation(calc))
    
    t.Log("✓ Commission calculation matches scenario requirements!")
}
