package telegramreport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

type ManagementFabricLine struct {
	Name   string  `json:"name"`
	Pieces int     `json:"pieces"`
	Meters float64 `json:"meters"`
	Weight float64 `json:"weight"`
}

type ManagementPartyBalance struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

type ManagementCheck struct {
	Customer  string  `json:"customer"`
	Amount    float64 `json:"amount"`
	CheckNo   string  `json:"check_no"`
	Bank      string  `json:"bank"`
	DueDate   string  `json:"due_date"`
	DueJalali string  `json:"due_jalali,omitempty"`
}

type ManagementAccount struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Balance float64 `json:"balance"`
}

type ManagementReport struct {
	Company     string    `json:"company"`
	Period      string    `json:"period"`
	Date        string    `json:"date"`
	PeriodStart string    `json:"period_start"`
	PeriodEnd   string    `json:"period_end"`
	GeneratedAt time.Time `json:"generated_at"`
	Timezone    string    `json:"timezone"`
	Filename    string    `json:"filename"`

	Production struct {
		Pieces     int                    `json:"pieces"`
		Meters     float64                `json:"meters"`
		Weight     float64                `json:"weight"`
		ActiveDays int                    `json:"active_days"`
		ByFabric   []ManagementFabricLine `json:"by_fabric"`
	} `json:"production"`

	Inputs struct {
		YarnCount  int     `json:"yarn_count"`
		YarnWeight float64 `json:"yarn_weight"`
		BeamCount  int     `json:"beam_count"`
		BeamWeight float64 `json:"beam_weight"`
	} `json:"inputs"`

	Outputs struct {
		FabricInvoices int     `json:"fabric_invoices"`
		FabricPieces   int     `json:"fabric_pieces"`
		FabricMeters   float64 `json:"fabric_meters"`
		FabricWeight   float64 `json:"fabric_weight"`
		YarnCount      int     `json:"yarn_count"`
		YarnWeight     float64 `json:"yarn_weight"`
	} `json:"outputs"`

	Inventory struct {
		FabricPieces int                    `json:"fabric_pieces"`
		FabricMeters float64                `json:"fabric_meters"`
		FabricWeight float64                `json:"fabric_weight"`
		YarnWeight   float64                `json:"yarn_weight"`
		YarnShortage float64                `json:"yarn_shortage"`
		ByFabric     []ManagementFabricLine `json:"by_fabric"`
	} `json:"inventory"`

	Waste struct {
		Weight float64 `json:"weight"`
		Rate   float64 `json:"rate"`
	} `json:"waste"`

	Debtors       []ManagementPartyBalance `json:"debtors"`
	DebtorsTotal  float64                  `json:"debtors_total"`
	Creditors     []ManagementPartyBalance `json:"creditors"`
	CreditorsTotal float64                 `json:"creditors_total"`

	PayableChecksThisMonth    []ManagementCheck `json:"payable_checks_this_month"`
	PayableChecksNextMonth    []ManagementCheck `json:"payable_checks_next_month"`
	ReceivableChecksThisMonth []ManagementCheck `json:"receivable_checks_this_month"`
	ReceivableChecksNextMonth []ManagementCheck `json:"receivable_checks_next_month"`

	PayableThisMonthTotal    float64 `json:"payable_this_month_total"`
	PayableNextMonthTotal    float64 `json:"payable_next_month_total"`
	ReceivableThisMonthTotal float64 `json:"receivable_this_month_total"`
	ReceivableNextMonthTotal float64 `json:"receivable_next_month_total"`

	Accounts      []ManagementAccount `json:"accounts"`
	BankBalance   float64             `json:"bank_balance"`
	CashBalance   float64             `json:"cash_balance"`
	LiquidityGross    float64         `json:"liquidity_gross"`
	LiquidityAdjusted float64         `json:"liquidity_adjusted"`

	Alerts []string `json:"alerts"`
	Text   string   `json:"text"`
}

