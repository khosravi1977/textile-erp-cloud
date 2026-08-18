package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// GetWorkspaceSummaryAccurate returns accounting-safe management KPIs.
// It intentionally keeps the legacy response keys while fixing two integrity
// problems: internal transfers must not change total liquidity, and inventory
// purchases must not be treated as cost of goods sold.
func (h *APIHandler) GetWorkspaceSummaryAccurate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	doc, err := loadWorkspace(r, requestctx.CompanyID(r.Context()))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state := decodeWorkspaceMap(doc.State)
	RespondJSON(w, http.StatusOK, buildWorkspaceSummaryAccurate(state, doc.Revision, doc.UpdatedAt))
}

func buildWorkspaceSummaryAccurate(state map[string]any, revision int64, updatedAt time.Time) map[string]any {
	invoices := rowsFrom(state, "invoices")
	incoming := rowsFrom(state, "incomingInvoices")
	yarnOut := rowsFrom(state, "yarnOutInvoices")
	expenses := rowsFrom(state, "expenses")
	receivables := rowsFrom(state, "receivableDocs")
	payables := rowsFrom(state, "payableDocs")
	accounts := rowsFrom(state, "accounts")
	movements := rowsFrom(state, "movements")

	totalSales := sumField(invoices, "total")
	yarnSales := 0.0
	totalCOGS := sumField(invoices, "costAmount")
	for _, row := range yarnOut {
		mode := strings.ToLower(strings.TrimSpace(stringValue(row["outMode"])))
		if mode == "sale" || mode == "barter" {
			yarnSales += number(row["amount"])
			if !strings.EqualFold(strings.TrimSpace(stringValue(row["stockType"])), "amanat") {
				totalCOGS += number(row["costAmount"])
			}
		}
	}
	totalRevenue := totalSales + yarnSales
	totalPurchases := sumFiltered(incoming, "amount", func(row map[string]any) bool {
		return !boolValue(row["nonFinancial"])
	})
	totalExpenses := sumField(expenses, "amount")
	grossMargin := totalRevenue - totalCOGS
	operatingProfit := grossMargin - totalExpenses

	openReceivables := sumFiltered(receivables, "amount", func(row map[string]any) bool {
		return !statusClosed(row, "cleared")
	})
	openPayables := sumFiltered(payables, "amount", func(row map[string]any) bool {
		return !statusClosed(row, "paid")
	})

	cashBalance := 0.0
	for _, account := range accounts {
		cashBalance += workspaceAccountBalance(account, movements)
	}

	return map[string]any{
		"revision": revision, "updated_at": updatedAt,
		"total_sales": totalSales, "yarn_sales": yarnSales, "total_revenue": totalRevenue,
		"total_purchases": totalPurchases, "total_cogs": totalCOGS, "total_expenses": totalExpenses,
		"gross_margin": grossMargin, "operating_profit": operatingProfit,
		"open_receivables": openReceivables, "open_payables": openPayables, "cash_balance": cashBalance,
		"invoice_count": len(invoices), "incoming_invoice_count": len(incoming), "expense_count": len(expenses),
	}
}

func workspaceAccountBalance(account map[string]any, movements []map[string]any) float64 {
	id := strings.TrimSpace(stringValue(account["id"]))
	balance := number(account["opening"])
	for _, movement := range movements {
		amount := number(movement["amount"])
		direction := strings.ToLower(strings.TrimSpace(stringValue(movement["direction"])))
		if strings.TrimSpace(stringValue(movement["accountId"])) == id {
			if direction == "in" {
				balance += amount
			} else {
				balance -= amount
			}
		}
		if strings.EqualFold(strings.TrimSpace(stringValue(movement["transactionType"])), "transfer") && strings.TrimSpace(stringValue(movement["counterAccountId"])) == id {
			// A transfer is stored once. Its counter account receives the opposite
			// side of the source movement; otherwise total liquidity would change.
			if direction == "in" {
				balance -= amount
			} else {
				balance += amount
			}
		}
	}
	return balance
}
