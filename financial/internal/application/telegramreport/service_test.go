package telegramreport

import (
	"strings"
	"testing"
)

func TestCollectWorkspaceAndFormat(t *testing.T) {
	state := map[string]any{
		"invoices": []any{
			map[string]any{"date": "2026-07-26", "weight": 125.5, "meter": 840, "total": 4500000},
			map[string]any{"date": "2026-07-25", "weight": 90},
		},
		"incomingInvoices": []any{
			map[string]any{"date": "2026-07-26", "quantity": 210.0, "amount": 2200000},
		},
		"yarnOutInvoices": []any{
			map[string]any{"date": "2026-07-26", "quantity": 15.0},
		},
		"ownedInventory": []any{
			map[string]any{"quantity": 300.0, "amount": 7000000},
			map[string]any{"quantity": -25.0, "amount": -400000},
		},
	}
	var report reportSnapshot
	collectWorkspace(&report, state, "2026-07-26")
	if report.OutputCount != 1 || report.OutputWeight != 125.5 || report.OutputMeters != 840 {
		t.Fatalf("unexpected production summary: %#v", report)
	}
	if report.InventoryQty != 275 || report.InventoryValue != 6600000 {
		t.Fatalf("unexpected inventory summary: %#v", report)
	}
	text := formatTextileReport(report, false)
	for _, expected := range []string{"گزارش روزانه", "126 کیلو", "840 متر", "6,600,000 تومان"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("report does not contain %q: %s", expected, text)
		}
	}
}

func TestValidClock(t *testing.T) {
	if !validClock("20:05") || validClock("24:00") || validClock("8:00") {
		t.Fatal("clock validation is incorrect")
	}
}

func TestSecretsAreNeverReturnedInSettings(t *testing.T) {
	service := New(nil, Config{Enabled: true, BotToken: "secret-token", BotUsername: "my_bot"})
	settings := Settings{Available: service.Available(), BotUsername: service.cfg.BotUsername}
	if strings.Contains(settings.BotUsername, "secret-token") {
		t.Fatal("bot token leaked")
	}
}
