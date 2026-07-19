package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

var workspacePermissionFields = map[string][]string{
	"initialData":      {"openingBalances", "accounts", "receivableDocs", "payableDocs", "ownedInventory", "smsGroups", "smsBankSenders"},
	"incomingInvoices": {"incomingInvoices", "movements", "payableDocs", "receivableDocs", "ownedInventory"},
	"yarnOutInvoices":  {"yarnOutInvoices", "ownedInventory"},
	"invoices":         {"invoices", "movements", "receivableDocs", "payableDocs", "ownedInventory"},
	"inventory":        {"ownedInventory"},
	"costs":            {"expenses", "movements", "smsGroups", "mobileTransactions"},
	"receivableDocs":   {"receivableDocs"},
	"payableDocs":      {"payableDocs", "checkbooks"},
	"bankCash":         {"accounts", "movements", "smsBankSenders", "mobileTransactions"},
}

var workspaceReadAllPermissions = map[string]bool{
	"dashboard": true, "financialHealth": true, "reports": true,
	"taxReports": true, "credit": true, "advisor": true,
}

const maxWorkspacePayload = 8 << 20

var errWorkspaceWriteForbidden = errors.New("this access level cannot change financial data")

type workspaceDocument struct {
	State     json.RawMessage `json:"state"`
	Revision  int64           `json:"revision"`
	Checksum  string          `json:"checksum"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type workspaceConflict struct {
	Current workspaceDocument
}

func (e workspaceConflict) Error() string { return "workspace revision conflict" }

func (h *APIHandler) WorkspaceRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		doc, err := loadWorkspace(r, requestctx.CompanyID(r.Context()))
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondJSON(w, http.StatusOK, filterWorkspaceDocument(doc, r.Context()))
	case http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, maxWorkspacePayload)
		var request struct {
			State    json.RawMessage `json:"state"`
			Revision *int64          `json:"revision"`
		}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&request); err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid workspace payload")
			return
		}
		state, checksum, err := validateWorkspaceState(request.State)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		doc, err := saveWorkspace(r, requestctx.CompanyID(r.Context()), requestctx.UserID(r.Context()), request.Revision, state, checksum, writableWorkspaceFields(r.Context()))
		if err != nil {
			if errors.Is(err, errWorkspaceWriteForbidden) {
				RespondError(w, http.StatusForbidden, err.Error())
				return
			}
			var conflict workspaceConflict
			if errors.As(err, &conflict) {
				RespondJSON(w, http.StatusConflict, map[string]any{
					"error": "Workspace was changed by another user", "current": filterWorkspaceDocument(conflict.Current, r.Context()),
				})
				return
			}
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondJSON(w, http.StatusOK, filterWorkspaceDocument(doc, r.Context()))
	default:
		w.Header().Set("Allow", "GET, PUT")
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *APIHandler) GetWorkspaceHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	limit := parseLimit(r, 30)
	rows, err := postgres.WithCompanySession(r.Context(), postgres.DB, companyID, func(q postgres.SessionQueryable) ([]map[string]any, error) {
		result, err := q.QueryContext(r.Context(), `
			SELECT revision, checksum, changed_by, changed_at
			FROM financial_workspace_history
			WHERE company_id=$1
			ORDER BY revision DESC
			LIMIT $2
		`, companyID, limit)
		if err != nil {
			return nil, err
		}
		defer result.Close()
		items := make([]map[string]any, 0, limit)
		for result.Next() {
			var revision int64
			var checksum string
			var changedBy sql.NullInt64
			var changedAt time.Time
			if err := result.Scan(&revision, &checksum, &changedBy, &changedAt); err != nil {
				return nil, err
			}
			items = append(items, map[string]any{
				"revision": revision, "checksum": checksum, "changed_by": nullableInt64(changedBy), "changed_at": changedAt,
			})
		}
		return items, result.Err()
	})
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"rows": rows, "total": len(rows)})
}

func (h *APIHandler) GetWorkspaceSummary(w http.ResponseWriter, r *http.Request) {
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
	RespondJSON(w, http.StatusOK, buildWorkspaceSummary(state, doc.Revision, doc.UpdatedAt))
}

func (h *APIHandler) GetWorkspaceAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	doc, err := loadWorkspace(r, requestctx.CompanyID(r.Context()))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	alerts := buildWorkspaceAlerts(decodeWorkspaceMap(doc.State))
	RespondJSON(w, http.StatusOK, map[string]any{"rows": alerts, "total": len(alerts), "revision": doc.Revision})
}

func loadWorkspace(r *http.Request, companyID int64) (workspaceDocument, error) {
	if postgres.DB == nil {
		return workspaceDocument{}, errors.New("database is not available")
	}
	return postgres.WithCompanySession(r.Context(), postgres.DB, companyID, func(q postgres.SessionQueryable) (workspaceDocument, error) {
		var doc workspaceDocument
		err := q.QueryRowContext(r.Context(), `
			SELECT state, revision, checksum, updated_at
			FROM financial_workspace_states
			WHERE company_id=$1
		`, companyID).Scan(&doc.State, &doc.Revision, &doc.Checksum, &doc.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			doc.State = json.RawMessage(`{}`)
			return doc, nil
		}
		return doc, err
	})
}

func saveWorkspace(r *http.Request, companyID, userID int64, expectedRevision *int64, state json.RawMessage, checksum string, writable map[string]bool) (workspaceDocument, error) {
	if postgres.DB == nil {
		return workspaceDocument{}, errors.New("database is not available")
	}
	var saved workspaceDocument
	err := postgres.WithCompanyTx(r.Context(), postgres.DB, companyID, func(tx *sql.Tx) error {
		var current workspaceDocument
		err := tx.QueryRowContext(r.Context(), `
			SELECT state, revision, checksum, updated_at
			FROM financial_workspace_states
			WHERE company_id=$1
			FOR UPDATE
		`, companyID).Scan(&current.State, &current.Revision, &current.Checksum, &current.UpdatedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if expectedRevision != nil && *expectedRevision != current.Revision {
			return workspaceConflict{Current: current}
		}
		if requestctx.IsPortalAccess(r.Context()) {
			state, checksum, err = mergeWorkspaceState(current.State, state, writable)
			if err != nil {
				return err
			}
		}
		if current.Revision > 0 && current.Checksum == checksum {
			saved = current
			return nil
		}
		nextRevision := current.Revision + 1
		if nextRevision <= 0 {
			nextRevision = 1
		}
		if current.Revision == 0 {
			err = tx.QueryRowContext(r.Context(), `
				INSERT INTO financial_workspace_states (company_id, state, revision, checksum, updated_by)
				VALUES ($1, $2, $3, $4, $5)
				RETURNING state, revision, checksum, updated_at
			`, companyID, state, nextRevision, checksum, nullUserID(userID)).Scan(&saved.State, &saved.Revision, &saved.Checksum, &saved.UpdatedAt)
		} else {
			err = tx.QueryRowContext(r.Context(), `
				UPDATE financial_workspace_states
				SET state=$2, revision=$3, checksum=$4, updated_by=$5, updated_at=CURRENT_TIMESTAMP
				WHERE company_id=$1
				RETURNING state, revision, checksum, updated_at
			`, companyID, state, nextRevision, checksum, nullUserID(userID)).Scan(&saved.State, &saved.Revision, &saved.Checksum, &saved.UpdatedAt)
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO financial_workspace_history (company_id, revision, checksum, state, changed_by)
			VALUES ($1, $2, $3, $4, $5)
		`, companyID, saved.Revision, checksum, state, nullUserID(userID))
		return err
	})
	return saved, err
}

