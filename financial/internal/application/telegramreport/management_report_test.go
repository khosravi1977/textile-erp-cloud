package telegramreport

import (
	"math"
	"testing"
	"time"
)

func TestManagementPeriod(t *testing.T) {
	loc := time.FixedZone("Asia/Tehran", 3*60*60+30*60)
	now := time.Date(2026, 8, 20, 19, 0, 0, 0, loc)

	dailyStart, dailyEnd := managementPeriod(now, "daily")
	if got := dailyStart.Format("2006-01-02 15:04"); got != "2026-08-20 00:00" {
		t.Fatalf("daily start=%s", got)
	}
	if !dailyEnd.Equal(now) {
		t.Fatalf("daily end=%v", dailyEnd)
	}

	weeklyStart, _ := managementPeriod(now, "weekly")
	if got := weeklyStart.Format("2006-01-02"); got != "2026-08-14" {
		t.Fatalf("weekly start=%s", got)
	}

	monthlyStart, _ := managementPeriod(now, "monthly")
	if got := monthlyStart.Format("2006-01-02 15:04"); got != "2026-08-01 00:00" {
		t.Fatalf("monthly start=%s", got)
	}
}

func TestOutstandingDocument(t *testing.T) {
	row := map[string]any{
		"total": 1000.0,
		"payments": []any{
			map[string]any{"type": "cash", "amount": 250.0},
			map[string]any{"type": "credit", "amount": 100.0},
		},
	}
	if got := outstandingDocument(row); got != 750 {
		t.Fatalf("outstanding=%v", got)
	}

	row["creditAmount"] = 300.0
	if got := outstandingDocument(row); got != 300 {
		t.Fatalf("explicit outstanding=%v", got)
	}
}

func TestCollectManagementFinancials(t *testing.T) {
	loc := time.FixedZone("Asia/Tehran", 3*60*60+30*60)
	now := time.Date(2026, 8, 18, 19, 0, 0, 0, loc)

	state := map[string]any{
		"invoices": []any{
			map[string]any{"customer": "مشتری الف", "total": 1000.0, "payments": []any{map[string]any{"type": "cash", "amount": 400.0}}},
		},
		"incomingInvoices": []any{
			map[string]any{"supplier": "فروشنده ب", "total": 700.0, "payments": []any{map[string]any{"type": "cash", "amount": 200.0}}},
		},
		"payableDocs": []any{
			map[string]any{"customer": "ذی نفع", "amount": 900.0, "checkNo": "P1", "dueDate": "2026-08-25", "bank": "بانک الف", "status": "open"},
			map[string]any{"customer": "ذی نفع ۲", "amount": 400.0, "checkNo": "P2", "dueDate": "2026-09-05", "bank": "بانک الف", "status": "open"},
		},
		"receivableDocs": []any{
			map[string]any{"customer": "مشتری چک", "amount": 500.0, "checkNo": "R1", "dueDate": "2026-08-27", "bank": "بانک ب", "status": "received"},
		},
		"accounts": []any{
			map[string]any{"id": "1", "name": "بانک اصلی", "type": "بانک", "opening": 1200.0},
			map[string]any{"id": "2", "name": "صندوق", "type": "صندوق", "opening": 100.0},
		},
		"movements": []any{
			map[string]any{"accountId": "1", "direction": "in", "amount": 300.0},
			map[string]any{"accountId": "1", "direction": "out", "amount": 200.0},
		},
	}

	var report ManagementReport
	collectManagementFinancials(&report, state, now)

	assertFloat(t, "debtors", report.DebtorsTotal, 600)
	assertFloat(t, "creditors", report.CreditorsTotal, 500)
	assertFloat(t, "payable this month", report.PayableThisMonthTotal, 900)
	assertFloat(t, "payable next month", report.PayableNextMonthTotal, 400)
	assertFloat(t, "receivable this month", report.ReceivableThisMonthTotal, 500)
	assertFloat(t, "bank balance", report.BankBalance, 1300)
	assertFloat(t, "cash balance", report.CashBalance, 100)
	assertFloat(t, "gross liquidity", report.LiquidityGross, 400)
	assertFloat(t, "adjusted liquidity", report.LiquidityAdjusted, 900)
}

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("%s: got %v want %v", name, got, want)
	}
}
