package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
	"github.com/erpsystem/textile-erp/internal/application/telegramreport"
	"github.com/erpsystem/textile-erp/internal/application/usecase"
	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/service"
	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
	"github.com/erpsystem/textile-erp/internal/infrastructure/cache"
	"github.com/erpsystem/textile-erp/internal/infrastructure/operationalbridge"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/password"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
	"github.com/erpsystem/textile-erp/internal/presentation/middleware"
)

type APIHandler struct {
	productionUC *usecase.ProductionUseCase
	settlementUC *usecase.SettlementUseCase
	advisor      *service.FinancialAdvisor
	costService  *service.CostService
	inventorySvc *service.InventoryService
	invoiceSvc   *service.InvoiceService
	financeCoreService *financecore.Service
	operational  *operationalbridge.Bridge
	telegram     *telegramreport.Service
	cache        *cache.Client
}

func NewAPIHandler(telegram *telegramreport.Service) *APIHandler {
	opBridge, err := operationalbridge.NewFromEnv()
	if err != nil {
		log.Printf("operational bridge disabled: %v", err)
	}
	return &APIHandler{
		productionUC: usecase.NewProductionUseCase(),
		settlementUC: usecase.NewSettlementUseCase(),
		advisor:      service.NewFinancialAdvisor(),
		costService:  service.NewCostService(),
		inventorySvc: service.NewInventoryService(),
		invoiceSvc:   service.NewInvoiceService(),
		operational:  opBridge,
		telegram:     telegram,
		cache:        cache.NewFromEnv(),
	}
}

func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func RespondError(w http.ResponseWriter, status int, message string) {
	RespondJSON(w, status, map[string]string{"error": message})
}

func (h *APIHandler) respondCachedJSON(w http.ResponseWriter, r *http.Request, key string, ttl time.Duration, build func() interface{}) {
	if h.cache != nil {
		if cached, ok, err := h.cache.Get(r.Context(), key); err == nil && ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cached)
			return
		}
	}
	data := build()
	payload, err := json.Marshal(data)
	if err == nil && h.cache != nil {
		_ = h.cache.SetJSON(r.Context(), key, payload, ttl)
	}
	w.Header().Set("X-Cache", "MISS")
	RespondJSON(w, http.StatusOK, data)
}

func (h *APIHandler) invalidateCache(r *http.Request, keys ...string) {
	if h.cache == nil {
		return
	}
	_ = h.cache.Delete(r.Context(), keys...)
}

func (h *APIHandler) requireOperational(w http.ResponseWriter, r *http.Request) (*operationalbridge.Bridge, func()) {
	if h.operational == nil {
		RespondError(w, http.StatusServiceUnavailable, "Operational database is not available")
		return nil, func() {}
	}
	bridge, cleanup, err := h.operational.ForCompany(r.Context(), requestctx.CompanyID(r.Context()))
	if err != nil {
		RespondError(w, http.StatusServiceUnavailable, "Operational company database is not available: "+err.Error())
		return nil, func() {}
	}
	return bridge, cleanup
}

func (h *APIHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	telegramAvailable := h.telegram != nil && h.telegram.Available()
	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":          "ok",
		"service":         "textile-erp",
		"version":         "2.1.0",
		"telegramReports": map[string]bool{"available": telegramAvailable},
		"features": []string{
			"financial-advisor", "waste-calculator", "commission-calculator",
			"credit-scoring", "settlement-validator", "production-usecase",
			"settlement-usecase", "cost-management", "inventory-management",
			"invoice-management",
		},
	})
}

func (h *APIHandler) Root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		RespondError(w, http.StatusNotFound, "Not found")
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"service": "textile-erp-financial-api",
		"status":  "ok",
		"links": map[string]string{
			"health":  "/health",
			"metrics": "/metrics",
			"login":   "/api/auth/login",
		},
	})
}

