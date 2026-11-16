package valueobject

import (
	"encoding/json"
	"fmt"
	"math"
)

// Money represents a monetary amount
type Money struct {
	amount   int64 // stored in Rials (lowest unit)
	currency string
}

// NewMoney creates a new Money instance from rials.
func NewMoney(value float64) Money {
	rials := int64(math.Round(value))
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
	return fmt.Sprintf("%d IRR", m.ToRials())
}

// MarshalJSON emits integer rials; JSON never uses a display unit.
func (m Money) MarshalJSON() ([]byte, error) { return json.Marshal(m.amount) }

// UnmarshalJSON accepts integer rials only, without float rounding.
func (m *Money) UnmarshalJSON(data []byte) error {
	var rials int64
	if string(data) == "null" {
		return fmt.Errorf("rial amount is required")
	}
	if err := json.Unmarshal(data, &rials); err != nil {
		return err
	}
	if rials > 9007199254740991 || rials < -9007199254740991 {
		return fmt.Errorf("rial amount exceeds safe range")
	}
	*m = FromRials(rials)
	return nil
}

// Zero returns zero Money
func Zero() Money {
	return Money{amount: 0, currency: "IRR"}
}