func validateWorkspaceState(raw json.RawMessage) (json.RawMessage, string, error) {
	if len(raw) == 0 {
		return nil, "", errors.New("Workspace state is required")
	}
	var state map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&state); err != nil || state == nil {
		return nil, "", errors.New("Workspace state must be a JSON object")
	}
	for _, key := range []string{"invoices", "incomingInvoices", "yarnOutInvoices", "expenses", "receivableDocs", "payableDocs", "accounts", "movements", "ownedInventory", "openingBalances", "smsGroups", "smsBankSenders", "mobileTransactions"} {
		if value, ok := state[key]; ok {
			if _, ok := value.([]any); !ok {
				return nil, "", fmt.Errorf("Workspace field %s must be an array", key)
			}
		}
	}
	canonical, err := json.Marshal(state)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

func writableWorkspaceFields(ctx context.Context) map[string]bool {
	if !requestctx.IsPortalAccess(ctx) {
		return nil
	}
	allowed := map[string]bool{}
	for _, permission := range requestctx.Permissions(ctx) {
		for _, field := range workspacePermissionFields[permission] {
			allowed[field] = true
		}
	}
	return allowed
}

func mergeWorkspaceState(currentRaw, proposedRaw json.RawMessage, writable map[string]bool) (json.RawMessage, string, error) {
	if len(writable) == 0 {
		return nil, "", errWorkspaceWriteForbidden
	}
	current := decodeWorkspaceMap(currentRaw)
	proposed := decodeWorkspaceMap(proposedRaw)
	for field := range writable {
		if value, exists := proposed[field]; exists {
			current[field] = value
		}
	}
	merged, err := json.Marshal(current)
	if err != nil {
		return nil, "", err
	}
	return validateWorkspaceState(merged)
}