func (h *APIHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || strings.TrimSpace(req.Password) == "" {
		RespondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	var user struct {
		ID           int64
		Username     string
		PasswordHash string
		Role         string
		CompanyID    int64
		IsActive     bool
	}
	db := postgres.DB
	if db == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	err := db.QueryRowContext(r.Context(), `
		SELECT id, username, password_hash, role, COALESCE(company_id, 1), COALESCE(is_active, true)
		FROM financial_users
		WHERE username = $1
	`, username).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.CompanyID, &user.IsActive)
	if err != nil || !user.IsActive || !password.Verify(req.Password, user.PasswordHash) {
		RespondError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	token, err := middleware.SignJWT(map[string]interface{}{
		"user_id":    user.ID,
		"company_id": user.CompanyID,
		"role":       user.Role,
		"username":   user.Username,
	})
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Could not create token")
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":         user.ID,
			"username":   user.Username,
			"role":       user.Role,
			"company_id": user.CompanyID,
		},
	})
}

// Production Handlers
func (h *APIHandler) CreateProductionOrder(w http.ResponseWriter, r *http.Request) {
	var req usecase.CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	resp, err := h.productionUC.CreateProductionOrder(req)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusCreated, resp)
}

func (h *APIHandler) CompleteProduction(w http.ResponseWriter, r *http.Request) {
	var req usecase.CompleteProductionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	resp, err := h.productionUC.CompleteProduction(req)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, resp)
}

// Settlement Handler
func (h *APIHandler) CreateSettlement(w http.ResponseWriter, r *http.Request) {
	var req usecase.SettlementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	resp, err := h.settlementUC.CreateSettlement(req)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusCreated, resp)
}

// Advisor Handlers
func (h *APIHandler) GetAdvice(w http.ResponseWriter, r *http.Request) {
	var req service.AdviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	profile := &entity.CustomerCreditProfile{PartyID: req.PartyID, CreditLimit: valueobject.NewMoney(300000000), CreditDays: 30, RiskGroup: "Medium", BaseScore: 65}
	advices := h.advisor.GetAdvice(req, profile)
	summary, riskLevel := "Advice generated", "low"
	for _, a := range advices {
		if a.Severity == service.SeverityBlock || a.Severity == service.SeverityCritical {
			riskLevel = "high"
			summary = "Critical warnings"
			break
		}
		if a.Severity == service.SeverityWarning {
			riskLevel = "medium"
			summary = "Recommendations available"
		}
	}
	RespondJSON(w, http.StatusOK, service.AdvisorResponse{PartyID: req.PartyID, Advices: advices, Summary: summary, RiskLevel: riskLevel})
}

func (h *APIHandler) GetCreditReport(w http.ResponseWriter, r *http.Request) {
	profile := &entity.CustomerCreditProfile{PartyID: 1, CreditLimit: valueobject.NewMoney(300000000), CreditDays: 30, RiskGroup: "Medium", BaseScore: 65}
	report := h.advisor.GenerateCreditReport(profile)
	RespondJSON(w, http.StatusOK, report)
}

// Commission Handler
type commissionRequest struct {
	YarnInput, FabricOutput, AgreedYarnPrice, AgreedFabricPrice, StdWasteRate, DowntimeDays, DowntimeRate float64
}

func (h *APIHandler) CalculateCommission(w http.ResponseWriter, r *http.Request) {
	var req commissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	resp, err := h.productionUC.CompleteProduction(usecase.CompleteProductionRequest{YarnInput: req.YarnInput, FabricOutput: req.FabricOutput, DowntimeDays: req.DowntimeDays, DowntimeRate: req.DowntimeRate, YarnPrice: req.AgreedYarnPrice, FabricPrice: req.AgreedFabricPrice, StdWasteRate: req.StdWasteRate})
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, resp)
}

// Cost Handlers
type costRequest struct {
	Category, Description string
	Amount                float64
	PartyID               *int64
	InvoiceNo             string
}