func (s *Service) ResolveCompanyID(ctx context.Context, name string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("database is not available")
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM companies
		WHERE LOWER(TRIM(name))=LOWER(TRIM($1))
		ORDER BY id
		LIMIT 1
	`, strings.TrimSpace(name)).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Service) BuildManagementReport(ctx context.Context, companyID int64, period string, now time.Time) (ManagementReport, error) {
	var report ManagementReport
	period = strings.ToLower(strings.TrimSpace(period))
	if period != "weekly" && period != "monthly" {
		period = "daily"
	}
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		loc = time.FixedZone("Asia/Tehran", 3*60*60+30*60)
	}
	local := now.In(loc)
	start, end := managementPeriod(local, period)
	settings, settingsErr := s.GetSettings(ctx, companyID)
	accountingSLADays := 2
	if settingsErr == nil && settings.AccountingSLADays > 0 {
		accountingSLADays = settings.AccountingSLADays
	}
	snapshot, err := s.collectPeriod(ctx, companyID, start, end, accountingSLADays)
	if err != nil {
		return report, err
	}

	report.Company = snapshot.Company
	report.Period = period
	report.Date = local.Format("2006-01-02")
	report.PeriodStart = start.Format("2006-01-02")
	report.PeriodEnd = end.Format("2006-01-02")
	report.GeneratedAt = local
	report.Timezone = "Asia/Tehran"
	report.Filename = report.Date + ".txt"

	report.Production.Pieces = snapshot.ProductionCount
	report.Production.Meters = snapshot.ProductionMeters
	report.Production.Weight = snapshot.ProductionWeight
	report.Production.ActiveDays = snapshot.ActiveDays
	report.Inputs.YarnCount = snapshot.InputCount
	report.Inputs.YarnWeight = snapshot.InputWeight
	report.Inputs.BeamCount = snapshot.BeamInputCount
	report.Inputs.BeamWeight = snapshot.BeamInputWeight
	report.Outputs.FabricInvoices = snapshot.FabricOutInvoices
	report.Outputs.FabricPieces = snapshot.FabricOutPieces
	report.Outputs.FabricMeters = snapshot.FabricOutMeters
	report.Outputs.FabricWeight = snapshot.FabricOutWeight
	report.Outputs.YarnCount = snapshot.YarnOutCount
	report.Outputs.YarnWeight = snapshot.YarnOutWeight
	report.Inventory.FabricPieces = snapshot.FabricStockPieces
	report.Inventory.FabricMeters = snapshot.FabricStockMeters
	report.Inventory.FabricWeight = snapshot.FabricStockWeight
	report.Inventory.YarnWeight = snapshot.YarnStockWeight
	if snapshot.YarnStockWeight < 0 {
		report.Inventory.YarnShortage = -snapshot.YarnStockWeight
	}
	report.Waste.Weight = snapshot.ScrapWeight
	if total := snapshot.ProductionWeight + snapshot.ScrapWeight; total > 0 {
		report.Waste.Rate = snapshot.ScrapWeight * 100 / total
	}

	workspaceState, err := s.managementWorkspaceState(ctx, companyID)
	if err != nil {
		return report, err
	}
	collectManagementFinancials(&report, workspaceState, local)
	if produced, stocked, breakdownErr := s.collectFabricBreakdowns(ctx, companyID, start, end); breakdownErr == nil {
		report.Production.ByFabric = produced
		report.Inventory.ByFabric = stocked
	}
	buildManagementAlerts(&report, local)
	report.Text = formatManagementReport(report)
	return report, nil
}

func managementPeriod(local time.Time, period string) (time.Time, time.Time) {
	end := local
	switch period {
	case "weekly":
		// Reports are sent on Thursday; the requested business week is Friday through Thursday.
		return end.AddDate(0, 0, -6), end
	case "monthly":
		return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location()), end
	default:
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()), end
	}
}

func (s *Service) managementWorkspaceState(ctx context.Context, companyID int64) (map[string]any, error) {
	return postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (map[string]any, error) {
		var raw []byte
		err := q.QueryRowContext(ctx, `SELECT state FROM financial_workspace_states WHERE company_id=$1`, companyID).Scan(&raw)
		if err != nil {
			if err == sql.ErrNoRows {
				return map[string]any{}, nil
			}
			return nil, err
		}
		state := map[string]any{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &state); err != nil {
				return nil, err
			}
		}
		return state, nil
	})
}

func collectManagementFinancials(report *ManagementReport, state map[string]any, local time.Time) {
	debtors := map[string]float64{}
	creditors := map[string]float64{}

	for _, row := range objectList(state["invoices"]) {
		name := strings.TrimSpace(stringValue(first(row, "customer", "party", "name")))
		if name == "" {
			name = "نامشخص"
		}
		if amount := outstandingDocument(row); amount > 0 {
			debtors[name] += amount
		}
	}
	for _, row := range objectList(state["incomingInvoices"]) {
		name := strings.TrimSpace(stringValue(first(row, "customer", "supplier", "party", "name")))
		if name == "" {
			name = "نامشخص"
		}
		if amount := outstandingDocument(row); amount > 0 {
			creditors[name] += amount
		}
	}
	for _, row := range objectList(state["openingBalances"]) {
		name := strings.TrimSpace(stringValue(first(row, "customer", "party", "name")))
		if name == "" {
			continue
		}
		amount := numberValue(first(row, "amount", "balance"))
		switch strings.ToLower(strings.TrimSpace(stringValue(row["type"]))) {
		case "receivable":
			debtors[name] += amount
		case "payable":
			creditors[name] += amount
		}
	}
	report.Debtors = sortedPartyBalances(debtors)
	report.Creditors = sortedPartyBalances(creditors)
	for _, row := range report.Debtors {
		report.DebtorsTotal += row.Amount
	}
	for _, row := range report.Creditors {
		report.CreditorsTotal += row.Amount
	}

	thisYear, thisMonth := local.Year(), local.Month()
	next := local.AddDate(0, 1, 0)
	for _, row := range objectList(state["payableDocs"]) {
		status := strings.ToLower(strings.TrimSpace(stringValue(row["status"])))
		if status == "paid" || status == "cleared" || status == "cancelled" {
			continue
		}
		check, due, ok := managementCheckFromRow(row)
		if !ok {
			continue
		}
		if due.Year() == thisYear && due.Month() == thisMonth {
			report.PayableChecksThisMonth = append(report.PayableChecksThisMonth, check)
			report.PayableThisMonthTotal += check.Amount
		} else if due.Year() == next.Year() && due.Month() == next.Month() {
			report.PayableChecksNextMonth = append(report.PayableChecksNextMonth, check)
			report.PayableNextMonthTotal += check.Amount
		}
	}
	for _, row := range objectList(state["receivableDocs"]) {
		status := strings.ToLower(strings.TrimSpace(stringValue(row["status"])))
		if status == "cleared" || status == "assigned" || status == "cancelled" {
			continue
		}
		check, due, ok := managementCheckFromRow(row)
		if !ok {
			continue
		}
		if due.Year() == thisYear && due.Month() == thisMonth {
			report.ReceivableChecksThisMonth = append(report.ReceivableChecksThisMonth, check)
			report.ReceivableThisMonthTotal += check.Amount
		} else if due.Year() == next.Year() && due.Month() == next.Month() {
			report.ReceivableChecksNextMonth = append(report.ReceivableChecksNextMonth, check)
			report.ReceivableNextMonthTotal += check.Amount
		}
	}
	sortChecks(report.PayableChecksThisMonth)
	sortChecks(report.PayableChecksNextMonth)
	sortChecks(report.ReceivableChecksThisMonth)
	sortChecks(report.ReceivableChecksNextMonth)

	movements := objectList(state["movements"])
	for _, account := range objectList(state["accounts"]) {
		id := strings.TrimSpace(stringValue(account["id"]))
		name := strings.TrimSpace(stringValue(account["name"]))
		kind := strings.TrimSpace(stringValue(account["type"]))
		balance := numberValue(first(account, "opening", "balance"))
		for _, movement := range movements {
			amount := numberValue(movement["amount"])
			direction := strings.ToLower(strings.TrimSpace(stringValue(movement["direction"])))
			if strings.TrimSpace(stringValue(movement["accountId"])) == id {
				if direction == "in" {
					balance += amount
				} else {
					balance -= amount
				}
			}
			if strings.EqualFold(strings.TrimSpace(stringValue(movement["transactionType"])), "transfer") &&
				strings.TrimSpace(stringValue(movement["counterAccountId"])) == id {
				if direction == "in" {
					balance -= amount
				} else {
					balance += amount
				}
			}
		}
		item := ManagementAccount{Name: name, Type: kind, Balance: balance}
		report.Accounts = append(report.Accounts, item)
		if strings.Contains(kind, "بانک") || strings.EqualFold(kind, "bank") {
			report.BankBalance += balance
		} else if strings.Contains(kind, "صندوق") || strings.EqualFold(kind, "cash") {
			report.CashBalance += balance
		}
	}
	sort.Slice(report.Accounts, func(i, j int) bool { return report.Accounts[i].Name < report.Accounts[j].Name })
	report.LiquidityGross = report.BankBalance - report.PayableThisMonthTotal
	report.LiquidityAdjusted = report.BankBalance + report.ReceivableThisMonthTotal - report.PayableThisMonthTotal
}

func outstandingDocument(row map[string]any) float64 {
	if explicit := numberValue(first(row, "creditAmount", "credit_amount", "debt", "remaining")); explicit > 0 {
		return explicit
	}
	total := numberValue(first(row, "total", "amount"))
	paid := 0.0
	for _, payment := range objectList(row["payments"]) {
		kind := strings.ToLower(strings.TrimSpace(stringValue(payment["type"])))
		if kind == "credit" || kind == "credit_balance" {
			continue
		}
		paid += numberValue(payment["amount"])
	}
	if total > paid {
		return total - paid
	}
	return 0
}

func sortedPartyBalances(values map[string]float64) []ManagementPartyBalance {
	rows := make([]ManagementPartyBalance, 0, len(values))
	for name, amount := range values {
		if amount > 0 {
			rows = append(rows, ManagementPartyBalance{Name: name, Amount: amount})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Amount == rows[j].Amount {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Amount > rows[j].Amount
	})
	return rows
}

func managementCheckFromRow(row map[string]any) (ManagementCheck, time.Time, bool) {
	dueText := strings.TrimSpace(stringValue(first(row, "dueDate", "due_date", "date")))
	due, ok := parseAccountingDate(dueText)
	if !ok {
		return ManagementCheck{}, time.Time{}, false
	}
	return ManagementCheck{
		Customer:  strings.TrimSpace(stringValue(first(row, "customer", "party", "supplier", "payer"))),
		Amount:    numberValue(first(row, "amount", "value")),
		CheckNo:   strings.TrimSpace(stringValue(first(row, "checkNo", "check_no", "number"))),
		Bank:      strings.TrimSpace(stringValue(first(row, "bank", "bankName", "bank_name"))),
		DueDate:   due.Format("2006-01-02"),
		DueJalali: strings.TrimSpace(stringValue(first(row, "dueJalali", "due_jalali"))),
	}, due, true
}

func sortChecks(rows []ManagementCheck) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].DueDate == rows[j].DueDate {
			return rows[i].Amount > rows[j].Amount
		}
		return rows[i].DueDate < rows[j].DueDate
	})
}

func (s *Service) collectFabricBreakdowns(ctx context.Context, companyID int64, start, end time.Time) ([]ManagementFabricLine, []ManagementFabricLine, error) {
	var schemaName string
	if err := s.db.QueryRowContext(ctx, `
		SELECT schema_name FROM public.operational_tenants
		WHERE external_company_id=$1 AND active=1
		ORDER BY id DESC LIMIT 1
	`, companyID).Scan(&schemaName); err != nil {
		return nil, nil, err
	}
	schemaName = strings.TrimSpace(schemaName)
	schema := pq.QuoteIdentifier(schemaName)
	columns, err := s.tableColumns(ctx, schemaName, "salon")
	if err != nil {
		return nil, nil, err
	}
	nameExpr := "'نامشخص'::text"
	for _, candidate := range []string{"noe_salon", "jens_salon", "name_kala_salon", "kala_name", "item_name", "product_type", "noe_kala", "name_kala"} {
		if columns[candidate] {
			nameExpr = fmt.Sprintf("COALESCE(NULLIF(TRIM(s.%s::text),''),'نامشخص')", pq.QuoteIdentifier(candidate))
			break
		}
	}
	query := fmt.Sprintf(`
		SELECT %s AS fabric_name,
		       COALESCE(s.tarikh_salon,'')::text,
		       COALESCE(s.metr_salon,0)::double precision,
		       COALESCE(s.w_salon,0)::double precision,
		       s.id_salon::text,
		       EXISTS (
			SELECT 1 FROM %s.f_khor f
			WHERE TRIM(COALESCE(f.taghe_cod_f_khor,''))=s.id_salon::text
		) AS is_out
		FROM %s.salon s
	`, nameExpr, schema, schema)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	produced := map[string]*ManagementFabricLine{}
	stocked := map[string]*ManagementFabricLine{}
	for rows.Next() {
		var name, dateText, id string
		var meters, weight float64
		var isOut bool
		if err := rows.Scan(&name, &dateText, &meters, &weight, &id, &isOut); err != nil {
			return nil, nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			name = "نامشخص"
		}
		if date, ok := parseAccountingDate(dateText); ok && dateWithinPeriod(date, start, end) {
			line := produced[name]
			if line == nil {
				line = &ManagementFabricLine{Name: name}
				produced[name] = line
			}
			line.Pieces++
			line.Meters += meters
			line.Weight += weight
		}
		if !isOut {
			line := stocked[name]
			if line == nil {
				line = &ManagementFabricLine{Name: name}
				stocked[name] = line
			}
			line.Pieces++
			line.Meters += meters
			line.Weight += weight
		}
		_ = id
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return fabricLines(produced), fabricLines(stocked), nil
}

func (s *Service) tableColumns(ctx context.Context, schema, table string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT LOWER(column_name)
		FROM information_schema.columns
		WHERE table_schema=$1 AND table_name=$2
	`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		result[name] = true
	}
	return result, rows.Err()
}

