package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

type supervisorFinding struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	Page      string `json:"page"`
	Reference string `json:"reference"`
	Evidence  string `json:"-"`
}

// Findings describe broken relations, never inferred transaction classifications.
// Comparing evidence permits unrelated work on legacy workspaces without
// allowing changes to silently break a previously valid relation.
func supervisorStateFindings(state map[string]any) []supervisorFinding {
	issues := []supervisorFinding{}
	add := func(code, severity, page, ref, detail string, evidence any) {
		raw, _ := json.Marshal(evidence)
		sum := sha256.Sum256(raw)
		issues = append(issues, supervisorFinding{
			ID: code + ":" + page + ":" + ref, Severity: severity, Category: "کنترل ارتباط و اثر مالی",
			Title: detail, Detail: detail, Page: page, Reference: ref, Evidence: hex.EncodeToString(sum[:]),
		})
	}
	accounts := indexSupervisorRows(rowsFrom(state, "accounts"), "id")
	expenses := indexSupervisorRows(rowsFrom(state, "expenses"), "id", "sourceId")
	incoming := indexSupervisorRows(rowsFrom(state, "incomingInvoices"), "id", "sourceId")
	sales := indexSupervisorRows(rowsFrom(state, "invoices"), "number", "id")
	movements := rowsFrom(state, "movements")
	normalized := func(row map[string]any) map[string]any {
		result := cloneOperationalSyncMap(row)
		for _, key := range []string{"syncedAt", "_accountingProcessedAt", "_accountingProcessedBy", "reconciled", "reconciledAt"} {
			delete(result, key)
		}
		return result
	}
	for _, field := range []string{"accounts", "expenses", "movements", "incomingInvoices", "invoices", "yarnOutInvoices", "ownedInventory", "receivableDocs", "payableDocs", "mobileTransactions"} {
		seen := map[string]int{}
		sources := map[string]int{}
		for i, row := range rowsFrom(state, field) {
			id := accountingRowIdentity(field, row, i)
			seen[id]++
			if source := firstText(row, "source_type"); source != "" && source != "manual" && firstText(row, "sourceId") != "" {
				sources[source+":"+firstText(row, "sourceId")]++
			}
		}
		for key, count := range seen {
			if count > 1 {
				add("duplicate", "critical", supervisorPage(field), key, "شناسه تکراری؛ امکان اثر مالی چندباره وجود دارد", count)
			}
		}
		for key, count := range sources {
			if count > 1 {
				add("duplicate-source", "critical", supervisorPage(field), key, "یک رویداد مبدأ چندبار در این دفتر ثبت شده است", count)
			}
		}
	}
	for id, expense := range expenses {
		if firstText(expense, "source_type") == "operational_expense" {
			verified, _ := expense["verifiedPayment"].(map[string]any)
			if firstText(verified, "accountId") != firstText(expense, "accountId") || firstText(verified, "date") != firstText(expense, "date") || !amountsEqual(number(verified["amount"]), number(expense["amount"])) {
				add("payment-verification", "warning", "costs", id, "مبدأ عملیاتی بانک پرداخت‌کننده را مشخص نمی‌کند؛ حساب و مبلغ هزینه را در فرم مالی بررسی و ذخیره کنید", firstText(expense, "accountId"))
			}
		}
		linked := filterSupervisorMovements(movements, "sourceExpense", id)
		if accounts[firstText(expense, "accountId")] == nil {
			add("account", "critical", "costs", id, "حساب پرداخت هزینه معتبر نیست", firstText(expense, "accountId"))
		}
		if len(linked) != 1 {
			add("expense-link", "critical", "costs", id, "هزینه باید دقیقاً یک گردش بانک/صندوق داشته باشد", len(linked))
		} else {
			m := linked[0]
			if !amountsEqual(number(m["amount"]), number(expense["amount"])) || firstText(m, "accountId") != firstText(expense, "accountId") || firstText(m, "date") != firstText(expense, "date") || firstText(m, "direction") != "out" || firstText(m, "transactionType") != "expense" {
				add("expense-effect", "critical", "costs", id, "مبلغ، تاریخ، ماهیت یا حساب گردش با هزینه یکسان نیست", []any{normalized(expense), normalized(m)})
			}
		}
		if firstText(expense, "group", "title") == "" || firstText(expense, "subgroup") == "" {
			add("category", "warning", "costs", id, "گروه یا زیرگروه هزینه تکمیل نشده است", []string{firstText(expense, "group", "title"), firstText(expense, "subgroup")})
		}
	}
	for _, config := range []struct {
		field, link, page, direction string
		items                        map[string]map[string]any
	}{
		{"incomingInvoices", "sourceIncomingInvoice", "incomingInvoices", "out", incoming},
		{"invoices", "sourceInvoice", "invoices", "in", sales},
	} {
		for id, invoice := range config.items {
			linked := filterSupervisorMovements(movements, config.link, id)
			payments := rowsFrom(invoice, "payments")
			stableLinked := []map[string]any{}
			for _, m := range linked {
				stableLinked = append(stableLinked, normalized(m))
			}
			evidence := []any{normalized(invoice), stableLinked}
			accountSet := map[string]bool{}
			for key := range accounts {
				accountSet[key] = true
			}
			if err := validateSupervisorCashPayments(invoice, movements, accountSet, config.link, id, "فاکتور"); err != nil {
				add("cash-links", "critical", config.page, id, err.Error(), evidence)
			}
			for _, m := range linked {
				party := firstText(m, "payer", "customer")
				if firstText(m, "direction") != config.direction || party != firstText(invoice, "customer") || !boolValue(m["counterpartyConfirmed"]) {
					add("cash-party", "critical", config.page, id, "جهت پرداخت یا طرف حساب گردش با فاکتور یکسان نیست", evidence)
					break
				}
			}
			if boolValue(invoice["nonFinancial"]) && (len(payments) > 0 || len(linked) > 0) {
				add("consignment", "critical", config.page, id, "فاکتور امانی بدون اثر ریالی نباید تسویه بانکی یا بدهی ایجاد کند", evidence)
			}
			if config.field == "incomingInvoices" {
				inventoryType := firstText(invoice, "inventoryType")
				if inventoryType == "yarn" || inventoryType == "fabric" || inventoryType == "spare_part" {
					stocks := filterSupervisorMovements(rowsFrom(state, "ownedInventory"), config.link, id)
					// Count and quantities detect duplicate valuation, even if amounts match.
					if len(stocks) != 1 {
						add("stock-link", "critical", config.page, id, "فاکتور ورود باید دقیقاً یک ردیف موجودی مرتبط داشته باشد", len(stocks))
					} else if math.Abs(number(stocks[0]["quantity"])-number(invoice["quantity"])) > 0.000001 || firstText(stocks[0], "itemName") != firstText(invoice, "itemName") || firstText(stocks[0], "customer") != firstText(invoice, "customer") {
						add("stock-effect", "critical", config.page, id, "مقدار، کالا یا مالک موجودی با فاکتور ورود متفاوت است", []any{normalized(invoice), normalized(stocks[0])})
					}
				}
			}
			docField := "receivableDocs"
			if config.field == "incomingInvoices" {
				docField = "payableDocs"
			}
			docs := filterSupervisorMovements(rowsFrom(state, docField), config.link, id)
			expected := map[string]float64{}
			actual := map[string]float64{}
			for _, p := range payments {
				if firstText(p, "type") == "check" {
					expected[firstText(p, "checkNo")+"|"+firstText(p, "dueDate")] += number(p["amount"])
				}
			}
			for _, d := range docs {
				actual[firstText(d, "checkNo")+"|"+firstText(d, "dueDate")] += number(d["amount"])
				if firstText(d, "customer") != firstText(invoice, "customer") {
					add("check-party", "critical", docField, firstText(d, "id"), "طرف حساب چک با فاکتور مرتبط یکسان نیست", normalized(d))
				}
			}
			rawExpected, _ := json.Marshal(expected)
			rawActual, _ := json.Marshal(actual)
			if string(rawExpected) != string(rawActual) {
				add("check-links", "critical", config.page, id, "چک‌های ثبت‌شده با ردیف‌های تسویه فاکتور تطبیق ندارند", []any{expected, actual})
			}
		}
	}
	for _, m := range movements {
		id := firstText(m, "id", "trackingNo")
		if accounts[firstText(m, "accountId")] == nil {
			add("account", "critical", "bankCash", id, "گردش به بانک/صندوق معتبر متصل نیست", firstText(m, "accountId"))
		}
		typ, dir := firstText(m, "transactionType"), firstText(m, "direction")
		if dir != "in" && dir != "out" || number(m["amount"]) <= 0 || math.IsNaN(number(m["amount"])) || math.IsInf(number(m["amount"]), 0) {
			add("movement-value", "critical", "bankCash", id, "مبلغ یا جهت گردش معتبر نیست", normalized(m))
		}
		if typ == "transfer" && (accounts[firstText(m, "counterAccountId")] == nil || firstText(m, "counterAccountId") == firstText(m, "accountId")) {
			add("transfer", "critical", "bankCash", id, "حساب مقصد انتقال معتبر نیست یا با مبدأ برابر است", normalized(m))
		}
		if (typ == "customer_receipt" && dir != "in") || (typ == "supplier_payment" && dir != "out") {
			add("party-direction", "critical", "bankCash", id, "جهت گردش با ماهیت دریافت/پرداخت طرف حساب یکسان نیست", normalized(m))
		}
		if (typ == "customer_receipt" || typ == "supplier_payment") && (!boolValue(m["counterpartyConfirmed"]) || firstText(m, "payer", "customer") == "") {
			add("party-unconfirmed", "warning", "bankCash", id, "طرف حساب هنوز تأیید نشده؛ اثر آن در مانده اشخاص باید بررسی شود", normalized(m))
		}
		for _, link := range []struct {
			field   string
			parents map[string]map[string]any
		}{
			{"sourceExpense", expenses}, {"sourceIncomingInvoice", incoming}, {"sourceInvoice", sales},
		} {
			if source := firstText(m, link.field); source != "" && link.parents[source] == nil {
				add("orphan-"+link.field, "critical", "bankCash", id, "گردش بدون سند مبدأ باقی مانده است", source)
			}
		}
	}
	for _, config := range []struct {
		field, link string
		parents     map[string]map[string]any
	}{
		{"ownedInventory", "sourceIncomingInvoice", incoming}, {"payableDocs", "sourceIncomingInvoice", incoming}, {"receivableDocs", "sourceInvoice", sales},
	} {
		for _, row := range rowsFrom(state, config.field) {
			if source := firstText(row, config.link); source != "" && config.parents[source] == nil {
				add("orphan", "critical", supervisorPage(config.field), firstText(row, "id"), "اثر موجودی یا چک بدون فاکتور مبدأ باقی مانده است", source)
			}
		}
	}
	for _, mobile := range rowsFrom(state, "mobileTransactions") {
		id := firstText(mobile, "externalId", "sourceId")
		if id == "" {
			continue
		}
		linked := filterSupervisorMovements(movements, "sourceMobileTransaction", id)
		if len(linked) != 1 {
			add("mobile-link", "critical", "mobileApp", id, "تراکنش حسابیار باید دقیقاً یک گردش مالی متناظر داشته باشد", len(linked))
			continue
		}
		m := linked[0]
		if !amountsEqual(number(m["amount"]), number(mobile["amount"])) || firstText(m, "direction") != firstText(mobile, "direction") || firstText(m, "accountId") != firstText(mobile, "accountId") {
			add("mobile-effect", "critical", "mobileApp", id, "مبلغ، جهت یا بانک تراکنش حسابیار با گردش مالی یکسان نیست", []any{normalized(mobile), normalized(m)})
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].ID < issues[j].ID })
	return issues
}