func filterWorkspaceDocument(doc workspaceDocument, ctx context.Context) workspaceDocument {
	if !requestctx.IsPortalAccess(ctx) {
		return doc
	}
	permissions := requestctx.Permissions(ctx)
	for _, permission := range permissions {
		if workspaceReadAllPermissions[permission] {
			return doc
		}
	}
	allowed := writableWorkspaceFields(ctx)
	state := decodeWorkspaceMap(doc.State)
	filtered := make(map[string]any, len(allowed))
	for field := range allowed {
		if value, exists := state[field]; exists {
			filtered[field] = value
		}
	}
	if payload, err := json.Marshal(filtered); err == nil {
		doc.State = payload
	}
	return doc
}

func decodeWorkspaceMap(raw json.RawMessage) map[string]any {
	state := map[string]any{}
	_ = json.Unmarshal(raw, &state)
	return state
}

func buildWorkspaceSummary(state map[string]any, revision int64, updatedAt time.Time) map[string]any {
	invoices := rowsFrom(state, "invoices")
	incoming := rowsFrom(state, "incomingInvoices")
	expenses := rowsFrom(state, "expenses")
	receivables := rowsFrom(state, "receivableDocs")
	payables := rowsFrom(state, "payableDocs")
	accounts := rowsFrom(state, "accounts")
	movements := rowsFrom(state, "movements")
	totalSales := sumField(invoices, "total")
	totalPurchases := sumField(incoming, "amount")
	totalExpenses := sumField(expenses, "amount")
	openReceivables := sumFiltered(receivables, "amount", func(row map[string]any) bool { return !statusClosed(row, "cleared") })
	openPayables := sumFiltered(payables, "amount", func(row map[string]any) bool { return !statusClosed(row, "paid") })
	cashBalance := 0.0
	for _, account := range accounts {
		balance := number(account["opening"])
		id := stringValue(account["id"])
		for _, movement := range movements {
			if stringValue(movement["accountId"]) != id {
				continue
			}
			amount := number(movement["amount"])
			if stringValue(movement["direction"]) == "out" {
				amount = -amount
			}
			balance += amount
		}
		cashBalance += balance
	}
	return map[string]any{
		"revision": revision, "updated_at": updatedAt,
		"total_sales": totalSales, "total_purchases": totalPurchases, "total_expenses": totalExpenses,
		"gross_margin":     totalSales - totalPurchases - totalExpenses,
		"open_receivables": openReceivables, "open_payables": openPayables, "cash_balance": cashBalance,
		"invoice_count": len(invoices), "incoming_invoice_count": len(incoming), "expense_count": len(expenses),
	}
}

