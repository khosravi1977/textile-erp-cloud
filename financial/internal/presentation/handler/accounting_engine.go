package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type ledgerLine struct {
	AccountCode string  `json:"accountCode"`
	AccountName string  `json:"accountName"`
	AccountType string  `json:"accountType"`
	Party       string  `json:"party,omitempty"`
	Debit       float64 `json:"debit"`
	Credit      float64 `json:"credit"`
	Description string  `json:"description,omitempty"`
}

type ledgerEntry struct {
	Key         string       `json:"key"`
	Date        string       `json:"date"`
	Description string       `json:"description"`
	SourceType  string       `json:"sourceType"`
	Party       string       `json:"party,omitempty"`
	Lines       []ledgerLine `json:"lines"`
}

type glAccount struct {
	Code string
	Name string
	Type string
}

var canonicalGL = map[string]glAccount{
	"cash":            {"1110", "صندوق", "Asset"},
	"bank":            {"1120", "بانک", "Asset"},
	"receivable":      {"1200", "حساب‌های دریافتنی", "Asset"},
	"checkReceivable": {"1210", "اسناد دریافتنی", "Asset"},
	"inventory":       {"1300", "موجودی مواد و کالا", "Asset"},
	"yarnInventory":   {"1310", "موجودی نخ", "Asset"},
	"fabricInventory": {"1320", "موجودی پارچه", "Asset"},
	"spareInventory":  {"1330", "موجودی قطعات", "Asset"},
	"inputVAT":        {"1410", "مالیات بر ارزش افزوده خرید", "Asset"},
	"payable":         {"2100", "حساب‌های پرداختنی", "Liability"},
	"checkPayable":    {"2110", "اسناد پرداختنی", "Liability"},
	"clearing":        {"2190", "حساب واسط دریافت و پرداخت", "Liability"},
	"outputVAT":       {"2310", "مالیات بر ارزش افزوده فروش", "Liability"},
	"opening":         {"3100", "سرمایه و مانده افتتاحیه", "Equity"},
	"sales":           {"4200", "درآمد فروش", "Income"},
	"yarnSales":       {"4300", "درآمد فروش نخ", "Income"},
	"otherIncome":     {"4900", "سایر درآمدها", "Income"},
	"cogs":            {"5300", "بهای تمام‌شده کالای فروش‌رفته", "Expense"},
	"expense":         {"5900", "هزینه‌های عملیاتی", "Expense"},
}

