package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
	"github.com/erpsystem/textile-erp/internal/presentation/middleware"
)

func pairingHash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func randomPairingCode() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

type mobileTransactionRequest struct {
	ExternalID      string           `json:"external_id"`
	Title           string           `json:"title"`
	Direction       string           `json:"direction"`
	AccountID       string           `json:"account_id"`
	Group           string           `json:"group"`
	Subgroup        string           `json:"subgroup"`
	Customer        string           `json:"customer"`
	Bank            string           `json:"bank"`
	Sender          string           `json:"sender"`
	Description     string           `json:"description"`
	OccurredJalali  string           `json:"occurred_jalali"`
	Amount          int64            `json:"amount"`
	ReportedBalance *int64           `json:"reported_balance"`
	TrackingNo      string           `json:"tracking_no"`
	CounterAccount  string           `json:"counter_account"`
	Groups          []map[string]any `json:"groups"`
	BankRules       []map[string]any `json:"bank_rules"`
}

func mobileCanonicalBankName(value string) string {
	original := strings.TrimSpace(value)
	clean := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(original, " ", ""), "-", ""))
	switch {
	case strings.Contains(clean, "melal") || strings.Contains(clean, "ملل"):
		return "بانک ملل"
	case strings.Contains(clean, "mellat") || strings.Contains(clean, "ملت"):
		return "بانک ملت"
	case strings.Contains(clean, "tejarat") || strings.Contains(clean, "تجارت"):
		return "بانک تجارت"
	case strings.Contains(clean, "saderat") || strings.Contains(clean, "صادرات"):
		return "بانک صادرات"
	case strings.Contains(clean, "melli") || strings.Contains(clean, "ملی"):
		return "بانک ملی"
	default:
		return original
	}
}

func normalizeMobileDirection(direction, fallback string) string {
	clean := strings.ToLower(strings.TrimSpace(direction + " " + fallback))
	clean = strings.NewReplacer("ي", "ی", "ك", "ک", "‌", "", " ", "", "-", "", "_", "").Replace(clean)
	switch {
	case strings.Contains(clean, "in") || strings.Contains(clean, "income") || strings.Contains(clean, "credit") ||
		strings.Contains(clean, "deposit") || strings.Contains(clean, "واریز") || strings.Contains(clean, "دریافت") ||
		strings.Contains(clean, "بستانکار") || strings.Contains(clean, "درآمد"):
		return "in"
	case strings.Contains(clean, "out") || strings.Contains(clean, "expense") || strings.Contains(clean, "cost") ||
		strings.Contains(clean, "debit") || strings.Contains(clean, "withdraw") || strings.Contains(clean, "برداشت") ||
		strings.Contains(clean, "پرداخت") || strings.Contains(clean, "خرید") || strings.Contains(clean, "هزینه"):
		return "out"
	default:
		return ""
	}
}

func mobileIsTransferGroup(value string) bool {
	clean := strings.ToLower(strings.TrimSpace(value))
	clean = strings.NewReplacer("ي", "ی", "ك", "ک", "‌", "", " ", "", "-", "", "_", "").Replace(clean)
	return strings.Contains(clean, "انتقال") || strings.Contains(clean, "transfer")
}

func mobileJalaliOrder(value string) string {
	replacer := strings.NewReplacer("۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4", "۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9", "٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4", "٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9")
	normalized := replacer.Replace(strings.TrimSpace(value))
	var digits strings.Builder
	for _, char := range normalized {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
		}
	}
	return digits.String()
}

