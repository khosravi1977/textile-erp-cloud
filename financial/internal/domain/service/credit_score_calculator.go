package service

import (
    "math"
    "time"
)

type CreditScoreCalculator struct {
    weightOnTimePayment    float64
    weightReturnedChecks   float64
    weightDebtRatio        float64
    weightSuccessfulOrders float64
    weightExcessWaste      float64
    weightDowntime         float64
}

func NewCreditScoreCalculator() *CreditScoreCalculator {
    return &CreditScoreCalculator{
        weightOnTimePayment:    0.25,
        weightReturnedChecks:   0.20,
        weightDebtRatio:        0.15,
        weightSuccessfulOrders: 0.15,
        weightExcessWaste:      0.15,
        weightDowntime:         0.10,
    }
}

type CreditScoreInput struct {
    OnTimePaymentRate   float64
    ReturnedChecksCount int
    DebtToCreditRatio   float64
    SuccessfulOrders    int
    TotalOrders         int
    ExcessWasteRate     float64
    DowntimeDays        int
}

func (csc *CreditScoreCalculator) Calculate(input CreditScoreInput) int {
    score := 0.0
    
    // 1. On-time payment score (25%)
    onTimeScore := input.OnTimePaymentRate * csc.weightOnTimePayment
    score += onTimeScore
    
    // 2. Returned checks score (20%)
    checksScore := 0.0
    if input.ReturnedChecksCount == 0 {
        checksScore = 100 * csc.weightReturnedChecks
    } else if input.ReturnedChecksCount <= 2 {
        checksScore = 60 * csc.weightReturnedChecks
    } else if input.ReturnedChecksCount <= 5 {
        checksScore = 30 * csc.weightReturnedChecks
    }
    score += checksScore
    
    // 3. Debt ratio score (15%)
    debtScore := (1 - input.DebtToCreditRatio) * 100 * csc.weightDebtRatio
    if debtScore < 0 {
        debtScore = 0
    }
    score += debtScore
    
    // 4. Successful orders score (15%)
    if input.TotalOrders > 0 {
        successRate := float64(input.SuccessfulOrders) / float64(input.TotalOrders)
        orderScore := successRate * 100 * csc.weightSuccessfulOrders
        score += orderScore
    } else {
        score += 50 * csc.weightSuccessfulOrders
    }
    
    // 5. Excess waste score (15%)
    wasteScore := 100 * csc.weightExcessWaste
    if input.ExcessWasteRate > 0.05 {
        wasteScore = math.Max(0, (1-input.ExcessWasteRate)*100) * csc.weightExcessWaste
    }
    score += wasteScore
    
    // 6. Downtime score (10%)
    downtimeScore := 100 * csc.weightDowntime
    if input.DowntimeDays > 0 {
        penalty := float64(input.DowntimeDays) * 5
        downtimeScore = math.Max(0, 100-penalty) * csc.weightDowntime
    }
    score += downtimeScore
    
    finalScore := int(math.Round(score))
    
    if finalScore < 0 {
        finalScore = 0
    }
    if finalScore > 100 {
        finalScore = 100
    }
    
    return finalScore
}

func (csc *CreditScoreCalculator) GetRiskGroup(score int) string {
    switch {
    case score >= 70:
        return "Low"
    case score >= 40:
        return "Medium"
    default:
        return "High"
    }
}

func (csc *CreditScoreCalculator) GetCreditTerms(riskGroup string) CreditTerms {
    switch riskGroup {
    case "Low":
        return CreditTerms{
            MaxCreditDays:   60,
            PrepaymentPct:   0,
            AllowCheck:      true,
            AllowBarter:     true,
            CreditMultiplier: 1.0,
        }
    case "Medium":
        return CreditTerms{
            MaxCreditDays:   30,
            PrepaymentPct:   20,
            AllowCheck:      true,
            AllowBarter:     true,
            CreditMultiplier: 0.8,
        }
    case "High":
        return CreditTerms{
            MaxCreditDays:   0,
            PrepaymentPct:   100,
            AllowCheck:      false,
            AllowBarter:     false,
            CreditMultiplier: 0.5,
        }
    }
    return CreditTerms{}
}

type CreditTerms struct {
    MaxCreditDays   int
    PrepaymentPct   int
    AllowCheck      bool
    AllowBarter     bool
    CreditMultiplier float64
}

type ScoreChangeEvent struct {
    PartyID      int64
    OldScore     int
    NewScore     int
    ChangeReason string
    Timestamp    time.Time
}