func validateWorkspaceAccounting(state map[string]any) error {
	seen := map[string]bool{}
	for _, row := range rowsFrom(state, "invoices") {
		id := firstText(row, "id", "number")
		if err := requireBusinessRow(row, id, "فاکتور فروش", "total", "customer", "date"); err != nil {
			return err
		}
		if strings.TrimSpace(stringValue(row["item"])) == "" {
			return fmt.Errorf("فاکتور فروش %s فاقد کالا است", id)
		}
		if vat := number(row["taxAmount"]); vat < 0 || vat > number(row["total"]) {
			return fmt.Errorf("مبلغ مالیات فاکتور فروش %s معتبر نیست", id)
		}
		if err := validatePayments(id, "فاکتور فروش", number(row["total"]), rowsFrom(row, "payments")); err != nil {
			return err
		}
		if key := "sale:" + id; seen[key] {
			return fmt.Errorf("فاکتور فروش تکراری است: %s", id)
		} else {
			seen[key] = true
		}
	}
	for _, row := range rowsFrom(state, "incomingInvoices") {
		id := firstText(row, "id", "sourceId")
		if err := requireBusinessRow(row, id, "فاکتور خرید/ورود", "amount", "customer", "date"); err != nil {
			return err
		}
		if !boolValue(row["nonFinancial"]) {
			if strings.TrimSpace(stringValue(row["itemName"])) == "" {
				return fmt.Errorf("فاکتور خرید/ورود %s فاقد کالا یا شرح خدمت است", id)
			}
			if vat := number(row["taxAmount"]); vat < 0 || vat > number(row["amount"]) {
				return fmt.Errorf("مبلغ مالیات فاکتور خرید/ورود %s معتبر نیست", id)
			}
			if err := validatePayments(id, "فاکتور خرید/ورود", number(row["amount"]), rowsFrom(row, "payments")); err != nil {
				return err
			}
		}
		if key := "purchase:" + id; seen[key] {
			return fmt.Errorf("فاکتور خرید/ورود تکراری است: %s", id)
		} else {
			seen[key] = true
		}
	}
	for _, row := range rowsFrom(state, "yarnOutInvoices") {
		mode := stringValue(row["outMode"])
		if mode != "sale" && mode != "barter" {
			continue
		}
		id := firstText(row, "id", "sourceId")
		if err := requireBusinessRow(row, id, "خروج مالی نخ", "amount", "customer", "itemName", "date"); err != nil {
			return err
		}
		if number(row["quantity"]) <= 0 {
			return fmt.Errorf("مقدار خروج نخ %s باید بزرگ‌تر از صفر باشد", id)
		}
		if stringValue(row["stockType"]) != "amanat" && number(row["costAmount"]) <= 0 {
			return fmt.Errorf("بهای تمام‌شده خروج نخ ملکی %s الزامی است", id)
		}
		if key := "yarn-out:" + id; seen[key] {
			return fmt.Errorf("خروج نخ تکراری است: %s", id)
		} else {
			seen[key] = true
		}
	}
	for _, row := range rowsFrom(state, "expenses") {
		id := firstText(row, "id", "sourceId")
		if err := requireBusinessRow(row, id, "هزینه", "amount", "date"); err != nil {
			return err
		}
		if strings.TrimSpace(stringValue(row["accountId"])) == "" {
			return fmt.Errorf("برای هزینه %s حساب پرداخت انتخاب نشده است", id)
		}
	}
	for _, key := range []string{"receivableDocs", "payableDocs"} {
		checkNumbers := map[string]bool{}
		for _, row := range rowsFrom(state, key) {
			id := firstText(row, "id", "checkNo")
			if number(row["amount"]) <= 0 {
				return fmt.Errorf("مبلغ سند %s باید بزرگ‌تر از صفر باشد", id)
			}
			checkNo := strings.TrimSpace(stringValue(row["checkNo"]))
			if checkNo == "" || strings.TrimSpace(stringValue(row["dueDate"])) == "" {
				return fmt.Errorf("شماره و تاریخ سررسید سند %s الزامی است", id)
			}
			if checkNumbers[checkNo] {
				return fmt.Errorf("شماره چک %s در یک دفتر تکراری است", checkNo)
			}
			checkNumbers[checkNo] = true
		}
	}
	for _, row := range rowsFrom(state, "movements") {
		if hasAny(row, "sourceInvoice", "sourceIncomingInvoice", "sourceExpense", "sourceMobileTransaction") {
			continue
		}
		id := firstText(row, "id", "trackingNo")
		if number(row["amount"]) <= 0 || strings.TrimSpace(stringValue(row["accountId"])) == "" {
			return fmt.Errorf("گردش نقدی %s مبلغ یا حساب معتبر ندارد", id)
		}
		direction := stringValue(row["direction"])
		if direction != "in" && direction != "out" {
			return fmt.Errorf("جهت گردش نقدی %s معتبر نیست", id)
		}
		transactionType := stringValue(row["transactionType"])
		if strings.TrimSpace(transactionType) == "" {
			return fmt.Errorf("ماهیت حسابداری گردش نقدی %s مشخص نشده است", id)
		}
		validMovementTypes := map[string]bool{"customer_receipt": true, "supplier_payment": true, "transfer": true, "expense": true, "other_income": true, "capital": true}
		if !validMovementTypes[transactionType] {
			return fmt.Errorf("ماهیت گردش نقدی %s معتبر نیست", id)
		}
		if transactionType == "transfer" && (strings.TrimSpace(stringValue(row["counterAccountId"])) == "" || stringValue(row["counterAccountId"]) == stringValue(row["accountId"])) {
			return fmt.Errorf("حساب مقصد انتقال %s معتبر نیست", id)
		}
		if (transactionType == "customer_receipt" || transactionType == "supplier_payment") && strings.TrimSpace(firstText(row, "payer", "customer")) == "" {
			return fmt.Errorf("طرف حساب گردش نقدی %s الزامی است", id)
		}
	}
	for _, row := range rowsFrom(state, "journalEntries") {
		id := firstText(row, "id", "number")
		lines := rowsFrom(row, "lines")
		if len(lines) < 2 {
			return fmt.Errorf("سند دستی %s حداقل به دو آرتیکل نیاز دارد", id)
		}
		debit, credit := 0.0, 0.0
		for _, line := range lines {
			d, c := number(line["debit"]), number(line["credit"])
			if strings.TrimSpace(stringValue(line["accountCode"])) == "" || d < 0 || c < 0 || (d > 0) == (c > 0) {
				return fmt.Errorf("آرتیکل نامعتبر در سند دستی %s", id)
			}
			debit += d
			credit += c
		}
		if !amountsEqual(debit, credit) {
			return fmt.Errorf("سند دستی %s تراز نیست؛ بدهکار %.0f و بستانکار %.0f", id, debit, credit)
		}
	}
	if _, err := deriveWorkspaceLedger(state); err != nil {
		return err
	}
	return nil
}

// validateWorkspaceAccountingChanges keeps historical workspace rows readable while
// applying the strict accounting rules to every row that is added or modified.
// This is important for production upgrades: old incomplete rows are never deleted
// or rewritten merely because an unrelated module is being saved.
func validateWorkspaceAccountingChanges(oldState, newState map[string]any) error {
	changed := map[string]any{}
	for _, key := range []string{"invoices", "incomingInvoices", "yarnOutInvoices", "expenses", "receivableDocs", "payableDocs", "movements", "journalEntries"} {
		oldRows := map[string][]byte{}
		for index, row := range rowsFrom(oldState, key) {
			identity := accountingRowIdentity(key, row, index)
			encoded, _ := json.Marshal(row)
			oldRows[identity] = encoded
		}
		rows := make([]any, 0)
		for index, row := range rowsFrom(newState, key) {
			identity := accountingRowIdentity(key, row, index)
			encoded, _ := json.Marshal(row)
			if previous, exists := oldRows[identity]; !exists || string(previous) != string(encoded) {
				rows = append(rows, row)
			}
		}
		if len(rows) > 0 {
			changed[key] = rows
		}
	}
	if err := validateAccountingUniqueness(newState); err != nil {
		return err
	}
	return validateWorkspaceAccounting(changed)
}