func (h *APIHandler) CreateMobilePairing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if postgres.DB == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	var req struct {
		APIBase string `json:"api_base"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	apiBase := strings.TrimRight(strings.TrimSpace(req.APIBase), "/")
	if apiBase == "" {
		apiBase = strings.TrimRight("https://"+r.Host+"/api", "/")
	}
	code, err := randomPairingCode()
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Could not create pairing code")
		return
	}
	companyID, userID := requestctx.CompanyID(r.Context()), requestctx.UserID(r.Context())
	expires := time.Now().Add(30 * time.Minute)
	_, err = postgres.DB.ExecContext(r.Context(), `
		INSERT INTO mobile_pairing_codes(code_hash, company_id, created_by, expires_at)
		VALUES($1,$2,$3,$4)
	`, pairingHash(code), companyID, userID, expires)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Could not save pairing code")
		return
	}
	payload, _ := json.Marshal(map[string]any{"v": 1, "api_base": apiBase, "code": code})
	RespondJSON(w, http.StatusCreated, map[string]any{"payload": string(payload), "expires_at": expires, "company_id": companyID})
}

func (h *APIHandler) PairMobileDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if postgres.DB == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	var req struct {
		Code       string `json:"code"`
		DeviceKey  string `json:"device_key"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.DeviceKey) == "" {
		RespondError(w, http.StatusBadRequest, "Pairing code and device key are required")
		return
	}
	tx, err := postgres.DB.BeginTx(r.Context(), nil)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Pairing failed")
		return
	}
	defer tx.Rollback()
	var companyID, createdBy int64
	err = tx.QueryRowContext(r.Context(), `
		SELECT company_id, COALESCE(created_by, 0) FROM mobile_pairing_codes
		WHERE code_hash=$1 AND used_at IS NULL AND expires_at > CURRENT_TIMESTAMP FOR UPDATE
	`, pairingHash(strings.TrimSpace(req.Code))).Scan(&companyID, &createdBy)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Pairing code is invalid or expired")
		return
	}
	name := strings.TrimSpace(req.DeviceName)
	if name == "" {
		name = "HesabYar Android"
	}
	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO mobile_devices(company_id, device_key, device_name, paired_by)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(company_id, device_key) DO UPDATE SET device_name=EXCLUDED.device_name, paired_by=EXCLUDED.paired_by, revoked_at=NULL, last_seen_at=CURRENT_TIMESTAMP
	`, companyID, strings.TrimSpace(req.DeviceKey), name, createdBy)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE mobile_pairing_codes SET used_at=CURRENT_TIMESTAMP WHERE code_hash=$1`, pairingHash(strings.TrimSpace(req.Code)))
	}
	if err != nil || tx.Commit() != nil {
		RespondError(w, http.StatusInternalServerError, "Pairing failed")
		return
	}
	token, err := middleware.SignJWT(map[string]interface{}{
		"user_id": createdBy, "company_id": companyID, "role": "mobile", "project_key": "textile-mobile",
		"device_key": strings.TrimSpace(req.DeviceKey), "exp": time.Now().Add(365 * 24 * time.Hour).Unix(),
	})
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Could not create device token")
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"token": token, "company_id": companyID, "api_version": 1})
}

func (h *APIHandler) MobileBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	if !h.mobileDeviceActive(r, companyID) {
		RespondError(w, http.StatusUnauthorized, "Device is not paired")
		return
	}
	doc, err := loadWorkspace(r, companyID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state := decodeWorkspaceMap(doc.State)
	accounts := rowsFrom(state, "accounts")
	movements := rowsFrom(state, "movements")
	mobileAccounts := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		copyAccount := map[string]any{}
		for key, value := range account {
			copyAccount[key] = value
		}
		balance := mobileInt64(account["opening"])
		for _, movement := range movements {
			if stringValue(movement["accountId"]) == stringValue(account["id"]) {
				if stringValue(movement["direction"]) == "in" {
					balance += mobileInt64(movement["amount"])
				} else {
					balance -= mobileInt64(movement["amount"])
				}
			}
		}
		copyAccount["balance"] = balance
		mobileAccounts = append(mobileAccounts, copyAccount)
	}
	customers := uniqueWorkspaceCustomers(state)
	seen := map[string]bool{}
	for _, customer := range customers {
		seen[stringValue(customer["name"])] = true
	}
	if h.operational != nil {
		if bridge, closeBridge, bridgeErr := h.operational.ForCompany(r.Context(), companyID); bridgeErr == nil {
			if rows, rowsErr := bridge.Customers(); rowsErr == nil {
				for _, row := range rows {
					if strings.TrimSpace(row.Name) != "" && !seen[row.Name] {
						customers = append(customers, map[string]any{"id": row.ID, "name": row.Name})
						seen[row.Name] = true
					}
				}
			}
			closeBridge()
		}
	}
	RespondJSON(w, http.StatusOK, map[string]any{
		"company_id": companyID, "revision": doc.Revision, "accounts": mobileAccounts,
		"customers": customers, "groups": rowsFrom(state, "smsGroups"), "bank_rules": rowsFrom(state, "smsBankSenders"),
		"receivable_checks": rowsFrom(state, "receivableDocs"), "payable_checks": rowsFrom(state, "payableDocs"),
	})
}

