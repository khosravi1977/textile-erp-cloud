package service

import (
    "fmt"
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// DowntimeCalculator handles machine downtime penalty calculations
type DowntimeCalculator struct {
    maxAllowedDays int // Maximum allowed delay before auto-block
}

// NewDowntimeCalculator creates a new calculator
func NewDowntimeCalculator(maxAllowedDays int) *DowntimeCalculator {
    return &DowntimeCalculator{
        maxAllowedDays: maxAllowedDays,
    }
}

// DowntimeResult holds the calculation result
type DowntimeResult struct {
    DurationDays    float64
    Amount          valueobject.Money
    ShouldBlock     bool
    BlockReason     string
}

// Calculate calculates downtime penalty
// delayDays: number of days customer delayed
// ratePerDay: machine downtime rate per day
func (dc *DowntimeCalculator) Calculate(
    commitmentDate time.Time,
    actualStartDate time.Time,
    ratePerDay valueobject.Money,
) DowntimeResult {
    // Calculate delay
    delay := actualStartDate.Sub(commitmentDate)
    
    if delay <= 0 {
        // No delay - started on time or early
        return DowntimeResult{
            DurationDays: 0,
            Amount:       valueobject.Zero(),
            ShouldBlock:  false,
        }
    }
    
    // Convert to days (rounded up)
    delayHours := delay.Hours()
    delayDays := delayHours / 24
    
    // Round up to nearest 0.5 day
    delayDays = float64(int((delayDays*2)+0.5)) / 2
    
    // Calculate penalty amount
    penaltyAmount := ratePerDay.Multiply(delayDays)
    
    // Check if should block
    shouldBlock := int(delayDays) > dc.maxAllowedDays
    blockReason := ""
    if shouldBlock {
        blockReason = fmt.Sprintf(
            "Customer delayed %.1f days (max allowed: %d days). New orders blocked.",
            delayDays, dc.maxAllowedDays,
        )
    }
    
    return DowntimeResult{
        DurationDays: delayDays,
        Amount:       penaltyAmount,
        ShouldBlock:  shouldBlock,
        BlockReason:  blockReason,
    }
}

// CreatePenalty creates a MachineIdlePenalty entity
func (dc *DowntimeCalculator) CreatePenalty(
    productionOrderID int64,
    machineID int64,
    result DowntimeResult,
    reason string,
) *entity.MachineIdlePenalty {
    if result.DurationDays <= 0 {
        return nil // No penalty
    }
    
    return entity.NewMachineIdlePenalty(
        productionOrderID,
        machineID,
        "Day",
        result.DurationDays,
        result.Amount, // This will be recalculated internally
        reason,
    )
}
