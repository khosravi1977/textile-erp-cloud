package valueobject

import "fmt"

// CreditScore represents customer credit score (0-100)
type CreditScore struct {
    score int
}

// NewCreditScore creates a validated credit score
func NewCreditScore(score int) (CreditScore, error) {
    if score < 0 || score > 100 {
        return CreditScore{}, fmt.Errorf("credit score must be between 0 and 100, got %d", score)
    }
    return CreditScore{score: score}, nil
}

// Value returns the score value
func (cs CreditScore) Value() int {
    return cs.score
}

// RiskGroup returns the risk group based on score
func (cs CreditScore) RiskGroup() string {
    switch {
    case cs.score >= 70:
        return "Low"
    case cs.score >= 40:
        return "Medium"
    default:
        return "High"
    }
}
