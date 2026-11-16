package financecore

import (
	"context"
	"database/sql"
	"github.com/erpsystem/textile-erp/internal/platform/monetary"
)

// NormalizeRialRequest is idempotent: conversion changes Currency to IRR and
// preserves the source values. Every linked allocation uses the same unit.
func NormalizeRialRequest(req TransactionRequest) (TransactionRequest, error) {
	unit, err := monetary.Unit(req.Currency, monetary.Rial)
	if err != nil {
		return req, err
	}
	if req.SourceCurrency == "" {
		req.SourceCurrency, req.SourceAmount = unit, req.Amount
	}
	if req.Amount, err = monetary.ToRials(req.Amount, unit); err != nil {
		return req, err
	}
	if req.InterestAmount, err = monetary.ToRials(req.InterestAmount, unit); err != nil {
		return req, err
	}
	req.Allocations = append([]AllocationInput(nil), req.Allocations...)
	for i := range req.Allocations {
		if req.Allocations[i].AllocatedAmount, err = monetary.ToRials(req.Allocations[i].AllocatedAmount, unit); err != nil {
			return req, err
		}
	}
	req.Currency = monetary.Rial
	return req, nil
}

// ApplyCurrencyPartyDelta uses the same AR/AP rules as ordinary posting.
func ApplyCurrencyPartyDelta(ctx context.Context, tx *sql.Tx, companyID int64, req TransactionRequest) error {
	return applyPartyBalance(ctx, tx, companyID, req)
}

// ReverseCurrencyVoucher retains the original lines and links the reversal to
// its original voucher, using the same immutable reversal as ordinary posting.
func ReverseCurrencyVoucher(ctx context.Context, tx *sql.Tx, companyID, voucherID int64) error {
	return reverseVoucher(ctx, tx, companyID, 0, voucherID, "Currency normalization IRT to IRR")
}