func (h *APIHandler) AddCost(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	var req costRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	cost := entity.NewCost(1, entity.CostCategory(req.Category), req.Description, valueobject.NewMoney(req.Amount))
	if req.PartyID != nil {
		cost.PartyID = req.PartyID
	}
	cost.InvoiceNo = req.InvoiceNo
	saved := h.costService.AddCostForCompany(companyID, *cost)
	h.invalidateCache(r,
		cache.TenantKey(companyID, "costs", "all", r.URL.RawQuery),
		cache.TenantKey(companyID, "costs", "summary"),
		cache.TenantKey(companyID, "costs", "profitability"),
	)
	RespondJSON(w, http.StatusCreated, saved)
}
func (h *APIHandler) GetCosts(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "costs", "all", r.URL.RawQuery)
	h.respondCachedJSON(w, r, key, 2*time.Minute, func() interface{} {
		category := r.URL.Query().Get("category")
		var costs []entity.Cost
		if category != "" {
			costs = h.costService.GetCostsByCategoryForCompany(companyID, entity.CostCategory(category))
		} else {
			costs = h.costService.GetCostsForCompany(companyID)
		}
		return map[string]interface{}{"costs": costs, "total": len(costs)}
	})
}
func (h *APIHandler) GetCostSummary(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "costs", "summary", r.URL.RawQuery)
	h.respondCachedJSON(w, r, key, 2*time.Minute, func() interface{} {
		days := 30
		if d := r.URL.Query().Get("days"); d != "" {
			fmt.Sscanf(d, "%d", &days)
		}
		return h.costService.GetSummaryForCompany(companyID, days)
	})
}
func (h *APIHandler) GetProfitability(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "costs", "profitability", r.URL.RawQuery)
	h.respondCachedJSON(w, r, key, 2*time.Minute, func() interface{} {
		revenue := 500000000.0
		if r := r.URL.Query().Get("revenue"); r != "" {
			fmt.Sscanf(r, "%f", &revenue)
		}
		days := 30
		if d := r.URL.Query().Get("days"); d != "" {
			fmt.Sscanf(d, "%d", &days)
		}
		return h.costService.GetProfitabilityForCompany(companyID, valueobject.NewMoney(revenue), days)
	})
}

// Inventory Handlers
func (h *APIHandler) GetInventory(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "inventory", "items")
	h.respondCachedJSON(w, r, key, time.Minute, func() interface{} {
		items := h.inventorySvc.GetItemsForCompany(companyID)
		return map[string]interface{}{"items": items, "total": len(items)}
	})
}
func (h *APIHandler) StockIn(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	var req struct {
		ItemID                                         int64
		Qty                                            float64
		UnitCost                                       float64
		ReferenceNo, Description, Warehouse, CreatedBy string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	txn, err := h.inventorySvc.StockInForCompany(companyID, req.ItemID, req.Qty, valueobject.NewMoney(req.UnitCost), req.ReferenceNo, req.Description, req.Warehouse, req.CreatedBy)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.invalidateInventoryCache(r, companyID)
	RespondJSON(w, http.StatusCreated, txn)
}
func (h *APIHandler) StockOut(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	var req struct {
		ItemID                                         int64
		Qty                                            float64
		ReferenceNo, Description, Warehouse, CreatedBy string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	txn, err := h.inventorySvc.StockOutForCompany(companyID, req.ItemID, req.Qty, req.ReferenceNo, req.Description, req.Warehouse, req.CreatedBy)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.invalidateInventoryCache(r, companyID)
	RespondJSON(w, http.StatusCreated, txn)
}
func (h *APIHandler) GetInventorySummary(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "inventory", "summary")
	h.respondCachedJSON(w, r, key, time.Minute, func() interface{} {
		return h.inventorySvc.GetStockSummaryForCompany(companyID)
	})
}
func (h *APIHandler) GetStockAlerts(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "inventory", "alerts")
	h.respondCachedJSON(w, r, key, time.Minute, func() interface{} {
		alerts := h.inventorySvc.GetStockAlertsForCompany(companyID)
		return map[string]interface{}{"alerts": alerts, "total": len(alerts)}
	})
}
func (h *APIHandler) GetInventoryTransactions(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "inventory", "transactions", r.URL.RawQuery)
	h.respondCachedJSON(w, r, key, time.Minute, func() interface{} {
		itemIDStr := r.URL.Query().Get("item_id")
		var txns []service.InventoryTransaction
		if itemIDStr != "" {
			itemID, _ := strconv.ParseInt(itemIDStr, 10, 64)
			txns = h.inventorySvc.GetTransactionsByItemForCompany(companyID, itemID)
		} else {
			txns = h.inventorySvc.GetTransactionsForCompany(companyID)
		}
		return map[string]interface{}{"transactions": txns, "total": len(txns)}
	})
}