func (h *APIHandler) MobileTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		h.deleteMobileTransaction(w, r)
		return
	}
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	if !h.mobileDeviceActive(r, companyID) {
		RespondError(w, http.StatusUnauthorized, "Device is not paired")
		return
	}
	var req mobileTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ExternalID) == "" || req.Amount <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid transaction")
		return
	}
	req.Direction = normalizeMobileDirection(req.Direction, "")
	if req.Direction == "" {
		RespondError(w, http.StatusBadRequest, "Invalid transaction")
		return
	}
	var saved workspaceDocument
	for attempt := 0; attempt < 3; attempt++ {
		doc, err := loadWorkspace(r, companyID)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		state := decodeWorkspaceMap(doc.State)
		for _, row := range rowsFrom(state, "mobileTransactions") {
			if stringValue(row["externalId"]) == req.ExternalID {
				RespondJSON(w, http.StatusOK, map[string]any{"status": "duplicate", "revision": doc.Revision})
				return
			}
		}
		now := time.Now().UTC().Format(time.RFC3339)
		occurred := strings.TrimSpace(req.OccurredJalali)
		accountName := mobileCanonicalBankName(req.Bank)
		if accountName == "" {
			accountName = mobileCanonicalBankName(req.AccountID)
		}
		accounts := rowsFrom(state, "accounts")
		accountIndex := -1
		for index, account := range accounts {
			if stringValue(account["id"]) == req.AccountID || strings.EqualFold(stringValue(account["name"]), accountName) || strings.EqualFold(stringValue(account["name"]), req.AccountID) {
				accountIndex = index
				break
			}
		}
		if accountIndex < 0 {
			accountID := "bank-mobile-" + pairingHash(accountName)[:12]
			accounts = append(accounts, map[string]any{"id": accountID, "name": accountName, "type": "بانک", "opening": int64(0), "source": "mobile_sms", "mobileManaged": true})
			accountIndex = len(accounts) - 1
		}
		resolvedAccountID := stringValue(accounts[accountIndex]["id"])
		if req.ReportedBalance != nil && (mobileJalaliOrder(stringValue(accounts[accountIndex]["lastReportedJalali"])) == "" || mobileJalaliOrder(req.OccurredJalali) >= mobileJalaliOrder(stringValue(accounts[accountIndex]["lastReportedJalali"]))) {
			movementNet := int64(0)
			for _, movement := range rowsFrom(state, "movements") {
				if stringValue(movement["accountId"]) == resolvedAccountID {
					if stringValue(movement["direction"]) == "in" {
						movementNet += mobileInt64(movement["amount"])
					} else {
						movementNet -= mobileInt64(movement["amount"])
					}
				}
			}
			currentEffect := req.Amount
			if req.Direction == "out" {
				currentEffect = -currentEffect
			}
			accounts[accountIndex]["opening"] = *req.ReportedBalance - movementNet - currentEffect
			accounts[accountIndex]["lastReportedBalance"] = *req.ReportedBalance
			accounts[accountIndex]["lastReportedJalali"] = strings.TrimSpace(req.OccurredJalali)
			accounts[accountIndex]["lastMobileSyncAt"] = now
		}
		isTransfer := strings.TrimSpace(req.CounterAccount) != "" && mobileIsTransferGroup(req.Group)
		counterAccountID := ""
		if isTransfer {
			for index, account := range accounts {
				if stringValue(account["id"]) == req.CounterAccount || strings.EqualFold(stringValue(account["name"]), req.CounterAccount) {
					counterAccountID = stringValue(accounts[index]["id"])
					break
				}
			}
			if counterAccountID == "" {
				counterAccountID = "bank-mobile-" + pairingHash(req.CounterAccount)[:12]
				accounts = append(accounts, map[string]any{"id": counterAccountID, "name": strings.TrimSpace(req.CounterAccount), "type": "بانک", "opening": int64(0), "source": "mobile_sms", "mobileManaged": true})
			}
		}
		state["accounts"] = mapsToAny(accounts)
		counterparty := strings.TrimSpace(req.Customer)
		if counterparty == "" {
			counterparty = strings.TrimSpace(req.Group + " / " + req.Subgroup)
		}
		mobileRow := map[string]any{"id": "sms-" + req.ExternalID, "externalId": req.ExternalID, "title": req.Title, "amount": req.Amount, "direction": req.Direction, "accountId": resolvedAccountID, "counterAccountId": counterAccountID, "counterAccount": req.CounterAccount, "group": req.Group, "subgroup": req.Subgroup, "customer": req.Customer, "counterparty": counterparty, "bank": accountName, "sender": req.Sender, "trackingNo": req.TrackingNo, "reportedBalance": req.ReportedBalance, "occurredAt": occurred, "syncedAt": now}
		state["mobileTransactions"] = append([]any{mobileRow}, anyRows(state, "mobileTransactions")...)
		trackingNo := strings.TrimSpace(req.TrackingNo)
		if trackingNo == "" {
			trackingNo = req.ExternalID
		}
		movement := map[string]any{"id": "mov-sms-" + req.ExternalID, "accountId": resolvedAccountID, "date": occurred, "direction": req.Direction, "amount": req.Amount, "payer": req.Customer, "counterparty": counterparty, "trackingNo": trackingNo, "description": req.Description, "source_type": "mobile_sms", "sourceId": req.ExternalID, "bank": accountName, "group": req.Group, "subgroup": req.Subgroup}
		state["movements"] = append([]any{movement}, anyRows(state, "movements")...)
		if isTransfer {
			counterDirection := "out"
			if req.Direction == "out" {
				counterDirection = "in"
			}
			counterMovement := map[string]any{"id": "mov-sms-" + req.ExternalID + "-counter", "accountId": counterAccountID, "date": occurred, "direction": counterDirection, "amount": req.Amount, "payer": req.Customer, "counterparty": accountName, "trackingNo": trackingNo, "description": "انتقال بین " + accountName + " و " + req.CounterAccount, "source_type": "mobile_sms_transfer", "sourceId": req.ExternalID, "bank": req.CounterAccount, "group": req.Group, "subgroup": req.Subgroup}
			state["movements"] = append([]any{counterMovement}, anyRows(state, "movements")...)
		}
		if req.Direction == "out" && !isTransfer {
			expense := map[string]any{"id": "exp-sms-" + req.ExternalID, "date": occurred, "group": req.Group, "subgroup": req.Subgroup, "amount": req.Amount, "description": req.Description, "accountId": resolvedAccountID, "customer": req.Customer, "counterparty": counterparty, "source_type": "mobile_sms", "sourceId": req.ExternalID, "bank": accountName}
			state["expenses"] = append([]any{expense}, anyRows(state, "expenses")...)
		}
		debitAccount, creditAccount := "بانک: "+accountName, "درآمد/طرف حساب: "+counterparty
		if req.Direction == "out" {
			debitAccount, creditAccount = "هزینه: "+strings.TrimSpace(req.Group+" / "+req.Subgroup), "بانک: "+accountName
		}
		if isTransfer && req.Direction == "out" {
			debitAccount, creditAccount = "بانک: "+req.CounterAccount, "بانک: "+accountName
		}
		if isTransfer && req.Direction == "in" {
			debitAccount, creditAccount = "بانک: "+accountName, "بانک: "+req.CounterAccount
		}
		journal := map[string]any{"id": "journal-sms-" + req.ExternalID, "date": occurred, "description": req.Description, "source_type": "mobile_sms", "sourceId": req.ExternalID, "trackingNo": trackingNo, "lines": []any{map[string]any{"account": debitAccount, "debit": req.Amount, "credit": 0, "counterparty": counterparty}, map[string]any{"account": creditAccount, "debit": 0, "credit": req.Amount, "counterparty": counterparty}}}
		state["journalEntries"] = append([]any{journal}, anyRows(state, "journalEntries")...)
		if len(req.Groups) > 0 {
			state["smsGroups"] = mapsToAny(req.Groups)
		}
		if len(req.BankRules) > 0 {
			state["smsBankSenders"] = mapsToAny(req.BankRules)
		}
		raw, _ := json.Marshal(state)
		canonical, checksum, err := validateWorkspaceState(raw)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		expected := doc.Revision
		saved, err = saveWorkspace(r, companyID, requestctx.UserID(r.Context()), &expected, canonical, checksum, nil)
		if err == nil {
			RespondJSON(w, http.StatusCreated, map[string]any{"status": "synced", "revision": saved.Revision})
			return
		}
		var conflict workspaceConflict
		if !errors.As(err, &conflict) {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	RespondError(w, http.StatusConflict, "Workspace changed; retry sync")
}

func (h *APIHandler) HesabYarLegacySync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	if strings.EqualFold(requestctx.UserRole(r.Context()), "mobile") && !h.mobileDeviceActive(r, companyID) {
		RespondError(w, http.StatusUnauthorized, "Device is not paired")
		return
	}
	var raw map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid HesabYar sync payload")
		return
	}
	payload := raw
	if nested, ok := raw["payload"].(map[string]any); ok {
		payload = nested
	}
	transactions := rowsFrom(payload, "transactions")
	if len(transactions) == 0 {
		RespondJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "empty", "synced": 0})
		return
	}
	var saved workspaceDocument
	synced := 0
	for attempt := 0; attempt < 3; attempt++ {
		doc, err := loadWorkspace(r, companyID)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		state := decodeWorkspaceMap(doc.State)
		now := time.Now().UTC().Format(time.RFC3339)
		for _, row := range transactions {
			if legacyReviewedValue(row["reviewed"]) == false {
				continue
			}
			req := legacyHesabYarTransaction(row)
			if strings.TrimSpace(req.ExternalID) == "" || req.Amount <= 0 || req.Direction == "" {
				continue
			}
			if mobileTransactionExists(state, req.ExternalID) {
				continue
			}
			appendMobileTransactionState(state, req, now)
			synced++
		}
		if groups := rowsFrom(payload, "groups"); len(groups) > 0 {
			state["smsGroups"] = mapsToAny(groups)
		}
		if groups := rowsFrom(payload, "category_groups"); len(groups) > 0 {
			state["smsGroups"] = mapsToAny(groups)
		}
		if rules := rowsFrom(payload, "bank_rules"); len(rules) > 0 {
			state["smsBankSenders"] = mapsToAny(rules)
		}
		rawState, _ := json.Marshal(state)
		canonical, checksum, err := validateWorkspaceState(rawState)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		expected := doc.Revision
		saved, err = saveWorkspace(r, companyID, requestctx.UserID(r.Context()), &expected, canonical, checksum, nil)
		if err == nil {
			RespondJSON(w, http.StatusCreated, map[string]any{"ok": true, "status": "synced", "synced": synced, "revision": saved.Revision})
			return
		}
		var conflict workspaceConflict
		if !errors.As(err, &conflict) {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		synced = 0
	}
	RespondError(w, http.StatusConflict, "Workspace changed; retry sync")
}