func fabricLines(values map[string]*ManagementFabricLine) []ManagementFabricLine {
	rows := make([]ManagementFabricLine, 0, len(values))
	for _, row := range values {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Meters == rows[j].Meters {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Meters > rows[j].Meters
	})
	return rows
}

func buildManagementAlerts(report *ManagementReport, local time.Time) {
	if report.Inventory.YarnShortage > 0 {
		report.Alerts = append(report.Alerts, fmt.Sprintf("کسری محاسبه‌شده نخ: %s کیلو", formatNumber(report.Inventory.YarnShortage)))
	}
	if report.LiquidityGross < 0 {
		report.Alerts = append(report.Alerts, fmt.Sprintf("کسری بانکی برای چک‌های این ماه: %s تومان", formatNumber(-report.LiquidityGross)))
	}
	if report.LiquidityAdjusted < 0 {
		report.Alerts = append(report.Alerts, fmt.Sprintf("حتی با احتساب چک‌های دریافتی این ماه، کسری نقدینگی %s تومان است", formatNumber(-report.LiquidityAdjusted)))
	}
	if len(report.Debtors) > 0 {
		report.Alerts = append(report.Alerts, fmt.Sprintf("بزرگ‌ترین بدهکار: %s با %s تومان", report.Debtors[0].Name, formatNumber(report.Debtors[0].Amount)))
	}
	if report.Waste.Rate >= 3 {
		report.Alerts = append(report.Alerts, fmt.Sprintf("نرخ ضایعات %s درصد است و نیاز به بررسی دارد", formatNumber(report.Waste.Rate)))
	}
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	for _, check := range report.PayableChecksThisMonth {
		if due, ok := parseAccountingDate(check.DueDate); ok && due.Before(today) {
			report.Alerts = append(report.Alerts, fmt.Sprintf("چک پرداختی سررسیدگذشته: %s، %s تومان، %s", check.DueDate, formatNumber(check.Amount), check.Customer))
		}
	}
	if len(report.Alerts) == 0 {
		report.Alerts = append(report.Alerts, "هشدار بحرانی ثبت‌شده‌ای در داده‌های قابل محاسبه دیده نشد.")
	}
}

