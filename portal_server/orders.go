package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed assets/plans-og.png
var plansOGImage []byte

const (
	purchaseOrderPending  = "pending"
	purchaseOrderApproved = "approved"
	purchaseOrderRejected = "rejected"
)

type purchaseOrder struct {
	ID                 string    `json:"id"`
	CompanyName        string    `json:"company_name"`
	ContactName        string    `json:"contact_name"`
	Mobile             string    `json:"mobile"`
	Email              string    `json:"email,omitempty"`
	AllowFinancial     bool      `json:"allow_financial"`
	AllowOperational   bool      `json:"allow_operational"`
	AllowWeaving       bool      `json:"allow_weaving"`
	EmployeeCount      int       `json:"employee_count"`
	MachineCount       int       `json:"machine_count"`
	UnitCount          int       `json:"unit_count"`
	BillingCycle       string    `json:"billing_cycle"`
	Notes              string    `json:"notes,omitempty"`
	Status             string    `json:"status"`
	AccessID           int64     `json:"access_id,omitempty"`
	RequesterAccessID  int64     `json:"requester_access_id,omitempty"`
	FinancialCompanyID int64     `json:"financial_company_id,omitempty"`
	RequestedBy        string    `json:"requested_by,omitempty"`
	AdminNote          string    `json:"admin_note,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type purchaseOrderRequest struct {
	CompanyName        string `json:"companyName"`
	ContactName        string `json:"contactName"`
	Mobile             string `json:"mobile"`
	Email              string `json:"email"`
	AllowFinancial     bool   `json:"allowFinancial"`
	AllowOperational   bool   `json:"allowOperational"`
	AllowWeaving       bool   `json:"allowWeaving"`
	EmployeeCount      int    `json:"employeeCount"`
	MachineCount       int    `json:"machineCount"`
	UnitCount          int    `json:"unitCount"`
	BillingCycle       string `json:"billingCycle"`
	Notes              string `json:"notes"`
	Website            string `json:"website"`
	RequesterAccessID  int64  `json:"-"`
	FinancialCompanyID int64  `json:"-"`
	RequestedBy        string `json:"-"`
}

func ensurePurchaseOrderStore(path string) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte("[]\n"), 0o600)
}

func readPurchaseOrders(path string) ([]purchaseOrder, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return []purchaseOrder{}, nil
	}
	var items []purchaseOrder
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func writePurchaseOrders(path string, items []purchaseOrder) error {
	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func (a *portalApp) listPurchaseOrders() ([]purchaseOrder, error) {
	a.ordersMu.Lock()
	defer a.ordersMu.Unlock()
	items, err := readPurchaseOrders(a.ordersFile)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (a *portalApp) purchaseOrderStoreStatus() string {
	if strings.TrimSpace(a.ordersFile) == "" {
		return "unconfigured"
	}
	if _, err := a.listPurchaseOrders(); err != nil {
		return "error"
	}
	return "ok"
}

func (a *portalApp) createPurchaseOrder(req purchaseOrderRequest) (purchaseOrder, error) {
	req.CompanyName = strings.TrimSpace(req.CompanyName)
	req.ContactName = strings.TrimSpace(req.ContactName)
	req.Mobile = strings.TrimSpace(req.Mobile)
	req.Email = strings.TrimSpace(req.Email)
	req.Notes = strings.TrimSpace(req.Notes)
	req.BillingCycle = strings.ToLower(strings.TrimSpace(req.BillingCycle))
	if strings.TrimSpace(req.Website) != "" {
		return purchaseOrder{}, fmt.Errorf("درخواست نامعتبر است")
	}
	if len([]rune(req.CompanyName)) < 2 || len([]rune(req.CompanyName)) > 120 {
		return purchaseOrder{}, fmt.Errorf("نام شرکت را کامل وارد کنید")
	}
	if len([]rune(req.ContactName)) < 2 || len([]rune(req.ContactName)) > 100 {
		return purchaseOrder{}, fmt.Errorf("نام مدیر یا مسئول خرید را کامل وارد کنید")
	}
	if !validPurchaseMobile(req.Mobile) {
		return purchaseOrder{}, fmt.Errorf("شماره همراه معتبر وارد کنید")
	}
	if req.Email != "" {
		address, err := mail.ParseAddress(req.Email)
		if err != nil || !strings.EqualFold(strings.TrimSpace(address.Address), req.Email) {
			return purchaseOrder{}, fmt.Errorf("ایمیل معتبر وارد کنید")
		}
	}
	if !req.AllowFinancial && !req.AllowOperational && !req.AllowWeaving {
		return purchaseOrder{}, fmt.Errorf("حداقل یکی از سه محصول را انتخاب کنید")
	}
	if req.EmployeeCount < 1 || req.EmployeeCount > 5000 {
		return purchaseOrder{}, fmt.Errorf("تعداد کارکنان باید بین ۱ تا ۵۰۰۰ باشد")
	}
	if req.UnitCount < 1 || req.UnitCount > 1000 {
		return purchaseOrder{}, fmt.Errorf("تعداد واحدهای تولیدی باید بین ۱ تا ۱۰۰۰ باشد")
	}
	if req.MachineCount < 0 || req.MachineCount > 20000 || (req.AllowWeaving && req.MachineCount < 1) {
		return purchaseOrder{}, fmt.Errorf("برای راندمان سالن، تعداد ماشین‌های بافندگی را وارد کنید")
	}
	if req.BillingCycle != "monthly" && req.BillingCycle != "annual" {
		req.BillingCycle = "annual"
	}
	if len([]rune(req.Notes)) > 1000 {
		return purchaseOrder{}, fmt.Errorf("توضیحات سفارش بیش از حد طولانی است")
	}
	randomID, err := randomHex(6)
	if err != nil {
		return purchaseOrder{}, err
	}
	now := time.Now().UTC()
	order := purchaseOrder{
		ID:                 "TX-" + strings.ToUpper(randomID),
		CompanyName:        req.CompanyName,
		ContactName:        req.ContactName,
		Mobile:             req.Mobile,
		Email:              req.Email,
		AllowFinancial:     req.AllowFinancial,
		AllowOperational:   req.AllowOperational,
		AllowWeaving:       req.AllowWeaving,
		EmployeeCount:      req.EmployeeCount,
		MachineCount:       req.MachineCount,
		UnitCount:          req.UnitCount,
		BillingCycle:       req.BillingCycle,
		Notes:              req.Notes,
		Status:             purchaseOrderPending,
		RequesterAccessID:  req.RequesterAccessID,
		FinancialCompanyID: req.FinancialCompanyID,
		RequestedBy:        req.RequestedBy,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	a.ordersMu.Lock()
	defer a.ordersMu.Unlock()
	items, err := readPurchaseOrders(a.ordersFile)
	if err != nil {
		return purchaseOrder{}, err
	}
	for _, item := range items {
		if item.Status != purchaseOrderPending || now.Sub(item.CreatedAt) > 10*time.Minute {
			continue
		}
		if strings.EqualFold(item.CompanyName, order.CompanyName) && item.Mobile == order.Mobile &&
			item.AllowFinancial == order.AllowFinancial && item.AllowOperational == order.AllowOperational && item.AllowWeaving == order.AllowWeaving &&
			item.EmployeeCount == order.EmployeeCount && item.MachineCount == order.MachineCount && item.UnitCount == order.UnitCount {
			return item, nil
		}
	}
	items = append(items, order)
	if err := writePurchaseOrders(a.ordersFile, items); err != nil {
		return purchaseOrder{}, err
	}
	return order, nil
}

func validPurchaseMobile(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 7 || len(value) > 20 {
		return false
	}
	digits := 0
	for index, char := range value {
		if char >= '0' && char <= '9' {
			digits++
			continue
		}
		if char == '+' && index == 0 {
			continue
		}
		if char != ' ' && char != '-' && char != '(' && char != ')' {
			return false
		}
	}
	return digits >= 7
}

func purchaseOrderModules(order purchaseOrder) string {
	parts := make([]string, 0, 3)
	if order.AllowFinancial {
		parts = append(parts, "مالی")
	}
	if order.AllowOperational {
		parts = append(parts, "عملیاتی")
	}
	if order.AllowWeaving {
		parts = append(parts, "راندمان سالن بافت")
	}
	return strings.Join(parts, " + ")
}

func purchaseOrderMarker(id string) string {
	return "[purchase_order:" + strings.TrimSpace(id) + "]"
}

func (a *portalApp) findPurchaseOrderAccess(order purchaseOrder) (projectAccess, error) {
	items, err := a.listAccesses()
	if err != nil {
		return projectAccess{}, err
	}
	marker := purchaseOrderMarker(order.ID)
	for _, item := range items {
		if (order.AccessID > 0 && item.ID == order.AccessID) || strings.Contains(item.Notes, marker) {
			return item, nil
		}
	}
	return projectAccess{}, os.ErrNotExist
}

func (a *portalApp) savePurchaseOrder(order purchaseOrder) error {
	a.ordersMu.Lock()
	defer a.ordersMu.Unlock()
	items, err := readPurchaseOrders(a.ordersFile)
	if err != nil {
		return err
	}
	for index := range items {
		if items[index].ID != order.ID {
			continue
		}
		order.UpdatedAt = time.Now().UTC()
		items[index] = order
		return writePurchaseOrders(a.ordersFile, items)
	}
	return os.ErrNotExist
}

func (a *portalApp) purchaseOrderByID(id string) (purchaseOrder, error) {
	items, err := a.listPurchaseOrders()
	if err != nil {
		return purchaseOrder{}, err
	}
	for _, item := range items {
		if strings.EqualFold(item.ID, strings.TrimSpace(id)) {
			return item, nil
		}
	}
	return purchaseOrder{}, os.ErrNotExist
}

func (a *portalApp) approvePurchaseOrder(id, adminNote string) (purchaseOrder, projectAccess, string, error) {
	order, err := a.purchaseOrderByID(id)
	if err != nil {
		return purchaseOrder{}, projectAccess{}, "", err
	}
	if order.Status == purchaseOrderRejected {
		return purchaseOrder{}, projectAccess{}, "", fmt.Errorf("سفارش ردشده قابل فعال‌سازی نیست")
	}
	if existing, existingErr := a.findPurchaseOrderAccess(order); existingErr == nil {
		order.Status = purchaseOrderApproved
		order.AccessID = existing.ID
		order.AdminNote = strings.TrimSpace(adminNote)
		if err := a.savePurchaseOrder(order); err != nil {
			return purchaseOrder{}, projectAccess{}, "", err
		}
		return order, existing, a.portalAccessPassword(existing), nil
	}
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if order.BillingCycle == "annual" {
		expiresAt = time.Now().Add(365 * 24 * time.Hour)
	}
	notes := strings.Join([]string{
		purchaseOrderMarker(order.ID),
		"سفارش عمومی Textile ERP",
		"محصولات: " + purchaseOrderModules(order),
		fmt.Sprintf("کارکنان: %d | ماشین‌ها: %d | واحدها: %d", order.EmployeeCount, order.MachineCount, order.UnitCount),
		strings.TrimSpace(order.Notes),
	}, "\n")
	if order.RequesterAccessID > 0 {
		items, listErr := a.listAccesses()
		if listErr != nil {
			return purchaseOrder{}, projectAccess{}, "", listErr
		}
		for _, existing := range items {
			if existing.ID != order.RequesterAccessID || existing.ProjectKey != "textile-erp" {
				continue
			}
			if existing.ExpiresAt.After(expiresAt) {
				expiresAt = existing.ExpiresAt
			}
			updatedNotes := strings.TrimSpace(existing.Notes + "\n" + notes)
			updated, rawPassword, updateErr := a.updateManagedAccess(
				existing.ID,
				existing.ProjectKey,
				existing.CompanyName,
				existing.ContactName,
				existing.Username,
				"",
				existing.FinancialCompanyID,
				expiresAt,
				updatedNotes,
				effectiveAccessRole(existing),
				effectivePermissions(existing),
				effectiveCanManageTeam(existing),
				false,
				effectiveAllowFinancial(existing) || order.AllowFinancial,
				effectiveAllowOperational(existing) || order.AllowOperational,
				effectiveAllowWeaving(existing) || order.AllowWeaving,
			)
			if updateErr != nil {
				return purchaseOrder{}, projectAccess{}, "", updateErr
			}
			order.Status = purchaseOrderApproved
			order.AccessID = updated.ID
			order.AdminNote = strings.TrimSpace(adminNote)
			if err := a.savePurchaseOrder(order); err != nil {
				return purchaseOrder{}, projectAccess{}, "", err
			}
			return order, updated, rawPassword, nil
		}
		return purchaseOrder{}, projectAccess{}, "", fmt.Errorf("حساب مدیر درخواست‌کننده پیدا نشد")
	}
	access, rawPassword, err := a.createManagedAccess(
		"textile-erp",
		order.CompanyName,
		order.ContactName,
		"",
		"",
		0,
		expiresAt,
		notes,
		"owner",
		financialPermissionCatalog,
		true,
		false,
		order.AllowFinancial,
		order.AllowOperational,
		order.AllowWeaving,
	)
	if err != nil {
		return purchaseOrder{}, projectAccess{}, "", err
	}
	order.Status = purchaseOrderApproved
	order.AccessID = access.ID
	order.AdminNote = strings.TrimSpace(adminNote)
	if err := a.savePurchaseOrder(order); err != nil {
		return purchaseOrder{}, projectAccess{}, "", err
	}
	return order, access, rawPassword, nil
}

func (a *portalApp) rejectPurchaseOrder(id, adminNote string) (purchaseOrder, error) {
	order, err := a.purchaseOrderByID(id)
	if err != nil {
		return purchaseOrder{}, err
	}
	if order.Status == purchaseOrderApproved || order.AccessID > 0 {
		return purchaseOrder{}, fmt.Errorf("حساب این سفارش ساخته شده و باید از بخش مشتریان مدیریت شود")
	}
	order.Status = purchaseOrderRejected
	order.AdminNote = strings.TrimSpace(adminNote)
	if err := a.savePurchaseOrder(order); err != nil {
		return purchaseOrder{}, err
	}
	return order, nil
}

func (a *portalApp) publicPurchaseOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req purchaseOrderRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "اطلاعات سفارش معتبر نیست"})
		return
	}
	if requester, err := a.accessRecordFromRequest(r); err == nil && requester.ProjectKey == "textile-erp" {
		role := effectiveAccessRole(requester)
		if role == "owner" || role == "manager" {
			owner := requester
			if role != "owner" {
				if tenantItems, tenantErr := a.tenantAccesses(requester); tenantErr == nil {
					for _, item := range tenantItems {
						if effectiveAccessRole(item) == "owner" {
							owner = item
							break
						}
					}
				}
			}
			req.RequesterAccessID = owner.ID
			req.FinancialCompanyID = owner.FinancialCompanyID
			req.RequestedBy = requester.Username
			req.CompanyName = owner.CompanyName
			if req.ContactName == "" {
				req.ContactName = requester.ContactName
			}
		}
	}
	order, err := a.createPurchaseOrder(req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"id":      order.ID,
		"status":  order.Status,
		"modules": purchaseOrderModules(order),
		"message": "سفارش ثبت شد. پس از بررسی و تأیید، اطلاعات ورود برای مدیر شرکت صادر می‌شود.",
	})
}

func (a *portalApp) adminOrdersAPI(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminAuthenticated(r) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	items, err := a.listPurchaseOrders()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *portalApp) adminOrderByID(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminAuthenticated(r) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/api/orders/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		http.NotFound(w, r)
		return
	}
	var body struct {
		AdminNote string `json:"adminNote"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body)
	switch parts[1] {
	case "approve":
		order, access, rawPassword, err := a.approvePurchaseOrder(parts[0], body.AdminNote)
		if err != nil {
			respondJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"order": order, "access": a.accessResponse(access, rawPassword)})
	case "reject":
		order, err := a.rejectPurchaseOrder(parts[0], body.AdminNote)
		if err != nil {
			respondJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"order": order})
	default:
		http.NotFound(w, r)
	}
}