func accountingRowIdentity(key string, row map[string]any, index int) string {
	identity := firstText(row, "id", "number", "sourceId", "checkNo", "trackingNo")
	if identity == "" {
		return fmt.Sprintf("%s:index:%d", key, index)
	}
	return key + ":" + identity
}

func validateAccountingUniqueness(state map[string]any) error {
	for _, key := range []string{"invoices", "incomingInvoices", "yarnOutInvoices"} {
		seen := map[string]bool{}
		for index, row := range rowsFrom(state, key) {
			identity := accountingRowIdentity(key, row, index)
			if seen[identity] {
				return fmt.Errorf("شناسه تکراری در %s: %s", key, identity)
			}
			seen[identity] = true
		}
	}
	for _, key := range []string{"receivableDocs", "payableDocs"} {
		seen := map[string]bool{}
		for _, row := range rowsFrom(state, key) {
			checkNo := strings.TrimSpace(stringValue(row["checkNo"]))
			if checkNo == "" {
				continue
			}
			if seen[checkNo] {
				return fmt.Errorf("شماره چک %s در یک دفتر تکراری است", checkNo)
			}
			seen[checkNo] = true
		}
	}
	return nil
}

func requireBusinessRow(row map[string]any, id, label, amountKey string, textKeys ...string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s فاقد شناسه است", label)
	}
	if number(row[amountKey]) <= 0 {
		return fmt.Errorf("مبلغ %s %s باید بزرگ‌تر از صفر باشد", label, id)
	}
	for _, key := range textKeys {
		if strings.TrimSpace(stringValue(row[key])) == "" {
			return fmt.Errorf("فیلد %s در %s %s الزامی است", key, label, id)
		}
	}
	return nil
}

func validatePayments(id, label string, total float64, payments []map[string]any) error {
	paid := 0.0
	for _, payment := range payments {
		amount := number(payment["amount"])
		if amount <= 0 {
			continue
		}
		paid += amount
		switch stringValue(payment["type"]) {
		case "cash":
			if strings.TrimSpace(stringValue(payment["accountId"])) == "" {
				return fmt.Errorf("در %s %s برای پرداخت نقدی حساب بانک/صندوق انتخاب نشده است", label, id)
			}
		case "check":
			if strings.TrimSpace(stringValue(payment["checkNo"])) == "" || strings.TrimSpace(stringValue(payment["dueDate"])) == "" {
				return fmt.Errorf("در %s %s شماره و سررسید چک الزامی است", label, id)
			}
		case "assign_receivable":
			if strings.TrimSpace(stringValue(payment["docId"])) == "" {
				return fmt.Errorf("در %s %s چک دریافتی قابل واگذاری انتخاب نشده است", label, id)
			}
		case "credit", "barter_yarn", "barter_fabric":
		default:
			return fmt.Errorf("نوع تسویه %s در %s %s معتبر نیست", stringValue(payment["type"]), label, id)
		}
	}
	if !amountsEqual(total, paid) {
		return fmt.Errorf("تسویه %s %s کامل نیست؛ مبلغ سند %.0f و جمع روش‌های تسویه %.0f تومان است", label, id, total, paid)
	}
	return nil
}