func formatManagementReport(r ManagementReport) string {
	var b strings.Builder
	title := "📊 گزارش روزانه تولید و عملیات"
	periodLabel := r.Date
	if r.Period == "weekly" {
		title = "📈 گزارش هفتگی تولید، عملیات و مالی"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	} else if r.Period == "monthly" {
		title = "🗓 گزارش ماهانه تولید، عملیات و مالی"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	}
	avgMeters, avgWeight := 0.0, 0.0
	if r.Production.ActiveDays > 0 {
		avgMeters = r.Production.Meters / float64(r.Production.ActiveDays)
		avgWeight = r.Production.Weight / float64(r.Production.ActiveDays)
	}
	fmt.Fprintf(&b, "%s\n🏢 %s\n📅 %s\n🕒 %s به وقت تهران\n\n", title, fallback(r.Company, "paregol"), periodLabel, r.GeneratedAt.Format("15:04"))
	fmt.Fprintf(&b, "🏭 تولید پارچه\n• تعداد طاقه تولیدشده: %s\n• کل متراژ تولید: %s متر\n• کل وزن تولید: %s کیلو\n", formatNumber(float64(r.Production.Pieces)), formatNumber(r.Production.Meters), formatNumber(r.Production.Weight))
	for _, row := range r.Production.ByFabric {
		fmt.Fprintf(&b, "• %s: %s طاقه، %s متر، %s کیلو\n", row.Name, formatNumber(float64(row.Pieces)), formatNumber(row.Meters), formatNumber(row.Weight))
	}
	fmt.Fprintf(&b, "• روزهای دارای تولید: %s\n• میانگین روز فعال: %s متر و %s کیلو\n\n", formatNumber(float64(r.Production.ActiveDays)), formatNumber(avgMeters), formatNumber(avgWeight))

	fmt.Fprintf(&b, "📥 ورود مواد و چله\n• ورود نخ: %s ثبت، %s کیلو\n• ورود چله: %s ثبت، %s کیلو\n\n", formatNumber(float64(r.Inputs.YarnCount)), formatNumber(r.Inputs.YarnWeight), formatNumber(float64(r.Inputs.BeamCount)), formatNumber(r.Inputs.BeamWeight))
	fmt.Fprintf(&b, "📤 خروج کالا و نخ\n• فاکتور خروج پارچه: %s\n• طاقه خروجی: %s\n• متراژ خروجی: %s متر\n• وزن خروجی: %s کیلو\n• خروج نخ: %s ثبت، %s کیلو\n\n", formatNumber(float64(r.Outputs.FabricInvoices)), formatNumber(float64(r.Outputs.FabricPieces)), formatNumber(r.Outputs.FabricMeters), formatNumber(r.Outputs.FabricWeight), formatNumber(float64(r.Outputs.YarnCount)), formatNumber(r.Outputs.YarnWeight))

	fmt.Fprintf(&b, "📦 موجودی فعلی\n• پارچه آماده: %s طاقه، %s متر، %s کیلو\n", formatNumber(float64(r.Inventory.FabricPieces)), formatNumber(r.Inventory.FabricMeters), formatNumber(r.Inventory.FabricWeight))
	for _, row := range r.Inventory.ByFabric {
		fmt.Fprintf(&b, "• %s: %s طاقه، %s متر، %s کیلو\n", row.Name, formatNumber(float64(row.Pieces)), formatNumber(row.Meters), formatNumber(row.Weight))
	}
	if r.Inventory.YarnShortage > 0 {
		fmt.Fprintf(&b, "• کسری محاسبه‌شده نخ: %s کیلو\n\n", formatNumber(r.Inventory.YarnShortage))
	} else {
		fmt.Fprintf(&b, "• موجودی محاسبه‌شده نخ: %s کیلو\n\n", formatNumber(r.Inventory.YarnWeight))
	}
	fmt.Fprintf(&b, "♻️ ضایعات تولید\n• وزن ضایعات: %s کیلو\n• نرخ ضایعات: %s درصد\n\n", formatNumber(r.Waste.Weight), formatNumber(r.Waste.Rate))

	fmt.Fprintf(&b, "💳 مشتریان بدهکار\n")
	writePartyLines(&b, r.Debtors)
	fmt.Fprintf(&b, "• جمع بدهکاران: %s تومان\n\n", formatNumber(r.DebtorsTotal))
	fmt.Fprintf(&b, "💚 بستانکاران / طلبکاران از شرکت\n")
	writePartyLines(&b, r.Creditors)
	fmt.Fprintf(&b, "• جمع بستانکاران: %s تومان\n\n", formatNumber(r.CreditorsTotal))

	writeCheckSection(&b, "🧾 چک‌های پرداختی این ماه", r.PayableChecksThisMonth, r.PayableThisMonthTotal)
	writeCheckSection(&b, "🧾 چک‌های پرداختی ماه آینده", r.PayableChecksNextMonth, r.PayableNextMonthTotal)
	writeCheckSection(&b, "💰 چک‌های دریافتی این ماه", r.ReceivableChecksThisMonth, r.ReceivableThisMonthTotal)
	writeCheckSection(&b, "💰 چک‌های دریافتی ماه آینده", r.ReceivableChecksNextMonth, r.ReceivableNextMonthTotal)

	fmt.Fprintf(&b, "🏦 موجودی بانک‌ها و صندوق\n")
	for _, account := range r.Accounts {
		fmt.Fprintf(&b, "• %s (%s): %s تومان\n", account.Name, account.Type, formatNumber(account.Balance))
	}
	fmt.Fprintf(&b, "• جمع بانک‌ها: %s تومان\n• جمع صندوق: %s تومان\n\n", formatNumber(r.BankBalance), formatNumber(r.CashBalance))
	fmt.Fprintf(&b, "⚖️ پوشش چک‌های این ماه\n• مازاد/کسری ناخالص بانک: %s تومان\n• مازاد/کسری با احتساب چک‌های دریافتی این ماه: %s تومان\n\n", formatSigned(r.LiquidityGross), formatSigned(r.LiquidityAdjusted))

	fmt.Fprintf(&b, "🚨 هشدارها و نکات مدیریتی\n")
	for _, alert := range r.Alerts {
		fmt.Fprintf(&b, "• %s\n", alert)
	}
	fmt.Fprintf(&b, "\nآخرین خواندن گزارش: %s\n", r.GeneratedAt.Format(time.RFC3339))
	return b.String()
}