func (h *APIHandler) invalidateInventoryCache(r *http.Request, companyID int64) {
	h.invalidateCache(r,
		cache.TenantKey(companyID, "inventory", "items"),
		cache.TenantKey(companyID, "inventory", "summary"),
		cache.TenantKey(companyID, "inventory", "alerts"),
		cache.TenantKey(companyID, "inventory", "transactions", r.URL.RawQuery),
	)
}

// ============================================
// INVOICE HANDLERS
// ============================================

// GetInvoices handles GET /api/invoices
func (h *APIHandler) InvoicesRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.GetInvoices(w, r)
	case http.MethodPost:
		h.CreateInvoice(w, r)
	default:
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *APIHandler) GetInvoices(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "invoices", "all")
	h.respondCachedJSON(w, r, key, time.Minute, func() interface{} {
		invoices := h.invoiceSvc.GetInvoicesForCompany(companyID)
		return map[string]interface{}{"invoices": invoices, "total": len(invoices)}
	})
}

// GetInvoice handles GET /api/invoices/{id}
func (h *APIHandler) GetInvoice(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	parts := strings.Split(r.URL.Path, "/")
	idStr := parts[len(parts)-1]
	id, _ := strconv.ParseInt(idStr, 10, 64)

	inv := h.invoiceSvc.GetInvoiceForCompany(companyID, id)
	if inv == nil {
		RespondError(w, http.StatusNotFound, "Invoice not found")
		return
	}
	RespondJSON(w, http.StatusOK, inv)
}