func (a *portalApp) purchasePlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	page := strings.ReplaceAll(purchasePlansHTML, "__OG_IMAGE__", html.EscapeString(strings.TrimRight(a.publicBase, "/")+"/assets/plans-og.png"))
	_, _ = io.WriteString(w, page)
}

func (a *portalApp) plansSocialImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodGet {
		_, _ = w.Write(plansOGImage)
	}
}

func (a *portalApp) adminOrdersPage(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminAuthenticated(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, adminOrdersHTML)
}

var purchasePlansHTML = `<!doctype html>
<html lang="fa" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>خرید محصولات Textile ERP</title><meta name="description" content="انتخاب و سفارش بخش مالی، عملیاتی و راندمان سالن بافت Textile ERP"><meta property="og:title" content="محصولات Textile ERP"><meta property="og:description" content="مالی، عملیاتی و راندمان سالن بافت؛ جداگانه یا با هم"><meta property="og:type" content="website"><meta property="og:image" content="__OG_IMAGE__"><meta name="twitter:card" content="summary_large_image"><meta name="twitter:image" content="__OG_IMAGE__">
<style>
*{box-sizing:border-box}body{margin:0;background:#071619;color:#eef8f5;font-family:Tahoma,Arial;min-height:100vh}.nav{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:18px max(22px,calc((100vw - 1180px)/2));border-bottom:1px solid #1d4547;background:#0a2023}.brand{font-size:20px;font-weight:bold}.nav a{color:#bdebe1;text-decoration:none;border:1px solid #2e6161;border-radius:12px;padding:9px 13px}.wrap{max-width:1180px;margin:auto;padding:38px 22px 70px}.hero{display:grid;grid-template-columns:1.3fr .7fr;gap:28px;align-items:center;margin-bottom:32px}.hero h1{font-size:clamp(32px,5vw,58px);line-height:1.25;margin:0 0 16px}.hero p{color:#a9c9c3;line-height:2;font-size:16px}.free{border:1px solid #d1a84e;background:#302713;color:#ffe39b;border-radius:20px;padding:22px;line-height:2}.products{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin:26px 0}.product{position:relative;border:1px solid #285257;background:#0c2529;border-radius:20px;padding:22px;cursor:pointer;min-height:230px;transition:.2s}.product:has(input:checked){border-color:#50d1b3;box-shadow:0 0 0 3px #50d1b326;transform:translateY(-2px)}.product input{position:absolute;top:18px;left:18px;width:22px;height:22px;accent-color:#45c6a8}.product h2{margin:0 0 12px;font-size:22px}.product p{color:#a9c9c3;line-height:1.9;font-size:14px}.tag{display:inline-block;margin-top:12px;border-radius:999px;background:#153e3c;color:#8df0da;padding:6px 10px;font-size:12px}.order{display:grid;grid-template-columns:1fr 1fr;gap:18px;background:#f7f4ec;color:#1b312e;border-radius:24px;padding:26px}.order h2,.full{grid-column:1/-1}.field{display:grid;gap:7px}.field label{font-size:13px;color:#526f69}.field input,.field select,.field textarea{width:100%;border:1px solid #b8c9c4;border-radius:12px;background:white;color:#122623;padding:13px 14px;font:inherit}.field textarea{min-height:95px;resize:vertical}.summary{grid-column:1/-1;border:1px solid #c8ded8;background:#eaf5f2;border-radius:14px;padding:14px;line-height:2}.submit{grid-column:1/-1;border:0;border-radius:14px;padding:15px;background:#0f7a67;color:white;font:inherit;font-weight:bold;cursor:pointer}.submit:disabled{opacity:.6;cursor:wait}.message{display:none;grid-column:1/-1;border-radius:14px;padding:15px;line-height:2}.message.ok{display:block;background:#dff6ea;color:#075d3c}.message.err{display:block;background:#fee8e8;color:#9f1d2e}.hp{position:absolute!important;left:-10000px!important;opacity:0!important}.note{color:#789b94;line-height:2;text-align:center;margin-top:18px;font-size:13px}@media(max-width:850px){.hero,.products,.order{grid-template-columns:1fr}.full,.order h2,.summary,.submit,.message{grid-column:1}.wrap{padding:25px 14px 50px}.nav{padding:14px}.hero h1{font-size:34px}}
</style></head><body>
<nav class="nav"><div class="brand">Textile ERP · Viora</div><a href="/login">ورود مشتریان</a></nav>
<main class="wrap"><section class="hero"><div><h1>هر بخشی را که نیاز دارید انتخاب کنید</h1><p>سه محصول تخصصی نساجی را جداگانه یا با هم سفارش دهید. یک حساب مرکزی دریافت می‌کنید و فقط بخش‌های خریداری‌شده برای مدیر و کارکنان نمایش داده می‌شود.</p></div><aside class="free"><strong>مرکز فرمان مدیر نساجی رایگان است</strong><br>این داشبورد همراه هر سفارش فعال می‌شود و فقط اطلاعات محصولات خریداری‌شده را نمایش می‌دهد.</aside></section>
<form id="order" class="order"><h2>۱. انتخاب محصولات</h2><div class="products full">
<label class="product"><input name="financial" type="checkbox" checked><h2>بخش مالی</h2><p>حسابداری، فاکتورها، انبار، هزینه‌ها، اسناد، بانک و گزارش‌های مدیریتی.</p><span class="tag">قابل خرید مستقل</span></label>
<label class="product"><input name="operational" type="checkbox" checked><h2>بخش عملیاتی</h2><p>عملیات تولید، گردش مواد، بارگیری، کنترل فرایند و گزارش عملکرد واحدها.</p><span class="tag">قابل خرید مستقل</span></label>
<label class="product"><input name="weaving" type="checkbox" checked><h2>راندمان سالن بافت</h2><p>ثبت تصویر مانیتور ماشین، استخراج داده، راندمان، توقف‌ها، بافنده و مرکز تحلیل سالن.</p><span class="tag">قابل خرید مستقل</span></label>
</div><h2>۲. مشخصات سفارش</h2>
<div class="field"><label for="company">نام شرکت</label><input id="company" name="company" maxlength="120" required></div>
<div class="field"><label for="contact">نام مدیر یا مسئول خرید</label><input id="contact" name="contact" maxlength="100" required></div>
<div class="field"><label for="mobile">شماره همراه</label><input id="mobile" name="mobile" inputmode="tel" dir="ltr" required></div>
<div class="field"><label for="email">ایمیل (اختیاری)</label><input id="email" name="email" type="email" dir="ltr"></div>
<div class="field"><label for="employees">تعداد کارکنان استفاده‌کننده</label><input id="employees" name="employees" type="number" min="1" max="5000" value="3" required></div>
<div class="field"><label for="machines">تعداد ماشین‌های بافندگی</label><input id="machines" name="machines" type="number" min="0" max="20000" value="1" required></div>
<div class="field"><label for="units">تعداد واحدهای تولیدی</label><input id="units" name="units" type="number" min="1" max="1000" value="1" required></div>
<div class="field"><label for="cycle">دوره اشتراک</label><select id="cycle" name="cycle"><option value="annual">سالانه</option><option value="monthly">ماهانه</option></select></div>
<div class="field full"><label for="notes">توضیحات یا نیاز خاص</label><textarea id="notes" name="notes" maxlength="1000"></textarea></div>
<div class="field hp" aria-hidden="true"><label>Website<input name="website" tabindex="-1" autocomplete="off"></label></div>
<div id="summary" class="summary"></div><div id="message" class="message" role="status"></div><button id="submit" class="submit" type="submit">ثبت سفارش و دریافت پیش‌فاکتور</button></form>
<p class="note">پس از بررسی سفارش، حساب مدیر با همان محصولات انتخاب‌شده صادر می‌شود. نام کاربری و رمز هر بخش جدا نیست.</p></main>
<script>
const form=document.getElementById('order'),summary=document.getElementById('summary'),message=document.getElementById('message'),submit=document.getElementById('submit');
const labels=()=>[form.financial.checked?'مالی':'',form.operational.checked?'عملیاتی':'',form.weaving.checked?'راندمان سالن بافت':''].filter(Boolean);
function update(){const selected=labels();summary.textContent=selected.length?'محصولات انتخابی: '+selected.join(' + ')+' · مرکز فرمان مدیر نساجی: رایگان':'حداقل یک محصول را انتخاب کنید.';form.machines.required=form.weaving.checked;}
form.addEventListener('change',update);update();
form.addEventListener('submit',async e=>{e.preventDefault();message.className='message';if(!labels().length){message.textContent='حداقل یک محصول را انتخاب کنید.';message.className='message err';return;}submit.disabled=true;try{const payload={companyName:form.company.value.trim(),contactName:form.contact.value.trim(),mobile:form.mobile.value.trim(),email:form.email.value.trim(),allowFinancial:form.financial.checked,allowOperational:form.operational.checked,allowWeaving:form.weaving.checked,employeeCount:Number(form.employees.value),machineCount:Number(form.machines.value),unitCount:Number(form.units.value),billingCycle:form.cycle.value,notes:form.notes.value.trim(),website:form.website.value};const res=await fetch('/api/public/purchase-orders',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});const data=await res.json();if(!res.ok)throw new Error(data.error||'ثبت سفارش انجام نشد.');message.innerHTML='<strong>سفارش با موفقیت ثبت شد.</strong><br>کد پیگیری: <b dir="ltr">'+data.id+'</b><br>'+data.message;message.className='message ok';form.querySelectorAll('input,select,textarea,button').forEach(el=>el.disabled=true);message.scrollIntoView({behavior:'smooth'});}catch(err){message.textContent=err.message;message.className='message err';submit.disabled=false;}});
</script></body></html>`

