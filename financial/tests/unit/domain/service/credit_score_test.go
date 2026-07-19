package service_test

import (
    "testing"
    "github.com/erpsystem/textile-erp/internal/domain/service"
)

func TestCreditScoreCalculator(t *testing.T) {
    calc := service.NewCreditScoreCalculator()
    
    // Test case 1: Perfect customer
    perfectInput := service.CreditScoreInput{
        OnTimePaymentRate:   100,
        ReturnedChecksCount: 0,
        DebtToCreditRatio:   0.1,
        SuccessfulOrders:    10,
        TotalOrders:         10,
        ExcessWasteRate:     0.01,
        DowntimeDays:        0,
    }
    
    perfectScore := calc.Calculate(perfectInput)
    if perfectScore < 90 {
        t.Errorf("Perfect customer should score >= 90, got %d", perfectScore)
    }
    t.Logf("Perfect customer score: %d, Risk: %s", perfectScore, calc.GetRiskGroup(perfectScore))
    
    // Test case 2: High risk customer
    riskyInput := service.CreditScoreInput{
        OnTimePaymentRate:   30,
        ReturnedChecksCount: 6,
        DebtToCreditRatio:   0.9,
        SuccessfulOrders:    2,
        TotalOrders:         10,
        ExcessWasteRate:     0.15,
        DowntimeDays:        15,
    }
    
    riskyScore := calc.Calculate(riskyInput)
    if riskyScore > 30 {
        t.Errorf("Risky customer should score <= 30, got %d", riskyScore)
    }
    t.Logf("Risky customer score: %d, Risk: %s", riskyScore, calc.GetRiskGroup(riskyScore))
    
    // Test credit terms
    terms := calc.GetCreditTerms("Low")
    if terms.MaxCreditDays != 60 || terms.PrepaymentPct != 0 {
        t.Error("Low risk terms incorrect")
    }
    t.Logf("Low risk terms: %d days credit, %d%% prepayment", terms.MaxCreditDays, terms.PrepaymentPct)
    
    terms = calc.GetCreditTerms("High")
    if terms.AllowCheck || terms.AllowBarter {
        t.Error("High risk should not allow check or barter")
    }
    t.Logf("High risk terms: Cash only")
}
