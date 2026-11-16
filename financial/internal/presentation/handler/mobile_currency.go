package handler

import (
	"fmt"
	"github.com/erpsystem/textile-erp/internal/platform/monetary"
)

// Old Android releases store whole tomans. Keep that external contract at the
// boundary; modern clients explicitly request IRR. Never round a rial away.
func adaptMobileBootstrap(payload map[string]any, requested string) error {
	unit, err := monetary.Unit(requested, monetary.Toman)
	if err != nil {
		return err
	}
	for _, collection := range []string{"accounts", "receivable_checks", "payable_checks"} {
		rows, ok := payload[collection].([]map[string]any)
		if !ok {
			rows = rowsFrom(payload, collection)
		}
		for _, row := range rows {
			if unit == monetary.Toman && collection == "accounts" {
				delete(row, "opening")
			}
			for _, key := range []string{"amount", "balance", "opening", "lastReportedBalance"} {
				if row[key] == nil {
					continue
				}
				value, err := rialInteger(row[key])
				if err != nil {
					return err
				}
				if unit == monetary.Toman {
					if value%10 != 0 {
						return fmt.Errorf("این نسخه حسابیار مبلغ یک‌ریالی را پشتیبانی نمی‌کند؛ نسخه پشتیبان IRR لازم است")
					}
					row[key] = value / 10
				}
			}
			row["currency"] = unit
		}
	}
	payload["currency"], payload["canonical_currency"] = unit, monetary.Rial
	return nil
}

// The shipped Android client stores/sends toman regardless of its display
// switch. Missing currency is IRT ONLY on that documented mobile contract.
func normalizeMobileMoney(req *mobileTransactionRequest) error {
	unit, err := monetary.Unit(req.Currency, monetary.Toman)
	if err != nil {
		return err
	}
	if req.SourceCurrency == "" {
		req.SourceCurrency, req.SourceAmount = unit, req.Amount
	}
	if req.Amount, err = monetary.ToRials(req.Amount, unit); err != nil {
		return err
	}
	if req.ReportedBalance != nil {
		balance, err := monetary.ToRials(*req.ReportedBalance, unit)
		if err != nil {
			return err
		}
		req.ReportedBalance = &balance
	}
	req.Currency = monetary.Rial
	return nil
}

type mobilePayableDocumentRequest struct {
	ExternalID  string `json:"external_id"`
	Customer    string `json:"customer"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	CheckNo     string `json:"check_no"`
	CheckNoAlt  string `json:"checkNo"`
	Sayad       string `json:"sayad"`
	SayadID     string `json:"sayad_id"`
	DueJalali   string `json:"due_jalali"`
	DueAlt      string `json:"dueJalali"`
	Bank        string `json:"bank"`
	Account     string `json:"account"`
	Beneficiary string `json:"beneficiary"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

type mobileTransactionRequest struct {
	ExternalID      string           `json:"external_id"`
	Title           string           `json:"title"`
	Direction       string           `json:"direction"`
	AccountID       string           `json:"account_id"`
	Group           string           `json:"group"`
	Subgroup        string           `json:"subgroup"`
	Customer        string           `json:"customer"`
	TransactionType string           `json:"transaction_type"`
	Bank            string           `json:"bank"`
	Sender          string           `json:"sender"`
	Description     string           `json:"description"`
	OccurredJalali  string           `json:"occurred_jalali"`
	Amount          int64            `json:"amount"`
	Currency        string           `json:"currency"`
	SourceCurrency  string           `json:"-"`
	SourceAmount    int64            `json:"-"`
	ReportedBalance *int64           `json:"reported_balance"`
	TrackingNo      string           `json:"tracking_no"`
	CounterAccount  string           `json:"counter_account"`
	Groups          []map[string]any `json:"groups"`
	BankRules       []map[string]any `json:"bank_rules"`
}
