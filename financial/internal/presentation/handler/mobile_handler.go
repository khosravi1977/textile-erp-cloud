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
	"strconv"
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

func classifyMobileTransaction(explicit, direction, group, customer, counterAccount string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	direction = strings.TrimSpace(direction)
	group = strings.TrimSpace(group)
	if direction != "in" && direction != "out" {
		return "", errors.New("transaction direction must be in or out")
	}
	allowed := map[string]bool{"transfer": true, "customer_receipt": true, "supplier_payment": true, "expense": true, "other_income": true}
	if explicit != "" {
		if !allowed[explicit] {
			return "", errors.New("invalid transaction type")
		}
		if explicit == "transfer" && strings.TrimSpace(counterAccount) == "" {
			return "", errors.New("counter account is required for transfer")
		}
		return explicit, nil
	}
	if strings.TrimSpace(counterAccount) != "" && group == "انتقال" {
		return "transfer", nil
	}
	// A selected expense group is stronger evidence than a possibly stale
	// customer left in the Android form from the previous transaction.
	if direction == "out" && group != "" && group != "انتقال" {
		return "expense", nil
	}
	if strings.TrimSpace(customer) != "" && direction == "in" {
		return "customer_receipt", nil
	}
	if strings.TrimSpace(customer) != "" && direction == "out" {
		return "supplier_payment", nil
	}
	if direction == "out" {
		return "expense", nil
	}
	return "other_income", nil
}

func setUnconfirmedCounterparty(row map[string]any, candidate string) {
	row["payer"] = ""
	row["customer"] = ""
	row["counterpartyCandidate"] = strings.TrimSpace(candidate)
	row["counterpartyConfirmed"] = false
}

// mobileAccountingDate accepts ISO dates and the yyyy/mm/dd Jalali dates sent by
// the offline Android application. The original Jalali value is retained on the
// workspace row; this conversion provides a valid Gregorian posting date.
func mobileAccountingDate(value string) string {
	value = strings.TrimSpace(strings.NewReplacer(
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
		"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
		"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
		"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
	).Replace(value))
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Format("2006-01-02")
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '-' || r == '.' })
	if len(parts) != 3 {
		return time.Now().Format("2006-01-02")
	}
	year, errY := strconv.Atoi(parts[0])
	month, errM := strconv.Atoi(parts[1])
	day, errD := strconv.Atoi(parts[2])
	if errY != nil || errM != nil || errD != nil || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Now().Format("2006-01-02")
	}
	if year >= 1700 {
		if parsed, err := time.Parse("2006-1-2", fmt.Sprintf("%d-%d-%d", year, month, day)); err == nil {
			return parsed.Format("2006-01-02")
		}
		return time.Now().Format("2006-01-02")
	}
	gy, gm, gd := jalaliToGregorian(year, month, day)
	parsed := time.Date(gy, time.Month(gm), gd, 0, 0, 0, 0, time.Local)
	return parsed.Format("2006-01-02")
}

func jalaliToGregorian(jy, jm, jd int) (int, int, int) {
	jy += 1595
	days := -355668 + (365 * jy) + ((jy / 33) * 8) + (((jy % 33) + 3) / 4) + jd
	if jm < 7 {
		days += (jm - 1) * 31
	} else {
		days += ((jm - 7) * 30) + 186
	}
	gy := 400 * (days / 146097)
	days %= 146097
	if days > 36524 {
		days--
		gy += 100 * (days / 36524)
		days %= 36524
		if days >= 365 {
			days++
		}
	}
	gy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		gy += (days - 1) / 365
		days = (days - 1) % 365
	}
	gd := days + 1
	leap := (gy%4 == 0 && gy%100 != 0) || gy%400 == 0
	monthDays := []int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if leap {
		monthDays[2] = 29
	}
	gm := 1
	for gm <= 12 && gd > monthDays[gm] {
		gd -= monthDays[gm]
		gm++
	}
	return gy, gm, gd
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
	var req struct {
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
		ReportedBalance *int64           `json:"reported_balance"`
		TrackingNo      string           `json:"tracking_no"`
		CounterAccount  string           `json:"counter_account"`
		Groups          []map[string]any `json:"groups"`
		BankRules       []map[string]any `json:"bank_rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ExternalID) == "" || req.Amount <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid transaction")
		return
	}
	transactionType, err := classifyMobileTransaction(req.TransactionType, req.Direction, req.Group, req.Customer, req.CounterAccount)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
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
		occurredJalali := strings.TrimSpace(req.OccurredJalali)
		occurred := mobileAccountingDate(occurredJalali)
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
		isTransfer := transactionType == "transfer"
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
		customer := strings.TrimSpace(req.Customer)
		if transactionType == "expense" || transactionType == "transfer" || transactionType == "other_income" {
			customer = ""
		}
		counterparty := strings.Trim(strings.TrimSpace(req.Group+" / "+req.Subgroup), " /")
		mobileRow := map[string]any{"id": "sms-" + req.ExternalID, "externalId": req.ExternalID, "title": req.Title, "amount": req.Amount, "direction": req.Direction, "transactionType": transactionType, "transactionTypeExplicit": strings.TrimSpace(req.TransactionType) != "", "accountId": resolvedAccountID, "counterAccountId": counterAccountID, "counterAccount": req.CounterAccount, "group": req.Group, "subgroup": req.Subgroup, "reportedCustomer": req.Customer, "counterparty": counterparty, "bank": accountName, "sender": req.Sender, "trackingNo": req.TrackingNo, "reportedBalance": req.ReportedBalance, "occurredAt": occurred, "occurredJalali": occurredJalali, "syncedAt": now}
		setUnconfirmedCounterparty(mobileRow, customer)
		state["mobileTransactions"] = append([]any{mobileRow}, anyRows(state, "mobileTransactions")...)
		trackingNo := strings.TrimSpace(req.TrackingNo)
		if trackingNo == "" {
			trackingNo = req.ExternalID
		}
		movement := map[string]any{"id": "mov-sms-" + req.ExternalID, "accountId": resolvedAccountID, "counterAccountId": counterAccountID, "date": occurred, "occurredJalali": occurredJalali, "direction": req.Direction, "transactionType": transactionType, "amount": req.Amount, "counterparty": counterparty, "trackingNo": trackingNo, "description": req.Description, "sourceMobileTransaction": req.ExternalID, "source_type": "mobile_sms", "sourceId": req.ExternalID, "bank": accountName, "group": req.Group, "subgroup": req.Subgroup}
		setUnconfirmedCounterparty(movement, customer)
		if transactionType == "expense" {
			expenseID := "exp-sms-" + req.ExternalID
			expense := map[string]any{"id": expenseID, "date": occurred, "occurredJalali": occurredJalali, "group": req.Group, "subgroup": req.Subgroup, "amount": req.Amount, "description": req.Description, "accountId": resolvedAccountID, "counterparty": counterparty, "reportedCustomer": req.Customer, "source_type": "mobile_sms", "sourceId": req.ExternalID, "bank": accountName}
			state["expenses"] = append([]any{expense}, anyRows(state, "expenses")...)
			movement["sourceExpense"] = expenseID
		}
		state["movements"] = append([]any{movement}, anyRows(state, "movements")...)
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