func writePartyLines(b *strings.Builder, rows []ManagementPartyBalance) {
	if len(rows) == 0 {
		b.WriteString("• مورد بازی ثبت نشده است.\n")
		return
	}
	for _, row := range rows {
		fmt.Fprintf(b, "• %s: %s تومان\n", row.Name, formatNumber(row.Amount))
	}
}

func writeCheckSection(b *strings.Builder, title string, rows []ManagementCheck, total float64) {
	fmt.Fprintf(b, "%s\n", title)
	if len(rows) == 0 {
		b.WriteString("• موردی ثبت نشده است.\n")
	}
	for _, row := range rows {
		due := row.DueDate
		if row.DueJalali != "" {
			due += " / " + row.DueJalali
		}
		fmt.Fprintf(b, "• %s | %s | %s تومان | %s | %s\n", due, fallback(row.CheckNo, "بدون شماره"), formatNumber(row.Amount), fallback(row.Bank, "بانک نامشخص"), fallback(row.Customer, "طرف حساب نامشخص"))
	}
	fmt.Fprintf(b, "• جمع: %s تومان\n\n", formatNumber(total))
}

func formatSigned(value float64) string {
	if value < 0 {
		return "کسری " + formatNumber(-value)
	}
	return "مازاد " + formatNumber(value)
}