// CreateInvoice handles POST /api/invoices
func (h *APIHandler) CreateInvoice(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	var req struct {
		CustomerID   int64                        `json:"customer_id"`
		CustomerName string                       `json:"customer_name"`
		Lines        []service.InvoiceLineRequest `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	inv := h.invoiceSvc.CreateInvoiceForCompany(companyID, req.CustomerID, req.CustomerName, req.Lines)
	h.invalidateInvoiceCache(r, companyID)
	RespondJSON(w, http.StatusCreated, inv)
}

// AddPayment handles POST /api/invoices/payment
func (h *APIHandler) AddPayment(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	var req struct {
		InvoiceID int64   `json:"invoice_id"`
		Amount    float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	inv, err := h.invoiceSvc.AddPaymentToInvoiceForCompany(companyID, req.InvoiceID, req.Amount)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.invalidateInvoiceCache(r, companyID)
	RespondJSON(w, http.StatusOK, inv)
}

// GetInvoiceReport handles GET /api/invoices/report
func (h *APIHandler) GetInvoiceReport(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	key := cache.TenantKey(companyID, "invoices", "report")
	h.respondCachedJSON(w, r, key, time.Minute, func() interface{} {
		return h.invoiceSvc.GetInvoiceReportForCompany(companyID)
	})
}

func (h *APIHandler) invalidateInvoiceCache(r *http.Request, companyID int64) {
	h.invalidateCache(r,
		cache.TenantKey(companyID, "invoices", "all"),
		cache.TenantKey(companyID, "invoices", "report"),
	)
}

// ============================================
// OPERATIONAL BRIDGE HANDLERS
// ============================================

func (h *APIHandler) GetOperationalCustomers(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	h.handleOperationalLookup(w, r, "customers", bridge.Customers, bridge.CreateCustomer)
}

func (h *APIHandler) GetOperationalKalaItems(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	h.handleOperationalLookup(w, r, "kala-items", bridge.KalaItems, bridge.CreateKalaItem)
}

func (h *APIHandler) GetOperationalYarnItems(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	h.handleOperationalLookup(w, r, "yarn-items", bridge.YarnItems, bridge.CreateYarnItem)
}

func (h *APIHandler) handleOperationalLookup(
	w http.ResponseWriter,
	r *http.Request,
	name string,
	list func() ([]operationalbridge.LookupRow, error),
	create func(string) (operationalbridge.LookupRow, error),
) {
	switch r.Method {
	case http.MethodGet:
		rows, err := list()
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": rows, "total": len(rows)})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		itemName := strings.TrimSpace(req.Name)
		if itemName == "" {
			RespondError(w, http.StatusBadRequest, "Name is required")
			return
		}
		row, err := create(itemName)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "item": row, "lookup": name})
	default:
		w.Header().Set("Allow", "GET, POST")
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *APIHandler) GetOperationalOutInvoices(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	rows, err := bridge.OutInvoices(parseLimit(r, 300))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": rows, "total": len(rows)})
}

func (h *APIHandler) GetOperationalYarnIncoming(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	rows, err := bridge.YarnIncoming(parseLimit(r, 300))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": rows, "total": len(rows)})
}

func (h *APIHandler) GetOperationalChelleIncoming(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	rows, err := bridge.ChelleIncoming(parseLimit(r, 300))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": rows, "total": len(rows)})
}

func (h *APIHandler) GetOperationalYarnOutgoing(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	rows, err := bridge.YarnOutgoing(parseLimit(r, 300))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": rows, "total": len(rows)})
}

func (h *APIHandler) GetOperationalExpenses(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	rows, err := bridge.Expenses(parseLimit(r, 300))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": rows, "total": len(rows)})
}

func (h *APIHandler) GetOperationalMiscIncoming(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	rows, err := bridge.MiscIncoming(parseLimit(r, 300))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": rows, "total": len(rows)})
}

func (h *APIHandler) GetOperationalSparePartsInventory(w http.ResponseWriter, r *http.Request) {
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	rows, err := bridge.SparePartsInventory(parseLimit(r, 300))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": rows, "total": len(rows)})
}

func (h *APIHandler) ReportOperationalMismatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	var req struct {
		SourceType  string `json:"source_type"`
		SourceID    string `json:"source_id"`
		InvoiceNo   string `json:"invoice_no"`
		InvoiceKind string `json:"invoice_kind"`
		Title       string `json:"title"`
		Message     string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid mismatch report payload")
		return
	}
	req.SourceType = strings.TrimSpace(req.SourceType)
	req.SourceID = strings.TrimSpace(req.SourceID)
	req.InvoiceNo = strings.TrimSpace(req.InvoiceNo)
	req.InvoiceKind = strings.TrimSpace(req.InvoiceKind)
	req.Title = strings.TrimSpace(req.Title)
	req.Message = strings.TrimSpace(req.Message)
	if req.SourceType == "" || !strings.HasPrefix(req.SourceType, "operational") || req.SourceID == "" || req.Message == "" {
		RespondError(w, http.StatusBadRequest, "Operational source and mismatch message are required")
		return
	}
	if req.Title == "" {
		req.Title = "گزارش مغایرت مالی"
	}
	if req.InvoiceKind == "" {
		req.InvoiceKind = "فاکتور عملیاتی"
	}
	if len([]rune(req.Message)) > 1200 {
		runes := []rune(req.Message)
		req.Message = string(runes[:1200])
	}
	reportedBy := fmt.Sprintf("user:%d", requestctx.UserID(r.Context()))
	if err := bridge.ReportFinancialMismatch(operationalbridge.FinancialMismatchReport{
		SourceType:  req.SourceType,
		SourceID:    req.SourceID,
		InvoiceNo:   req.InvoiceNo,
		InvoiceKind: req.InvoiceKind,
		Title:       req.Title,
		Message:     req.Message,
		ReportedBy:  reportedBy,
	}); err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"success": true})
}

func parseLimit(r *http.Request, fallback int) int {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		return fallback
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
