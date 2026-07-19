package entity

import (
    "time"
)

// Branch represents a company branch
type Branch struct {
    ID        int64     `json:"id"`
    Code      string    `json:"code"`
    Name      string    `json:"name"`
    Address   string    `json:"address"`
    Phone     string    `json:"phone"`
    IsActive  bool      `json:"is_active"`
    CreatedAt time.Time `json:"created_at"`
    CreatedBy int64     `json:"created_by"`
}

// NewBranch creates a new Branch instance
func NewBranch(code, name, address, phone string, createdBy int64) *Branch {
    return &Branch{
        Code:      code,
        Name:      name,
        Address:   address,
        Phone:     phone,
        IsActive:  true,
        CreatedAt: time.Now(),
        CreatedBy: createdBy,
    }
}

// Deactivate deactivates the branch
func (b *Branch) Deactivate() {
    b.IsActive = false
}

// Activate activates the branch
func (b *Branch) Activate() {
    b.IsActive = true
}