var adminOrdersHTML = `<!doctype html><html lang="fa" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>سفارش‌های خرید Textile ERP</title><style>
*{box-sizing:border-box}body{margin:0;background:#0b1422;color:#e8eef8;font-family:Tahoma,Arial}.top{position:sticky;top:0;z-index:2;display:flex;justify-content:space-between;align-items:center;gap:12px;background:#111e31;border-bottom:1px solid #2f4058;padding:15px 22px}.top h1{font-size:21px;margin:0}.top a,.btn{border:1px solid #40536d;border-radius:10px;background:#172840;color:#dbeafe;padding:9px 12px;text-decoration:none;cursor:pointer;font:inherit}.wrap{max-width:1180px;margin:auto;padding:24px}.toolbar{display:flex;justify-content:space-between;align-items:center;gap:10px;margin-bottom:18px}.filters{display:flex;gap:8px;flex-wrap:wrap}.filters button.active{background:#2563eb}.order{border:1px solid #30445d;background:#101d2f;border-radius:16px;padding:18px;margin-bottom:14px}.head{display:flex;justify-content:space-between;gap:14px;align-items:start}.id{direction:ltr;color:#93c5fd;font-weight:bold}.meta{color:#9fb0c8;font-size:13px;line-height:2;margin-top:8px}.tags{display:flex;gap:7px;flex-wrap:wrap;margin-top:10px}.tag{border-radius:999px;padding:5px 9px;background:#164e63;color:#cffafe;font-size:11px}.status{border-radius:999px;padding:6px 10px;font-size:11px}.pending{background:#78350f;color:#fde68a}.approved{background:#064e3b;color:#a7f3d0}.rejected{background:#7f1d1d;color:#fecaca}.actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px}.approve{background:#047857;border-color:#10b981}.reject{background:#991b1b;border-color:#ef4444}.empty{text-align:center;color:#94a3b8;padding:60px}.result{display:none;border:1px solid #2d6a5d;background:#0c302b;color:#d1fae5;border-radius:14px;padding:16px;margin-bottom:18px;line-height:2}.result.show{display:block}.cred{direction:ltr;text-align:left;background:#07111e;border:1px dashed #4d6a88;border-radius:9px;padding:10px;margin-top:8px;overflow-wrap:anywhere}@media(max-width:700px){.head,.toolbar{display:grid}.wrap{padding:14px}}
</style></head><body><header class="top"><h1>سفارش‌های خرید</h1><div><a href="/admin">مدیریت مشتریان</a> <a href="/plans" target="_blank">صفحه خرید</a></div></header><main class="wrap"><div id="result" class="result"></div><div class="toolbar"><div id="count"></div><div class="filters"><button class="btn active" data-filter="all">همه</button><button class="btn" data-filter="pending">در انتظار</button><button class="btn" data-filter="approved">تأییدشده</button><button class="btn" data-filter="rejected">ردشده</button></div></div><section id="orders"></section></main><script>
const box=document.getElementById('orders'),count=document.getElementById('count'),result=document.getElementById('result');let rows=[],filter='all';const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));const moduleText=o=>[o.allow_financial?'مالی':'',o.allow_operational?'عملیاتی':'',o.allow_weaving?'راندمان سالن':''].filter(Boolean).join(' + ');const statusText=s=>s==='approved'?'تأییدشده':s==='rejected'?'ردشده':'در انتظار';
async function api(url,options={}){const res=await fetch(url,{headers:{'Content-Type':'application/json'},...options});const data=await res.json().catch(()=>({}));if(!res.ok)throw new Error(data.error||'خطا در ارتباط با سامانه');return data;}
function render(){const list=rows.filter(r=>filter==='all'||r.status===filter);count.textContent=list.length+' سفارش';if(!list.length){box.innerHTML='<div class="empty">سفارشی در این وضعیت وجود ندارد.</div>';return;}box.innerHTML=list.map(o=>'<article class="order"><div class="head"><div><div class="id">'+esc(o.id)+'</div><h2>'+esc(o.company_name)+'</h2></div><span class="status '+esc(o.status)+'">'+statusText(o.status)+'</span></div><div class="meta">مدیر: '+esc(o.contact_name)+' · همراه: <span dir="ltr">'+esc(o.mobile)+'</span>'+(o.email?' · ایمیل: <span dir="ltr">'+esc(o.email)+'</span>':'')+'<br>کارکنان: '+o.employee_count+' · ماشین‌ها: '+o.machine_count+' · واحدها: '+o.unit_count+' · دوره: '+(o.billing_cycle==='annual'?'سالانه':'ماهانه')+'</div><div class="tags">'+moduleText(o).split(' + ').map(x=>'<span class="tag">'+esc(x)+'</span>').join('')+'</div>'+(o.notes?'<div class="meta">توضیحات: '+esc(o.notes)+'</div>':'')+(o.status==='pending'?'<div class="actions"><button class="btn approve" data-action="approve" data-id="'+esc(o.id)+'">تأیید و ساخت حساب</button><button class="btn reject" data-action="reject" data-id="'+esc(o.id)+'">رد سفارش</button></div>':'')+'</article>').join('');}
async function load(){const data=await api('/admin/api/orders');rows=data.items||[];render();}
document.querySelectorAll('[data-filter]').forEach(b=>b.onclick=()=>{filter=b.dataset.filter;document.querySelectorAll('[data-filter]').forEach(x=>x.classList.toggle('active',x===b));render();});
box.addEventListener('click',async e=>{const btn=e.target.closest('[data-action]');if(!btn)return;const action=btn.dataset.action,id=btn.dataset.id;if(!confirm(action==='approve'?'این سفارش تأیید و حساب مدیر ساخته شود؟':'این سفارش رد شود؟'))return;btn.disabled=true;try{const data=await api('/admin/api/orders/'+encodeURIComponent(id)+'/'+action,{method:'POST',body:'{}'});if(data.access){const a=data.access;result.innerHTML='<strong>حساب مشتری ساخته شد</strong><div class="cred">Username: '+esc(a.username)+'</div><div class="cred">Password: '+esc(a.password)+'</div><div class="cred">Login: https://textile.vioraapps.com/login</div><button class="btn" id="copy">کپی اطلاعات ورود</button>';result.className='result show';document.getElementById('copy').onclick=()=>navigator.clipboard.writeText('ورود: https://textile.vioraapps.com/login\nنام کاربری: '+a.username+'\nرمز عبور: '+a.password+'\nمحصولات: '+a.moduleAccessLabel);}await load();}catch(err){alert(err.message);btn.disabled=false;}});load().catch(err=>box.innerHTML='<div class="empty">'+esc(err.message)+'</div>');
</script></body></html>`