func deriveWorkspaceLedger(state map[string]any) (map[string]ledgerEntry, error) {
	entries := map[string]ledgerEntry{}
	accountMap := workspaceCashAccounts(state)
	add := func(entry ledgerEntry) error {
		if entry.Key == "" || len(entry.Lines) < 2 {
			return fmt.Errorf("سند حسابداری %s ناقص است", entry.Key)
		}
		debit, credit := 0.0, 0.0
		for i := range entry.Lines {
			entry.Lines[i].Debit = moneyRound(entry.Lines[i].Debit)
			entry.Lines[i].Credit = moneyRound(entry.Lines[i].Credit)
			debit += entry.Lines[i].Debit
			credit += entry.Lines[i].Credit
		}
		if !amountsEqual(debit, credit) {
			return fmt.Errorf("سند حسابداری %s تراز نیست", entry.Key)
		}
		entries[entry.Key] = entry
		return nil
	}
	for _, row := range rowsFrom(state, "accounts") {
		amount := number(row["opening"])
		if amount == 0 {
			continue
		}
		account := accountMap[stringValue(row["id"])]
		lines := debitCredit(account, canonicalGL["opening"], amount, "مانده افتتاحیه حساب", "")
		if err := add(ledgerEntry{Key: "account-opening:" + stringValue(row["id"]), Date: firstText(row, "date", "createdAt"), Description: "مانده افتتاحیه " + stringValue(row["name"]), SourceType: "AccountOpening", Lines: lines}); err != nil {
			return nil, err
		}
	}
	for _, row := range rowsFrom(state, "openingBalances") {
		amount := number(row["amount"])
		if amount <= 0 {
			continue
		}
		party, id := stringValue(row["customer"]), firstText(row, "id", "customer")
		left, right := canonicalGL["receivable"], canonicalGL["opening"]
		if stringValue(row["type"]) == "payable" {
			left, right = canonicalGL["opening"], canonicalGL["payable"]
		}
		lines := []ledgerLine{debitLine(left, amount, party, "مانده افتتاحیه"), creditLine(right, amount, party, "مانده افتتاحیه")}
		if err := add(ledgerEntry{Key: "party-opening:" + id, Date: stringValue(row["date"]), Description: "مانده افتتاحیه طرف حساب " + party, SourceType: "PartyOpening", Party: party, Lines: lines}); err != nil {
			return nil, err
		}
	}
	for _, row := range rowsFrom(state, "invoices") {
		id, party, total := firstText(row, "id", "number"), stringValue(row["customer"]), number(row["total"])
		if total <= 0 {
			continue
		}
		vat := math.Max(0, number(row["taxAmount"]))
		revenue := math.Max(0, total-vat)
		lines := []ledgerLine{debitLine(canonicalGL["receivable"], total, party, "ثبت فروش")}
		if revenue > 0 {
			lines = append(lines, creditLine(canonicalGL["sales"], revenue, party, "درآمد فروش"))
		}
		if vat > 0 {
			lines = append(lines, creditLine(canonicalGL["outputVAT"], vat, party, "مالیات فروش"))
		}
		if err := add(ledgerEntry{Key: "sale:" + id, Date: stringValue(row["date"]), Description: "فاکتور فروش " + firstText(row, "number", "id"), SourceType: "SalesInvoice", Party: party, Lines: lines}); err != nil {
			return nil, err
		}
		if cost := number(row["costAmount"]); cost > 0 {
			if err := add(ledgerEntry{Key: "sale-cogs:" + id, Date: stringValue(row["date"]), Description: "بهای تمام‌شده فروش " + firstText(row, "number", "id"), SourceType: "CostOfGoodsSold", Party: party, Lines: []ledgerLine{debitLine(canonicalGL["cogs"], cost, party, "بهای تمام‌شده فروش"), creditLine(canonicalGL["inventory"], cost, party, "خروج موجودی بابت فروش")}}); err != nil {
				return nil, err
			}
		}
		for _, payment := range rowsFrom(row, "payments") {
			amount := number(payment["amount"])
			if amount <= 0 || stringValue(payment["type"]) == "credit" {
				continue
			}
			asset := settlementDebitAccount(payment, accountMap)
			pid := firstText(payment, "id", "checkNo", "docId")
			if err := add(ledgerEntry{Key: "sale-payment:" + id + ":" + pid, Date: stringValue(row["date"]), Description: "تسویه فاکتور فروش " + firstText(row, "number", "id"), SourceType: "SalesSettlement", Party: party, Lines: []ledgerLine{debitLine(asset, amount, party, "دریافت از مشتری"), creditLine(canonicalGL["receivable"], amount, party, "تسویه حساب مشتری")}}); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range rowsFrom(state, "incomingInvoices") {
		if boolValue(row["nonFinancial"]) || number(row["amount"]) <= 0 {
			continue
		}
		id, party, total := firstText(row, "id", "sourceId"), stringValue(row["customer"]), number(row["amount"])
		vat := math.Max(0, number(row["taxAmount"]))
		base := math.Max(0, total-vat)
		purchaseAccount := inventoryAccount(stringValue(row["inventoryType"]))
		lines := []ledgerLine{debitLine(purchaseAccount, base, party, "خرید/ورود کالا")}
		if vat > 0 {
			lines = append(lines, debitLine(canonicalGL["inputVAT"], vat, party, "اعتبار مالیاتی خرید"))
		}
		lines = append(lines, creditLine(canonicalGL["payable"], total, party, "بدهی به فروشنده"))
		if err := add(ledgerEntry{Key: "purchase:" + id, Date: stringValue(row["date"]), Description: "فاکتور خرید/ورود " + id, SourceType: "PurchaseInvoice", Party: party, Lines: lines}); err != nil {
			return nil, err
		}
		for _, payment := range rowsFrom(row, "payments") {
			amount, typ := number(payment["amount"]), stringValue(payment["type"])
			if amount <= 0 || typ == "credit" {
				continue
			}
			creditAccount := settlementCreditAccount(payment, accountMap)
			pid := firstText(payment, "id", "checkNo", "docId")
			if err := add(ledgerEntry{Key: "purchase-payment:" + id + ":" + pid, Date: stringValue(row["date"]), Description: "تسویه فاکتور خرید/ورود " + id, SourceType: "PurchaseSettlement", Party: party, Lines: []ledgerLine{debitLine(canonicalGL["payable"], amount, party, "تسویه فروشنده"), creditLine(creditAccount, amount, party, "پرداخت به فروشنده")}}); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range rowsFrom(state, "yarnOutInvoices") {
		amount := number(row["amount"])
		mode := stringValue(row["outMode"])
		if amount <= 0 || (mode != "sale" && mode != "barter") {
			continue
		}
		id, party := firstText(row, "id", "sourceId"), stringValue(row["customer"])
		debitAccount := canonicalGL["receivable"]
		if mode == "barter" {
			debitAccount = canonicalGL["inventory"]
		}
		if err := add(ledgerEntry{Key: "yarn-out:" + id, Date: stringValue(row["date"]), Description: "خروج مالی نخ " + id, SourceType: "YarnSale", Party: party, Lines: []ledgerLine{debitLine(debitAccount, amount, party, "خروج نخ"), creditLine(canonicalGL["yarnSales"], amount, party, "درآمد خروج نخ")}}); err != nil {
			return nil, err
		}
		if cost := number(row["costAmount"]); cost > 0 && stringValue(row["stockType"]) != "amanat" {
			if err := add(ledgerEntry{Key: "yarn-out-cogs:" + id, Date: stringValue(row["date"]), Description: "بهای تمام‌شده خروج نخ " + id, SourceType: "CostOfGoodsSold", Party: party, Lines: []ledgerLine{debitLine(canonicalGL["cogs"], cost, party, "بهای تمام‌شده خروج نخ"), creditLine(canonicalGL["yarnInventory"], cost, party, "کاهش موجودی نخ ملکی")}}); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range rowsFrom(state, "expenses") {
		amount := number(row["amount"])
		if amount <= 0 {
			continue
		}
		id := firstText(row, "id", "sourceId")
		account := accountMap[stringValue(row["accountId"])]
		if account.Code == "" {
			account = canonicalGL["cash"]
		}
		description := firstText(row, "description", "subgroup", "title")
		if err := add(ledgerEntry{Key: "expense:" + id, Date: stringValue(row["date"]), Description: "هزینه " + description, SourceType: "Expense", Lines: []ledgerLine{debitLine(canonicalGL["expense"], amount, "", description), creditLine(account, amount, "", "پرداخت هزینه")}}); err != nil {
			return nil, err
		}
	}
	for _, row := range rowsFrom(state, "movements") {
		if hasAny(row, "sourceInvoice", "sourceIncomingInvoice", "sourceExpense") || number(row["amount"]) <= 0 {
			continue
		}
		id, party, amount := firstText(row, "id", "trackingNo"), firstText(row, "payer", "customer"), number(row["amount"])
		cashAccount := accountMap[stringValue(row["accountId"])]
		if cashAccount.Code == "" {
			cashAccount = canonicalGL["bank"]
		}
		counterpart := movementCounterpart(stringValue(row["transactionType"]), stringValue(row["direction"]))
		if stringValue(row["transactionType"]) == "transfer" {
			if transferAccount := accountMap[stringValue(row["counterAccountId"])]; transferAccount.Code != "" {
				counterpart = transferAccount
			}
		}
		left, right := cashAccount, counterpart
		if stringValue(row["direction"]) == "out" {
			left, right = counterpart, cashAccount
		}
		if err := add(ledgerEntry{Key: "movement:" + id, Date: stringValue(row["date"]), Description: firstText(row, "description", "trackingNo"), SourceType: "CashMovement", Party: party, Lines: []ledgerLine{debitLine(left, amount, party, "گردش نقدی"), creditLine(right, amount, party, "گردش نقدی")}}); err != nil {
			return nil, err
		}
	}
	for _, row := range rowsFrom(state, "receivableDocs") {
		amount := number(row["amount"])
		if amount <= 0 {
			continue
		}
		id, party := firstText(row, "id", "checkNo"), stringValue(row["customer"])
		if strings.TrimSpace(stringValue(row["sourceInvoice"])) == "" {
			if err := add(ledgerEntry{Key: "manual-receivable-check:" + id, Date: firstText(row, "receivedAt", "dueDate"), Description: "دریافت چک " + stringValue(row["checkNo"]), SourceType: "ReceivableCheck", Party: party, Lines: []ledgerLine{debitLine(canonicalGL["checkReceivable"], amount, party, "دریافت چک"), creditLine(canonicalGL["receivable"], amount, party, "تسویه حساب مشتری")}}); err != nil {
				return nil, err
			}
		}
		assignedTo := strings.TrimSpace(stringValue(row["assignedTo"]))
		if stringValue(row["status"]) == "assigned" && assignedTo != "" && strings.TrimSpace(stringValue(row["assignedIncomingInvoice"])) == "" {
			if err := add(ledgerEntry{Key: "receivable-check-assigned:" + id, Date: firstText(row, "assignedAt", "dueDate"), Description: "واگذاری چک " + stringValue(row["checkNo"]), SourceType: "CheckAssignment", Party: assignedTo, Lines: []ledgerLine{debitLine(canonicalGL["payable"], amount, assignedTo, "تسویه فروشنده با واگذاری چک"), creditLine(canonicalGL["checkReceivable"], amount, assignedTo, "واگذاری اسناد دریافتنی")}}); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range rowsFrom(state, "payableDocs") {
		amount := number(row["amount"])
		if amount <= 0 || strings.TrimSpace(stringValue(row["sourceIncomingInvoice"])) != "" {
			continue
		}
		id, party := firstText(row, "id", "checkNo"), stringValue(row["customer"])
		if err := add(ledgerEntry{Key: "manual-payable-check:" + id, Date: firstText(row, "issuedAt", "dueDate"), Description: "صدور چک " + stringValue(row["checkNo"]), SourceType: "PayableCheck", Party: party, Lines: []ledgerLine{debitLine(canonicalGL["payable"], amount, party, "تسویه حساب فروشنده"), creditLine(canonicalGL["checkPayable"], amount, party, "صدور چک پرداختنی")}}); err != nil {
			return nil, err
		}
	}
	if err := deriveCheckLifecycleEntries(state, accountMap, add); err != nil {
		return nil, err
	}
	for _, row := range rowsFrom(state, "journalEntries") {
		id := firstText(row, "id", "number")
		entry := ledgerEntry{Key: "manual:" + id, Date: stringValue(row["date"]), Description: stringValue(row["description"]), SourceType: "ManualJournal"}
		for _, source := range rowsFrom(row, "lines") {
			entry.Lines = append(entry.Lines, ledgerLine{AccountCode: stringValue(source["accountCode"]), AccountName: firstText(source, "accountName", "accountCode"), AccountType: firstText(source, "accountType", "type"), Party: stringValue(source["party"]), Debit: number(source["debit"]), Credit: number(source["credit"]), Description: stringValue(source["description"])})
		}
		if err := add(entry); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func deriveCheckLifecycleEntries(state map[string]any, accounts map[string]glAccount, add func(ledgerEntry) error) error {
	defaultBank := canonicalGL["bank"]
	for _, row := range rowsFrom(state, "receivableDocs") {
		status, amount := stringValue(row["status"]), number(row["amount"])
		if amount <= 0 {
			continue
		}
		id, party := firstText(row, "id", "checkNo"), stringValue(row["customer"])
		if status == "returned" || status == "bounced" {
			label := "مرجوع کردن چک"
			if status == "bounced" {
				label = "برگشت چک"
			}
			if err := add(ledgerEntry{Key: "check-received-returned:" + id, Date: firstText(row, "bouncedAt", "returnedAt", "dueDate"), Description: label + " " + stringValue(row["checkNo"]), SourceType: "CheckReturn", Party: party, Lines: []ledgerLine{debitLine(canonicalGL["receivable"], amount, party, label+" و بازگشت طلب مشتری"), creditLine(canonicalGL["checkReceivable"], amount, party, "خروج از اسناد دریافتنی")}}); err != nil {
				return err
			}
			continue
		}
		if status != "cleared" {
			continue
		}
		bank := accounts[firstText(row, "clearingAccountId", "accountId")]
		if bank.Code == "" {
			bank = defaultBank
		}
		if err := add(ledgerEntry{Key: "check-received-cleared:" + id, Date: firstText(row, "clearedAt", "dueDate"), Description: "وصول چک " + stringValue(row["checkNo"]), SourceType: "CheckClearance", Party: party, Lines: []ledgerLine{debitLine(bank, amount, party, "وصول چک"), creditLine(canonicalGL["checkReceivable"], amount, party, "خروج از اسناد دریافتنی")}}); err != nil {
			return err
		}
	}
	for _, row := range rowsFrom(state, "payableDocs") {
		status, amount := stringValue(row["status"]), number(row["amount"])
		if amount <= 0 {
			continue
		}
		id, party := firstText(row, "id", "checkNo"), stringValue(row["customer"])
		if status == "returned" || status == "bounced" {
			label := "مرجوع کردن چک پرداختنی"
			if status == "bounced" {
				label = "برگشت چک پرداختنی"
			}
			if err := add(ledgerEntry{Key: "check-payable-returned:" + id, Date: firstText(row, "bouncedAt", "returnedAt", "dueDate"), Description: label + " " + stringValue(row["checkNo"]), SourceType: "CheckReturn", Party: party, Lines: []ledgerLine{debitLine(canonicalGL["checkPayable"], amount, party, "خروج از اسناد پرداختنی"), creditLine(canonicalGL["payable"], amount, party, "بازگشت بدهی فروشنده")}}); err != nil {
				return err
			}
			continue
		}
		if status != "paid" {
			continue
		}
		bank := accounts[firstText(row, "clearingAccountId", "accountId")]
		if bank.Code == "" {
			bank = defaultBank
		}
		if err := add(ledgerEntry{Key: "check-payable-paid:" + id, Date: firstText(row, "paidAt", "dueDate"), Description: "پرداخت چک " + stringValue(row["checkNo"]), SourceType: "CheckPayment", Party: party, Lines: []ledgerLine{debitLine(canonicalGL["checkPayable"], amount, party, "تسویه اسناد پرداختنی"), creditLine(bank, amount, party, "برداشت بانک")}}); err != nil {
			return err
		}
	}
	return nil
}

func syncWorkspaceLedger(ctx context.Context, tx *sql.Tx, companyID, userID, revision int64, oldState, newState map[string]any) error {
	var existingEntries int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_vouchers WHERE company_id=$1 AND external_key LIKE 'WS:%'`, companyID).Scan(&existingEntries); err != nil {
		return err
	}
	oldEntries := map[string]ledgerEntry{}
	var err error
	if existingEntries > 0 {
		oldEntries, err = deriveWorkspaceLedger(oldState)
		if err != nil {
			return fmt.Errorf("derive previous ledger: %w", err)
		}
	}
	newEntries, err := deriveWorkspaceLedger(newState)
	if err != nil {
		return fmt.Errorf("derive ledger: %w", err)
	}
	// When no workspace vouchers exist, oldEntries intentionally stays empty;
	// the first save performs an idempotent backfill of the current state.
	keys := make([]string, 0, len(oldEntries)+len(newEntries))
	seen := map[string]bool{}
	for key := range oldEntries {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range newEntries {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	branchID, err := ensureLedgerBranch(ctx, tx, companyID)
	if err != nil {
		return err
	}
	for _, key := range keys {
		oldEntry, oldOK := oldEntries[key]
		newEntry, newOK := newEntries[key]
		oldHash, newHash := ledgerHash(oldEntry), ledgerHash(newEntry)
		if oldOK && (!newOK || oldHash != newHash) {
			reversal := reverseLedgerEntry(oldEntry)
			if err := insertLedgerEntry(ctx, tx, companyID, userID, branchID, revision, reversal, "R", oldHash); err != nil {
				return err
			}
		}
		if newOK && (!oldOK || oldHash != newHash) {
			if err := insertLedgerEntry(ctx, tx, companyID, userID, branchID, revision, newEntry, "N", newHash); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureLedgerBranch(ctx context.Context, tx *sql.Tx, companyID int64) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO branches(company_id, code, name, is_active)
		VALUES($1, 'MAIN', 'شعبه اصلی', true)
		ON CONFLICT (company_id, code) DO UPDATE SET is_active=true
		RETURNING id
	`, companyID).Scan(&id)
	return id, err
}

func insertLedgerEntry(ctx context.Context, tx *sql.Tx, companyID, userID, branchID, revision int64, entry ledgerEntry, direction, contentHash string) error {
	if strings.TrimSpace(entry.Date) == "" {
		entry.Date = time.Now().Format("2006-01-02")
	}
	date, err := time.Parse("2006-01-02", entry.Date)
	if err != nil {
		return fmt.Errorf("تاریخ سند %s معتبر نیست", entry.Key)
	}
	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fiscal_periods WHERE company_id=$1 AND status='Closed' AND $2::date BETWEEN start_date AND end_date)`, companyID, date).Scan(&locked); err != nil {
		return err
	}
	if locked {
		return fmt.Errorf("دوره مالی تاریخ %s بسته است و ثبت یا تغییر سند در آن مجاز نیست", entry.Date)
	}
	keyHash := sha256.Sum256([]byte(entry.Key))
	externalKey := fmt.Sprintf("WS:%d:%s:%s:%s", revision, direction, hex.EncodeToString(keyHash[:8]), contentHash[:16])
	var reversalOf any
	if direction == "R" {
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM journal_vouchers WHERE company_id=$1 AND source_reference=$2 AND status='Posted' ORDER BY id DESC LIMIT 1`, companyID, entry.Key).Scan(&id); err == nil {
			reversalOf = id
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	var voucherID int64
	created := true
	err = tx.QueryRowContext(ctx, `
		INSERT INTO journal_vouchers(company_id, branch_id, voucher_no, voucher_date, description, source_doc_type, status, created_by, posted_at, external_key, source_reference, workspace_revision, reversal_of)
		VALUES($1,$2,$3,$4,$5,$6,'Draft',$7,NULL,$8,$9,$10,$11)
		ON CONFLICT (company_id, external_key) WHERE external_key IS NOT NULL
		DO NOTHING
		RETURNING id
	`, companyID, branchID, fmt.Sprintf("WS-%d-%s-%s", revision, direction, contentHash[:8]), date, truncateText(entry.Description, 200), entry.SourceType, nullUserID(userID), externalKey, truncateText(entry.Key, 160), revision, reversalOf).Scan(&voucherID)
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		err = tx.QueryRowContext(ctx, `SELECT id FROM journal_vouchers WHERE company_id=$1 AND external_key=$2`, companyID, externalKey).Scan(&voucherID)
	}
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	for index, line := range entry.Lines {
		accountID, err := ensureLedgerAccount(ctx, tx, companyID, line)
		if err != nil {
			return err
		}
		partyID, err := ensureLedgerParty(ctx, tx, companyID, line.Party)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO journal_voucher_lines(company_id, journal_voucher_id, account_id, party_id, debit, credit, description, line_no)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (journal_voucher_id, line_no) WHERE line_no IS NOT NULL DO NOTHING
		`, companyID, voucherID, accountID, partyID, moneyRound(line.Debit), moneyRound(line.Credit), truncateText(line.Description, 200), index+1)
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE journal_vouchers SET status='Posted', posted_at=CURRENT_TIMESTAMP WHERE company_id=$1 AND id=$2 AND status='Draft'`, companyID, voucherID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("failed to post journal voucher %d", voucherID)
	}
	return nil
}

func ensureLedgerAccount(ctx context.Context, tx *sql.Tx, companyID int64, line ledgerLine) (int64, error) {
	accountType := line.AccountType
	if accountType == "" {
		accountType = "Asset"
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO accounts(company_id, code, name, type, is_detail, is_active)
		VALUES($1,$2,$3,$4,true,true)
		ON CONFLICT (company_id, code) DO UPDATE SET name=EXCLUDED.name, type=EXCLUDED.type, is_active=true
		RETURNING id
	`, companyID, truncateText(line.AccountCode, 20), truncateText(line.AccountName, 100), accountType).Scan(&id)
	return id, err
}

func ensureLedgerParty(ctx context.Context, tx *sql.Tx, companyID int64, name string) (any, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	sum := sha256.Sum256([]byte(strings.ToLower(name)))
	code := "WS-" + strings.ToUpper(hex.EncodeToString(sum[:6]))
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO parties(company_id, code, name, type, is_active)
		VALUES($1,$2,$3,'Customer',true)
		ON CONFLICT (company_id, lower(name)) DO UPDATE SET is_active=true
		RETURNING id
	`, companyID, code, truncateText(name, 100)).Scan(&id)
	return id, err
}

func workspaceCashAccounts(state map[string]any) map[string]glAccount {
	out := map[string]glAccount{}
	for _, row := range rowsFrom(state, "accounts") {
		id := stringValue(row["id"])
		typ := stringValue(row["type"])
		base := canonicalGL["bank"]
		prefix := "112"
		if strings.Contains(typ, "صندوق") || strings.EqualFold(typ, "cash") {
			base = canonicalGL["cash"]
			prefix = "111"
		}
		sum := sha256.Sum256([]byte(id))
		base.Code = prefix + strings.ToUpper(hex.EncodeToString(sum[:3]))
		base.Name = firstText(row, "name", "id")
		out[id] = base
	}
	return out
}

func settlementDebitAccount(payment map[string]any, accounts map[string]glAccount) glAccount {
	switch stringValue(payment["type"]) {
	case "cash":
		if account := accounts[stringValue(payment["accountId"])]; account.Code != "" {
			return account
		}
		return canonicalGL["cash"]
	case "check":
		return canonicalGL["checkReceivable"]
	case "barter_yarn":
		return canonicalGL["yarnInventory"]
	case "barter_fabric":
		return canonicalGL["fabricInventory"]
	default:
		return canonicalGL["clearing"]
	}
}

func settlementCreditAccount(payment map[string]any, accounts map[string]glAccount) glAccount {
	switch stringValue(payment["type"]) {
	case "cash":
		if account := accounts[stringValue(payment["accountId"])]; account.Code != "" {
			return account
		}
		return canonicalGL["cash"]
	case "check":
		return canonicalGL["checkPayable"]
	case "assign_receivable":
		return canonicalGL["checkReceivable"]
	case "barter_yarn":
		return canonicalGL["yarnInventory"]
	case "barter_fabric":
		return canonicalGL["fabricInventory"]
	default:
		return canonicalGL["clearing"]
	}
}

func movementCounterpart(typ, direction string) glAccount {
	switch typ {
	case "customer_receipt":
		return canonicalGL["receivable"]
	case "supplier_payment":
		return canonicalGL["payable"]
	case "expense":
		return canonicalGL["expense"]
	case "other_income":
		return canonicalGL["otherIncome"]
	case "capital":
		return canonicalGL["opening"]
	case "transfer":
		return canonicalGL["clearing"]
	default:
		if direction == "out" {
			return canonicalGL["expense"]
		}
		return canonicalGL["clearing"]
	}
}

func inventoryAccount(typ string) glAccount {
	switch typ {
	case "yarn":
		return canonicalGL["yarnInventory"]
	case "fabric":
		return canonicalGL["fabricInventory"]
	case "spare_part":
		return canonicalGL["spareInventory"]
	case "other":
		return canonicalGL["expense"]
	default:
		return canonicalGL["inventory"]
	}
}

func debitLine(account glAccount, amount float64, party, description string) ledgerLine {
	return ledgerLine{AccountCode: account.Code, AccountName: account.Name, AccountType: account.Type, Party: party, Debit: amount, Description: description}
}

func creditLine(account glAccount, amount float64, party, description string) ledgerLine {
	return ledgerLine{AccountCode: account.Code, AccountName: account.Name, AccountType: account.Type, Party: party, Credit: amount, Description: description}
}

func debitCredit(debitAccount, creditAccount glAccount, amount float64, description, party string) []ledgerLine {
	if amount >= 0 {
		return []ledgerLine{debitLine(debitAccount, amount, party, description), creditLine(creditAccount, amount, party, description)}
	}
	return []ledgerLine{debitLine(creditAccount, -amount, party, description), creditLine(debitAccount, -amount, party, description)}
}

func reverseLedgerEntry(entry ledgerEntry) ledgerEntry {
	result := entry
	// A correction belongs to the accounting period of the source transaction.
	// Posting the reversal on "today" shifts historical revenue/cost between periods.
	result.Date = entry.Date
	result.Description = "برگشت: " + entry.Description
	for index := range result.Lines {
		result.Lines[index].Debit, result.Lines[index].Credit = result.Lines[index].Credit, result.Lines[index].Debit
		result.Lines[index].Description = "برگشت: " + result.Lines[index].Description
	}
	return result
}

func ledgerHash(entry ledgerEntry) string {
	if entry.Key == "" {
		return ""
	}
	payload, _ := json.Marshal(entry)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func firstText(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringValue(row[key])); value != "" {
			return value
		}
	}
	return ""
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func hasAny(row map[string]any, keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(stringValue(row[key])) != "" {
			return true
		}
	}
	return false
}

func amountsEqual(a, b float64) bool {
	tolerance := math.Max(1, math.Max(math.Abs(a), math.Abs(b))*0.000001)
	return math.Abs(a-b) <= tolerance
}

func moneyRound(value float64) float64 { return math.Round(value*100) / 100 }

func truncateText(value string, size int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= size {
		return value
	}
	return string(runes[:size])
}