func legacyHesabYarTransaction(row map[string]any) mobileTransactionRequest {
	category := strings.TrimSpace(stringValue(row["category"]))
	group := strings.TrimSpace(stringValue(row["group"]))
	subgroup := strings.TrimSpace(stringValue(row["subgroup"]))
	if category != "" && (group == "" || subgroup == "") {
		parts := strings.Split(category, "/")
		if group == "" && len(parts) > 0 {
			group = strings.TrimSpace(parts[0])
		}
		if subgroup == "" && len(parts) > 1 {
			subgroup = strings.TrimSpace(parts[1])
		}
	}
	externalID := strings.TrimSpace(stringValue(row["external_id"]))
	if externalID == "" {
		externalID = strings.TrimSpace(stringValue(row["id"]))
	}
	accountID := strings.TrimSpace(stringValue(row["account_id"]))
	if accountID == "" {
		accountID = strings.TrimSpace(stringValue(row["account"]))
	}
	occurred := strings.TrimSpace(stringValue(row["occurred_jalali"]))
	if occurred == "" {
		occurred = strings.TrimSpace(stringValue(row["jalaliDateTime"]))
	}
	description := strings.TrimSpace(stringValue(row["description"]))
	if description == "" {
		description = strings.TrimSpace(stringValue(row["title"]))
	}
	return mobileTransactionRequest{
		ExternalID:     externalID,
		Title:          strings.TrimSpace(stringValue(row["title"])),
		Direction:      normalizeMobileDirection(stringValue(row["direction"]), stringValue(row["type"])),
		AccountID:      accountID,
		Group:          group,
		Subgroup:       subgroup,
		Customer:       strings.TrimSpace(stringValue(row["customer"])),
		Bank:           strings.TrimSpace(stringValue(row["bank"])),
		Sender:         strings.TrimSpace(stringValue(row["sender"])),
		Description:    description,
		OccurredJalali: occurred,
		Amount:         mobileInt64(row["amount"]),
		TrackingNo:     strings.TrimSpace(stringValue(row["tracking_no"])),
		CounterAccount: strings.TrimSpace(stringValue(row["counter_account"])),
	}
}

func legacyReviewedValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return !strings.EqualFold(strings.TrimSpace(typed), "false")
	default:
		return true
	}
}

func mobileTransactionExists(state map[string]any, externalID string) bool {
	for _, row := range rowsFrom(state, "mobileTransactions") {
		if stringValue(row["externalId"]) == externalID {
			return true
		}
	}
	return false
}

func appendMobileTransactionState(state map[string]any, req mobileTransactionRequest, now string) {
	occurred := strings.TrimSpace(req.OccurredJalali)
	accountName := mobileCanonicalBankName(req.Bank)
	if accountName == "" {
		accountName = mobileCanonicalBankName(req.AccountID)
	}
	accounts := rowsFrom(state, "accounts")
	accountIndex := -1
	for index, account := range accounts {
		if stringValue(account["id"]) == req.AccountID || strings.EqualFold(stringValue(account["name"]), accountName) || strings.EqualFold(stringValue(account["name"]), req.AccountID) {
			accountIndex = index
			break
		}
	}
	if accountIndex < 0 {
		accountID := "bank-mobile-" + pairingHash(accountName)[:12]
		accounts = append(accounts, map[string]any{"id": accountID, "name": accountName, "type": "بانک", "opening": int64(0), "source": "mobile_sms", "mobileManaged": true})
		accountIndex = len(accounts) - 1
	}
	resolvedAccountID := stringValue(accounts[accountIndex]["id"])
	isTransfer := strings.TrimSpace(req.CounterAccount) != "" && mobileIsTransferGroup(req.Group)
	counterAccountID := ""
	if isTransfer {
		for index, account := range accounts {
			if stringValue(account["id"]) == req.CounterAccount || strings.EqualFold(stringValue(account["name"]), req.CounterAccount) {
				counterAccountID = stringValue(accounts[index]["id"])
				break
			}
		}
		if counterAccountID == "" {
			counterAccountID = "bank-mobile-" + pairingHash(req.CounterAccount)[:12]
			accounts = append(accounts, map[string]any{"id": counterAccountID, "name": strings.TrimSpace(req.CounterAccount), "type": "بانک", "opening": int64(0), "source": "mobile_sms", "mobileManaged": true})
		}
	}
	state["accounts"] = mapsToAny(accounts)
	counterparty := strings.TrimSpace(req.Customer)
	if counterparty == "" {
		counterparty = strings.TrimSpace(req.Group + " / " + req.Subgroup)
	}
	mobileRow := map[string]any{"id": "sms-" + req.ExternalID, "externalId": req.ExternalID, "title": req.Title, "amount": req.Amount, "direction": req.Direction, "accountId": resolvedAccountID, "counterAccountId": counterAccountID, "counterAccount": req.CounterAccount, "group": req.Group, "subgroup": req.Subgroup, "customer": req.Customer, "counterparty": counterparty, "bank": accountName, "sender": req.Sender, "trackingNo": req.TrackingNo, "reportedBalance": req.ReportedBalance, "occurredAt": occurred, "syncedAt": now}
	state["mobileTransactions"] = append([]any{mobileRow}, anyRows(state, "mobileTransactions")...)
	trackingNo := strings.TrimSpace(req.TrackingNo)
	if trackingNo == "" {
		trackingNo = req.ExternalID
	}
	movement := map[string]any{"id": "mov-sms-" + req.ExternalID, "accountId": resolvedAccountID, "date": occurred, "direction": req.Direction, "amount": req.Amount, "payer": req.Customer, "counterparty": counterparty, "trackingNo": trackingNo, "description": req.Description, "source_type": "mobile_sms", "sourceId": req.ExternalID, "bank": accountName, "group": req.Group, "subgroup": req.Subgroup}
	state["movements"] = append([]any{movement}, anyRows(state, "movements")...)
	if isTransfer {
		counterDirection := "out"
		if req.Direction == "out" {
			counterDirection = "in"
		}
		counterMovement := map[string]any{"id": "mov-sms-" + req.ExternalID + "-counter", "accountId": counterAccountID, "date": occurred, "direction": counterDirection, "amount": req.Amount, "payer": req.Customer, "counterparty": accountName, "trackingNo": trackingNo, "description": "انتقال بین " + accountName + " و " + req.CounterAccount, "source_type": "mobile_sms_transfer", "sourceId": req.ExternalID, "bank": req.CounterAccount, "group": req.Group, "subgroup": req.Subgroup}
		state["movements"] = append([]any{counterMovement}, anyRows(state, "movements")...)
	}
	if req.Direction == "out" && !isTransfer {
		expense := map[string]any{"id": "exp-sms-" + req.ExternalID, "date": occurred, "group": req.Group, "subgroup": req.Subgroup, "amount": req.Amount, "description": req.Description, "accountId": resolvedAccountID, "customer": req.Customer, "counterparty": counterparty, "source_type": "mobile_sms", "sourceId": req.ExternalID, "bank": accountName}
		state["expenses"] = append([]any{expense}, anyRows(state, "expenses")...)
	}
	debitAccount, creditAccount := "بانک: "+accountName, "درآمد/طرف حساب: "+counterparty
	if req.Direction == "out" {
		debitAccount, creditAccount = "هزینه: "+strings.TrimSpace(req.Group+" / "+req.Subgroup), "بانک: "+accountName
	}
	if isTransfer && req.Direction == "out" {
		debitAccount, creditAccount = "بانک: "+req.CounterAccount, "بانک: "+accountName
	}
	if isTransfer && req.Direction == "in" {
		debitAccount, creditAccount = "بانک: "+accountName, "بانک: "+req.CounterAccount
	}
	journal := map[string]any{"id": "journal-sms-" + req.ExternalID, "date": occurred, "description": req.Description, "source_type": "mobile_sms", "sourceId": req.ExternalID, "trackingNo": trackingNo, "lines": []any{map[string]any{"account": debitAccount, "debit": req.Amount, "credit": 0, "counterparty": counterparty}, map[string]any{"account": creditAccount, "debit": 0, "credit": req.Amount, "counterparty": counterparty}}}
	state["journalEntries"] = append([]any{journal}, anyRows(state, "journalEntries")...)
}