func supervisorPage(field string) string {
	switch field {
	case "expenses":
		return "costs"
	case "movements", "accounts":
		return "bankCash"
	case "ownedInventory":
		return "inventory"
	case "mobileTransactions":
		return "mobileApp"
	default:
		return field
	}
}

// Both the preview and the committed journal use deriveWorkspaceLedger. There
// is no second accounting model maintained by the assistant.
func supervisorLedgerDelta(oldState, newState map[string]any) ([]ledgerLine, error) {
	oldEntries, err := deriveWorkspaceLedger(oldState)
	if err != nil {
		return nil, err
	}
	newEntries, err := deriveWorkspaceLedger(newState)
	if err != nil {
		return nil, err
	}
	totals := map[string]ledgerLine{}
	accumulate := func(entries map[string]ledgerEntry, sign float64) {
		for _, entry := range entries {
			for _, line := range entry.Lines {
				key := line.AccountCode + "|" + line.Party
				total := totals[key]
				total.AccountCode = line.AccountCode
				total.AccountName = line.AccountName
				total.AccountType = line.AccountType
				total.Party = line.Party
				total.Debit += sign * line.Debit
				total.Credit += sign * line.Credit
				totals[key] = total
			}
		}
	}
	accumulate(oldEntries, -1)
	accumulate(newEntries, 1)
	result := []ledgerLine{}
	for _, line := range totals {
		delta := moneyRound(line.Debit - line.Credit)
		line.Debit = math.Max(delta, 0)
		line.Credit = math.Max(-delta, 0)
		if delta != 0 {
			result = append(result, line)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].AccountCode+result[i].Party < result[j].AccountCode+result[j].Party
	})
	var debit, credit float64
	for _, line := range result {
		debit += line.Debit
		credit += line.Credit
	}
	if !amountsEqual(debit, credit) {
		return nil, fmt.Errorf("اثر پیش‌نمایش تراز نیست")
	}
	return result, nil
}
