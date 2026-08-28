package handler

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func (h *APIHandler) GetWorkspaceAlertsAccurate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	doc, err := loadWorkspace(r, requestctx.CompanyID(r.Context()))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	alerts := buildWorkspaceAlertsAccurate(decodeWorkspaceMap(doc.State), time.Now())
	RespondJSON(w, http.StatusOK, map[string]any{"rows": alerts, "total": len(alerts), "revision": doc.Revision})
}

func buildWorkspaceAlertsAccurate(state map[string]any, now time.Time) []map[string]any {
	alerts := make([]map[string]any, 0)
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
	appendCheckAlerts := func(register string, rows []map[string]any) {
		for _, row := range rows {
			checkNo := strings.TrimSpace(stringValue(row["checkNo"]))
			if checkNo != "" {
				checkNumbers[register+":"+checkNo]++
			}
			status := strings.ToLower(strings.TrimSpace(stringValue(row["status"])))
			if register == "receivableDocs" && (status == "cleared" || status == "assigned") {
				continue
			}
			if register == "payableDocs" && status == "paid" {
				continue
			}
			if status == "bounced" || status == "returned" {
				kind := "سند دریافتی"
				if register == "payableDocs" {
					kind = "سند پرداختی"
				}
				alerts = append(alerts, alert("critical", "سند برگشتی", fmt.Sprintf("%s %s به مبلغ %s برگشتی/مرجوع ثبت شده است.", kind, checkNo, formatAmount(number(row["amount"])))))
				continue
			}
			if due, ok := parseDate(stringValue(row["dueDate"])); ok && due.Before(now) {
				alerts = append(alerts, alert("critical", "سند سررسید گذشته", fmt.Sprintf("سند %s به مبلغ %s سررسید شده است.", checkNo, formatAmount(number(row["amount"])))))
			}
		}
	}
	appendCheckAlerts("receivableDocs", rowsFrom(state, "receivableDocs"))
	appendCheckAlerts("payableDocs", rowsFrom(state, "payableDocs"))

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

	movements := rowsFrom(state, "movements")
	for _, account := range rowsFrom(state, "accounts") {
		balance := workspaceAccountBalance(account, movements)
		if balance < 0 {
			alerts = append(alerts, alert("critical", "مانده منفی حساب", fmt.Sprintf("حساب %s دارای مانده منفی %s است.", stringValue(account["name"]), formatAmount(-balance))))
		}
	}
	return alerts
}
