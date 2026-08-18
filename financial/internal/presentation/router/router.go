package router

import (
	"github.com/erpsystem/textile-erp/internal/application/telegramreport"
	"github.com/erpsystem/textile-erp/internal/presentation/handler"
	"github.com/erpsystem/textile-erp/internal/presentation/middleware"
	"net/http"
)

func SetupRouter(services ...*telegramreport.Service) http.Handler {
	var telegram *telegramreport.Service
	if len(services) > 0 {
		telegram = services[0]
	}
	h := handler.NewAPIHandler(telegram)
	mux := http.NewServeMux()

	mux.HandleFunc("/health", h.HealthCheck)
	mux.HandleFunc("/metrics", middleware.MetricsHandler)
	mux.HandleFunc("/api/auth/login", h.Login)
	mux.HandleFunc("/api/management-report", h.ManagementReport)
	mux.HandleFunc("/api/mobile/pairing", h.CreateMobilePairing)
	mux.HandleFunc("/api/mobile/pair", h.PairMobileDevice)
	mux.HandleFunc("/api/mobile/bootstrap", h.MobileBootstrap)
	mux.HandleFunc("/api/mobile/settings", h.MobileSettings)
	mux.HandleFunc("/api/mobile/transactions", h.MobileTransaction)
	mux.HandleFunc("/api/mobile/payable-documents", h.MobilePayableDocument)
	mux.HandleFunc("/api/production/orders", h.CreateProductionOrder)
	mux.HandleFunc("/api/production/complete", h.CompleteProduction)
	mux.HandleFunc("/api/settlements", h.CreateSettlement)
	mux.HandleFunc("/api/advisor/advice", h.GetAdvice)
	mux.HandleFunc("/api/advisor/credit-report/", h.GetCreditReport)
	mux.HandleFunc("/api/intelligence/ai-advisor", h.GenerateAIAnalysis)
	mux.HandleFunc("/api/commission/calculate", h.CalculateCommission)
	mux.HandleFunc("/api/costs", h.AddCost)
	mux.HandleFunc("/api/costs/", h.GetCosts)
	mux.HandleFunc("/api/costs/summary", h.GetCostSummary)
	mux.HandleFunc("/api/costs/profitability", h.GetProfitability)
	mux.HandleFunc("/api/inventory", h.GetInventory)
	mux.HandleFunc("/api/inventory/stock-in", h.StockIn)
	mux.HandleFunc("/api/inventory/stock-out", h.StockOut)
	mux.HandleFunc("/api/inventory/summary", h.GetInventorySummary)
	mux.HandleFunc("/api/inventory/alerts", h.GetStockAlerts)
	mux.HandleFunc("/api/inventory/transactions", h.GetInventoryTransactions)
	mux.HandleFunc("/api/workspace", h.WorkspaceRoot)
	mux.HandleFunc("/api/workspace/history", h.GetWorkspaceHistory)
	mux.HandleFunc("/api/workspace/summary", h.GetWorkspaceSummary)
	mux.HandleFunc("/api/workspace/alerts", h.GetWorkspaceAlerts)
	mux.HandleFunc("/api/accounting/reports", h.GetAccountingReports)
	mux.HandleFunc("/api/accounting/periods", h.AccountingPeriods)
	mux.HandleFunc("/api/telegram-reports/config", h.TelegramReportConfig)
	mux.HandleFunc("/api/telegram-reports/pairing", h.TelegramReportPairing)
	mux.HandleFunc("/api/telegram-reports/recipients", h.TelegramReportRecipients)
	mux.HandleFunc("/api/telegram-reports/test", h.TelegramReportTest)
	mux.HandleFunc("/api/telegram-reports/history", h.TelegramReportHistory)

	// Invoice API
	mux.HandleFunc("/api/invoices", h.InvoicesRoot)            // GET, POST
	mux.HandleFunc("/api/invoices/report", h.GetInvoiceReport) // GET
	mux.HandleFunc("/api/invoices/payment", h.AddPayment)      // POST
	mux.HandleFunc("/api/invoices/", h.GetInvoices)            // GET all or GET by ID

	// Operational bridge API: reads source data from the old operational SQLite database.
	mux.HandleFunc("/api/operational/customers", h.GetOperationalCustomers)
	mux.HandleFunc("/api/operational/kala-items", h.GetOperationalKalaItems)
	mux.HandleFunc("/api/operational/yarn-items", h.GetOperationalYarnItems)
	mux.HandleFunc("/api/operational/out-invoices", h.GetOperationalOutInvoices)
	mux.HandleFunc("/api/operational/yarn-incoming", h.GetOperationalYarnIncoming)
	mux.HandleFunc("/api/operational/chelle-incoming", h.GetOperationalChelleIncoming)
	mux.HandleFunc("/api/operational/yarn-outgoing", h.GetOperationalYarnOutgoing)
	mux.HandleFunc("/api/operational/expenses", h.GetOperationalExpenses)
	mux.HandleFunc("/api/operational/misc-incoming", h.GetOperationalMiscIncoming)
	mux.HandleFunc("/api/operational/spare-parts-inventory", h.GetOperationalSparePartsInventory)

	// Financial-compatible aliases used by the previous financial UI.
	mux.HandleFunc("/api/financial/lookups/customers", h.GetOperationalCustomers)
	mux.HandleFunc("/api/financial/lookups/kala-items", h.GetOperationalKalaItems)
	mux.HandleFunc("/api/financial/lookups/yarn-items", h.GetOperationalYarnItems)
	mux.HandleFunc("/api/financial/lookups/operational/f_khor", h.GetOperationalOutInvoices)
	mux.HandleFunc("/api/financial/operational/yarn-incoming", h.GetOperationalYarnIncoming)
	mux.HandleFunc("/api/financial/operational/chelle-incoming", h.GetOperationalChelleIncoming)
	mux.HandleFunc("/api/financial/operational/yarn-outgoing", h.GetOperationalYarnOutgoing)
	mux.HandleFunc("/api/financial/operational/expenses", h.GetOperationalExpenses)
	mux.HandleFunc("/api/financial/operational/misc-incoming", h.GetOperationalMiscIncoming)
	mux.HandleFunc("/api/financial/operational/spare-parts-inventory", h.GetOperationalSparePartsInventory)
	mux.HandleFunc("/", h.Root)

	var handler http.Handler = mux
	handler = middleware.AuditLog(handler)
	handler = middleware.Metrics(handler)
	handler = middleware.AdminAuth(handler)
	handler = middleware.RateLimit(handler)
	handler = middleware.Logger(handler)
	handler = middleware.CORS(handler)
	handler = middleware.Recovery(handler)

	return handler
}
