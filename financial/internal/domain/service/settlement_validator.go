package service

import (
    "fmt"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// SettlementValidator validates settlement operations
type SettlementValidator struct{}

// NewSettlementValidator creates a new validator
func NewSettlementValidator() *SettlementValidator {
    return &SettlementValidator{}
}

// ValidationResult holds validation result
type ValidationResult struct {
    IsValid bool
    Errors  []string
    Warnings []string
}

// ValidateSettlement validates a settlement against customer credit limits
func (sv *SettlementValidator) ValidateSettlement(
    settlement *entity.SettlementHeader,
    customerProfile *entity.CustomerCreditProfile,
    currentDebt valueobject.Money,
) ValidationResult {
    result := ValidationResult{
        IsValid:  true,
        Errors:   []string{},
        Warnings: []string{},
    }
    
    // Check each line
    for i, line := range settlement.Lines {
        switch line.SettlementType {
        case entity.SettlementCheck:
            // Validate check acceptance based on risk group
            if customerProfile.RiskGroup == "High" {
                result.IsValid = false
                result.Errors = append(result.Errors, 
                    fmt.Sprintf("Line %d: Checks not allowed for High risk customers", i+1))
            }
            
            // Check if check due date is within credit days
            if line.CheckDueDate != nil {
                daysUntilDue := line.CheckDueDate.Sub(settlement.SettlementDate).Hours() / 24
                if int(daysUntilDue) > customerProfile.CreditDays {
                    result.Warnings = append(result.Warnings,
                        fmt.Sprintf("Line %d: Check due date (%d days) exceeds credit days (%d)",
                            i+1, int(daysUntilDue), customerProfile.CreditDays))
                }
            }
            
        case entity.SettlementProduct, entity.SettlementMaterial:
            // Validate barter based on risk group
            if customerProfile.RiskGroup == "High" {
                result.IsValid = false
                result.Errors = append(result.Errors,
                    fmt.Sprintf("Line %d: Barter not allowed for High risk customers", i+1))
            }
            
            // Verify product/material exists and has valid price
            if line.ItemID == nil || line.Qty == nil || *line.Qty <= 0 {
                result.IsValid = false
                result.Errors = append(result.Errors,
                    fmt.Sprintf("Line %d: Invalid product/material settlement details", i+1))
            }
            
        case entity.SettlementInternalTransfer:
            // Internal transfer validation
            if line.Amount.IsNegative() || line.Amount.ToRials() == 0 {
                result.IsValid = false
                result.Errors = append(result.Errors,
                    fmt.Sprintf("Line %d: Invalid transfer amount", i+1))
            }
        }
    }
    
    // Check total settlement vs credit limit
    newDebt := currentDebt.Subtract(settlement.TotalAmount)
    if newDebt.IsGreaterThan(customerProfile.CreditLimit) {
        result.Warnings = append(result.Warnings,
            fmt.Sprintf("After settlement, debt (%.0f) still exceeds credit limit (%.0f)",
                newDebt.ToToman(), customerProfile.CreditLimit.ToToman()))
    }
    
    return result
}
