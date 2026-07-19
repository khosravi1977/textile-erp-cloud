package valueobject

import (
    "encoding/json"
    "fmt"
    "math"
)

// Money represents a monetary amount
type Money struct {
    amount   int64  // stored in Rials (lowest unit)
    currency string
}

// NewMoney creates a new Money instance from Toman
func NewMoney(toman float64) Money {
    rials := int64(math.Round(toman * 10))
    return Money{amount: rials, currency: "IRR"}
}

// FromRials creates Money from Rials
func FromRials(rials int64) Money {
    return Money{amount: rials, currency: "IRR"}
}

// ToToman converts to Toman
func (m Money) ToToman() float64 {
    return float64(m.amount) / 10.0
}

// ToRials returns amount in Rials
func (m Money) ToRials() int64 {
    return m.amount
}

// Add adds two Money values
func (m Money) Add(other Money) Money {
    return Money{amount: m.amount + other.amount, currency: "IRR"}
}

// Subtract subtracts two Money values
func (m Money) Subtract(other Money) Money {
    return Money{amount: m.amount - other.amount, currency: "IRR"}
}

// Multiply multiplies Money by a factor
func (m Money) Multiply(factor float64) Money {
    return Money{amount: int64(float64(m.amount) * factor), currency: "IRR"}
}

// IsGreaterThan checks if this Money is greater than another
func (m Money) IsGreaterThan(other Money) bool {
    return m.amount > other.amount
}

// IsNegative checks if the amount is negative
func (m Money) IsNegative() bool {
    return m.amount < 0
}

// String returns string representation
func (m Money) String() string {
    return fmt.Sprintf("%.0f Toman", m.ToToman())
}

// MarshalJSON implements json.Marshaler - outputs Toman value
func (m Money) MarshalJSON() ([]byte, error) {
    return []byte(fmt.Sprintf("%.0f", m.ToToman())), nil
}

// UnmarshalJSON implements json.Unmarshaler - expects Toman value
func (m *Money) UnmarshalJSON(data []byte) error {
    var toman float64
    if err := json.Unmarshal(data, &toman); err != nil {
        return err
    }
    *m = NewMoney(toman)
    return nil
}

// Zero returns zero Money
func Zero() Money {
    return Money{amount: 0, currency: "IRR"}
}
