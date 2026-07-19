package service

import (
    "fmt"
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// CommissionInvoiceCalculator calculates commission invoice totals
type CommissionInvoiceCalculator struct {
    wasteCalculator     *WasteCalculator
    downtimeCalculator  *DowntimeCalculator
}

// NewCommissionInvoiceCalculator creates a new calculator
func NewCommissionInvoiceCalculator(
    wasteCalc *WasteCalculator,
    downtimeCalc *DowntimeCalculator,
) *CommissionInvoiceCalculator {
    return &CommissionInvoiceCalculator{
        wasteCalculator:    wasteCalc,
        downtimeCalculator: downtimeCalc,
    }
}

// CommissionCalculation holds all calculated values
type CommissionCalculation struct {
    LaborAmount valueobject.Money `json:"labor_amount"`
    MachineIdlePenalty valueobject.Money `json:"machine_idle_penalty"`
    WasteDebitAmount valueobject.Money `json:"waste_debit_amount"`
    TotalBeforeTax valueobject.Money `json:"total_before_tax"`
    TaxAmount valueobject.Money `json:"tax_amount"`
    NetAmount valueobject.Money `json:"net_amount"`
}

// CalculateFromScenario calculates commission from complete production scenario
// This matches the test scenario from the requirements
func (cic *CommissionInvoiceCalculator) CalculateFromScenario(
    yarnInput float64,
    fabricOutput float64,
    agreedYarnPrice valueobject.Money,
    agreedFabricPrice valueobject.Money,
    stdWasteRate float64,
    downtimeDays float64,
    downtimeRatePerDay valueobject.Money,
) CommissionCalculation {
    // 1. Calculate labor amount (value of fabric produced)
    laborAmount := agreedFabricPrice.Multiply(fabricOutput)
    
    // 2. Calculate waste
    wasteResult := cic.wasteCalculator.Calculate(
        yarnInput,
        fabricOutput,
        stdWasteRate,
        agreedYarnPrice,
    )
    
    // Excess waste is debited from contractor/customer
    wasteDebit := wasteResult.ExcessWasteAmount
    
    // 3. Calculate downtime penalty
    downtimePenalty := downtimeRatePerDay.Multiply(downtimeDays)
    
    // 4. Calculate totals
    totalBeforeTax := laborAmount.Add(downtimePenalty).Subtract(wasteDebit)
    taxAmount := totalBeforeTax.Multiply(0.09) // 9% VAT
    netAmount := totalBeforeTax.Add(taxAmount)
    
    return CommissionCalculation{
        LaborAmount:        laborAmount,
        MachineIdlePenalty: downtimePenalty,
        WasteDebitAmount:   wasteDebit,
        TotalBeforeTax:     totalBeforeTax,
        TaxAmount:          taxAmount,
        NetAmount:          netAmount,
    }
}

// CreateInvoice creates a CommissionInvoice entity from calculation
func (cic *CommissionInvoiceCalculator) CreateInvoice(
    invoiceNo string,
    partyID int64,
    productionOrderID int64,
    calc CommissionCalculation,
    dueDate time.Time,
) *entity.CommissionInvoice {
    invoice := entity.NewCommissionInvoice(
        invoiceNo,
        partyID,
        productionOrderID,
        calc.LaborAmount,
    )
    
    invoice.MachineIdlePenaltyAmount = calc.MachineIdlePenalty
    invoice.WasteDebitAmount = calc.WasteDebitAmount
    invoice.TotalAmount = calc.TotalBeforeTax
    invoice.TaxAmount = calc.TaxAmount
    invoice.NetAmount = calc.NetAmount
    invoice.DueDate = dueDate
    invoice.IssuedDate = time.Now()
    
    return invoice
}

// PrintCalculation returns a formatted string of the calculation
func (cic *CommissionInvoiceCalculator) PrintCalculation(calc CommissionCalculation) string {
    return fmt.Sprintf(`
Commission Invoice Calculation:
================================
Labor Amount:           %s
Downtime Penalty:       %s
Waste Debit:           -%s
------------------------
Subtotal:               %s
Tax (9%%):               %s
------------------------
Net Amount:             %s
`,
        calc.LaborAmount.String(),
        calc.MachineIdlePenalty.String(),
        calc.WasteDebitAmount.String(),
        calc.TotalBeforeTax.String(),
        calc.TaxAmount.String(),
        calc.NetAmount.String(),
    )
}