func (h *APIHandler) deleteMobileTransaction(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	if !h.mobileDeviceActive(r, companyID) {
		RespondError(w, http.StatusUnauthorized, "Device is not paired")
		return
	}
	var req struct {
		ExternalID string `json:"external_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.ExternalID) == "" {
		RespondError(w, http.StatusBadRequest, "External transaction id is required")
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		doc, err := loadWorkspace(r, companyID)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		state := decodeWorkspaceMap(doc.State)
		state["mobileTransactions"] = removeMobileRows(state, "mobileTransactions", req.ExternalID)
		state["movements"] = removeMobileRows(state, "movements", req.ExternalID)
		state["expenses"] = removeMobileRows(state, "expenses", req.ExternalID)
		state["journalEntries"] = removeMobileRows(state, "journalEntries", req.ExternalID)
		for _, account := range rowsFrom(state, "accounts") {
			if _, managed := account["lastReportedBalance"]; !managed {
				continue
			}
			net := int64(0)
			for _, movement := range rowsFrom(state, "movements") {
				if stringValue(movement["accountId"]) == stringValue(account["id"]) {
					if stringValue(movement["direction"]) == "in" {
						net += mobileInt64(movement["amount"])
					} else {
						net -= mobileInt64(movement["amount"])
					}
				}
			}
			account["opening"] = mobileInt64(account["lastReportedBalance"]) - net
		}
		raw, _ := json.Marshal(state)
		canonical, checksum, validationErr := validateWorkspaceState(raw)
		if validationErr != nil {
			RespondError(w, http.StatusBadRequest, validationErr.Error())
			return
		}
		expected := doc.Revision
		saved, saveErr := saveWorkspace(r, companyID, requestctx.UserID(r.Context()), &expected, canonical, checksum, nil)
		if saveErr == nil {
			RespondJSON(w, http.StatusOK, map[string]any{"status": "deleted", "revision": saved.Revision})
			return
		}
		var conflict workspaceConflict
		if !errors.As(saveErr, &conflict) {
			RespondError(w, http.StatusInternalServerError, saveErr.Error())
			return
		}
	}
	RespondError(w, http.StatusConflict, "Workspace changed; retry delete")
}

func removeMobileRows(state map[string]any, key, externalID string) []any {
	result := []any{}
	for _, row := range anyRows(state, key) {
		item, ok := row.(map[string]any)
		if !ok {
			result = append(result, row)
			continue
		}
		if stringValue(item["externalId"]) == externalID || stringValue(item["sourceId"]) == externalID || strings.Contains(stringValue(item["id"]), "sms-"+externalID) {
			continue
		}
		result = append(result, row)
	}
	return result
}

func (h *APIHandler) MobileSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	if !h.mobileDeviceActive(r, companyID) {
		RespondError(w, http.StatusUnauthorized, "Device is not paired")
		return
	}
	var req struct {
		Groups       []map[string]any `json:"groups"`
		BankRules    []map[string]any `json:"bank_rules"`
		BankBalances []struct {
			Name       string `json:"name"`
			Balance    int64  `json:"balance"`
			ReportedAt string `json:"reported_at"`
		} `json:"bank_balances"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid settings")
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		doc, err := loadWorkspace(r, companyID)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		state := decodeWorkspaceMap(doc.State)
		state["smsGroups"] = mapsToAny(req.Groups)
		state["smsBankSenders"] = mapsToAny(req.BankRules)
		accounts := rowsFrom(state, "accounts")
		movements := rowsFrom(state, "movements")
		for _, reported := range req.BankBalances {
			accountName := mobileCanonicalBankName(reported.Name)
			if accountName == "" {
				continue
			}
			accountIndex := -1
			for index, account := range accounts {
				if mobileCanonicalBankName(stringValue(account["name"])) == accountName {
					accountIndex = index
					if stringValue(account["name"]) == accountName {
						break
					}
				}
			}
			if accountIndex < 0 {
				accountID := "bank-mobile-" + pairingHash(accountName)[:12]
				accounts = append(accounts, map[string]any{"id": accountID, "name": accountName, "type": "بانک", "opening": int64(0), "source": "mobile_sms", "mobileManaged": true})
				accountIndex = len(accounts) - 1
			}
			resolvedID := stringValue(accounts[accountIndex]["id"])
			accounts[accountIndex]["name"] = accountName
			latestExistingOrder := mobileJalaliOrder(stringValue(accounts[accountIndex]["lastReportedJalali"]))
			duplicateIDs := map[string]bool{}
			filteredAccounts := make([]map[string]any, 0, len(accounts))
			for index, account := range accounts {
				if index != accountIndex && mobileCanonicalBankName(stringValue(account["name"])) == accountName {
					if candidate := mobileJalaliOrder(stringValue(account["lastReportedJalali"])); candidate > latestExistingOrder {
						latestExistingOrder = candidate
					}
					duplicateIDs[stringValue(account["id"])] = true
					continue
				}
				filteredAccounts = append(filteredAccounts, account)
			}
			if len(duplicateIDs) > 0 {
				for _, movement := range movements {
					if duplicateIDs[stringValue(movement["accountId"])] {
						movement["accountId"] = resolvedID
					}
				}
				accounts = filteredAccounts
				for index, account := range accounts {
					if stringValue(account["id"]) == resolvedID {
						accountIndex = index
						break
					}
				}
			}
			incomingOrder := mobileJalaliOrder(reported.ReportedAt)
			if latestExistingOrder != "" && incomingOrder != "" && incomingOrder < latestExistingOrder {
				continue
			}
			movementNet := int64(0)
			for _, movement := range movements {
				if stringValue(movement["accountId"]) == resolvedID {
					if stringValue(movement["direction"]) == "in" {
						movementNet += mobileInt64(movement["amount"])
					} else {
						movementNet -= mobileInt64(movement["amount"])
					}
				}
			}
			accounts[accountIndex]["opening"] = reported.Balance - movementNet
			accounts[accountIndex]["lastReportedBalance"] = reported.Balance
			accounts[accountIndex]["lastReportedJalali"] = strings.TrimSpace(reported.ReportedAt)
			accounts[accountIndex]["lastMobileSyncAt"] = time.Now().UTC().Format(time.RFC3339)
		}
		state["accounts"] = mapsToAny(accounts)
		raw, _ := json.Marshal(state)
		canonical, checksum, err := validateWorkspaceState(raw)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		expected := doc.Revision
		saved, err := saveWorkspace(r, companyID, requestctx.UserID(r.Context()), &expected, canonical, checksum, nil)
		if err == nil {
			RespondJSON(w, http.StatusOK, map[string]any{"status": "synced", "revision": saved.Revision})
			return
		}
		var conflict workspaceConflict
		if !errors.As(err, &conflict) {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	RespondError(w, http.StatusConflict, "Workspace changed; retry sync")
}

func (h *APIHandler) mobileDeviceActive(r *http.Request, companyID int64) bool {
	claims, err := middleware.VerifyJWT(strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	if err != nil {
		return false
	}
	deviceKey := strings.TrimSpace(fmt.Sprint(claims["device_key"]))
	if deviceKey == "" {
		return false
	}
	var active bool
	err = postgres.DB.QueryRowContext(r.Context(), `SELECT revoked_at IS NULL FROM mobile_devices WHERE company_id=$1 AND device_key=$2`, companyID, deviceKey).Scan(&active)
	if err == nil && active {
		_, _ = postgres.DB.ExecContext(r.Context(), `UPDATE mobile_devices SET last_seen_at=CURRENT_TIMESTAMP WHERE company_id=$1 AND device_key=$2`, companyID, deviceKey)
	}
	return err == nil && active
}

func anyRows(state map[string]any, key string) []any { values, _ := state[key].([]any); return values }
func mapsToAny(values []map[string]any) []any {
	out := make([]any, len(values))
	for i := range values {
		out[i] = values[i]
	}
	return out
}

func mobileInt64(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case float32:
		return int64(number)
	case int64:
		return number
	case int:
		return int64(number)
	case json.Number:
		parsed, _ := number.Int64()
		return parsed
	case string:
		var parsed int64
		_, _ = fmt.Sscan(strings.ReplaceAll(number, ",", ""), &parsed)
		return parsed
	default:
		return 0
	}
}

func uniqueWorkspaceCustomers(state map[string]any) []map[string]any {
	seen := map[string]bool{}
	out := []map[string]any{}
	for _, key := range []string{"invoices", "incomingInvoices", "yarnOutInvoices", "receivableDocs", "payableDocs", "openingBalances"} {
		for _, row := range rowsFrom(state, key) {
			name := strings.TrimSpace(stringValue(row["customer"]))
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, map[string]any{"id": name, "name": name})
		}
	}
	return out
}