func buildWorkspaceAlerts(state map[string]any) []map[string]any {
	alerts := make([]map[string]any, 0)
	now := time.Now()
	for _, row := range rowsFrom(state, "invoices") {
		total := number(row["total"])
		settled := 0.0
		for _, payment := range rowsFrom(row, "payments") {
			settled += number(payment["amount"])
		}
		if total > 0 && absolute(total-settled) > 1 {
			alerts = append(alerts, alert("warning", "فاکتور تسویه‌نشده", fmt.Sprintf("فاکتور %s دارای مانده %s است.", stringValue(row["number"]), formatAmount(total-settled))))
		}
		if strings.TrimSpace(stringValue(row["customer"])) == "" {
			alerts = append(alerts, alert("critical", "طرف حساب ناقص", "یک فاکتور فروش بدون طرف حساب ثبت شده است."))
		}
	}
	checkNumbers := map[string]int{}
	for _, key := range []string{"receivableDocs", "payableDocs"} {
		for _, row := range rowsFrom(state, key) {
			numberValue := strings.TrimSpace(stringValue(row["checkNo"]))
			if numberValue != "" {
				checkNumbers[key+":"+numberValue]++
			}
			if statusClosed(row, "cleared") || statusClosed(row, "paid") {
				continue
			}
			if due, ok := parseDate(stringValue(row["dueDate"])); ok && due.Before(now) {
				alerts = append(alerts, alert("critical", "سند سررسید گذشته", fmt.Sprintf("سند %s به مبلغ %s سررسید شده است.", numberValue, formatAmount(number(row["amount"])))))
			}
		}
	}
	keys := make([]string, 0, len(checkNumbers))
	for key, count := range checkNumbers {
		if count > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.SplitN(key, ":", 2)
		alerts = append(alerts, alert("warning", "شماره سند تکراری", fmt.Sprintf("شماره %s در %d رکورد تکرار شده است.", parts[1], checkNumbers[key])))
	}
	for _, account := range rowsFrom(state, "accounts") {
		balance := number(account["opening"])
		id := stringValue(account["id"])
		for _, movement := range rowsFrom(state, "movements") {
			if stringValue(movement["accountId"]) == id {
				amount := number(movement["amount"])
				if stringValue(movement["direction"]) == "out" {
					amount = -amount
				}
				balance += amount
			}
		}
		if balance < 0 {
			alerts = append(alerts, alert("critical", "مانده منفی حساب", fmt.Sprintf("حساب %s دارای مانده منفی %s است.", stringValue(account["name"]), formatAmount(-balance))))
		}
	}
	return alerts
}

func rowsFrom(container map[string]any, key string) []map[string]any {
	values, _ := container[key].([]any)
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if row, ok := value.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func sumField(rows []map[string]any, key string) float64 {
	return sumFiltered(rows, key, func(map[string]any) bool { return true })
}

func sumFiltered(rows []map[string]any, key string, include func(map[string]any) bool) float64 {
	total := 0.0
	for _, row := range rows {
		if include(row) {
			total += number(row[key])
		}
	}
	return total
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case json.Number:
		n, _ := typed.Float64()
		return n
	case string:
		var n float64
		_, _ = fmt.Sscan(strings.ReplaceAll(typed, ",", ""), &n)
		return n
	default:
		return 0
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func statusClosed(row map[string]any, closed string) bool {
	return strings.EqualFold(strings.TrimSpace(stringValue(row["status"])), closed)
}

func parseDate(value string) (time.Time, bool) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return parsed, err == nil
}

func alert(severity, title, message string) map[string]any {
	return map[string]any{"severity": severity, "title": title, "message": message}
}

func formatAmount(value float64) string { return fmt.Sprintf("%.0f تومان", value) }

func absolute(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func nullableInt64(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func nullUserID(userID int64) any {
	if userID > 0 {
		return userID
	}
	return nil
}
