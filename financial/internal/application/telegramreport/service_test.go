package telegramreport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	report.PeriodStart = "2026-07-26"
	report.PeriodEnd = "2026-07-26"
	text := formatTextileReport(report, "daily")
	for _, expected := range []string{"گزارش روزانه", "126 کیلو", "840 متر", "6,600,000 تومان"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("report does not contain %q: %s", expected, text)
		}
	}
}

func TestCollectWorkspacePeriod(t *testing.T) {
	state := map[string]any{
		"invoices": []any{
			map[string]any{"date": "2026-07-20", "weight": 100.0, "meter": 600.0, "total": 3000000.0},
			map[string]any{"date": "2026-07-26", "weight": 150.0, "meter": 900.0, "total": 5000000.0},
			map[string]any{"date": "2026-07-19", "weight": 999.0, "meter": 999.0, "total": 999.0},
		},
		"incomingInvoices": []any{
			map[string]any{"date": "2026-07-21", "quantity": 80.0, "amount": 2000000.0},
		},
		"yarnOutInvoices": []any{
			map[string]any{"date": "2026-07-22", "quantity": 20.0},
		},
		"ownedInventory": []any{
			map[string]any{"quantity": 450.0, "amount": 12000000.0},
		},
	}
	var report reportSnapshot
	collectWorkspacePeriod(&report, state, "2026-07-20", "2026-07-26")
	if report.OutputCount != 2 || report.OutputWeight != 250 || report.OutputMeters != 1500 {
		t.Fatalf("unexpected weekly production summary: %#v", report)
	}
	if report.InputCount != 1 || report.YarnOutCount != 1 {
		t.Fatalf("unexpected weekly movement summary: %#v", report)
	}
	if days := countDaysWithActivity(state, "2026-07-20", "2026-07-26"); days != 4 {
		t.Fatalf("unexpected active day count: %d", days)
	}
}

func TestReportPeriods(t *testing.T) {
	location := time.FixedZone("Tehran", 3*60*60+30*60)
	now := time.Date(2026, time.July, 27, 20, 0, 0, 0, location)

	weeklyStart, weeklyEnd := reportPeriod(now, "weekly")
	if weeklyStart.Format("2006-01-02") != "2026-07-21" || weeklyEnd.Format("2006-01-02") != "2026-07-27" {
		t.Fatalf("unexpected weekly period: %s..%s", weeklyStart, weeklyEnd)
	}

	monthlyStart, monthlyEnd := reportPeriod(now, "monthly")
	if monthlyStart.Format("2006-01-02") != "2026-06-01" || monthlyEnd.Format("2006-01-02") != "2026-06-30" {
		t.Fatalf("unexpected monthly period: %s..%s", monthlyStart, monthlyEnd)
	}
}

func TestApplyOperationalRowsAndFormatReport(t *testing.T) {
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	report := reportSnapshot{
		Company:           "نساجی آزمایشی",
		PeriodStart:       start.Format("2006-01-02"),
		PeriodEnd:         end.Format("2006-01-02"),
		FabricStockPieces: 14,
		FabricStockMeters: 2100,
		FabricStockWeight: 520,
		YarnStockWeight:   -35,
	}
	rows := []operationalReportRow{
		{Kind: "production", Date: "2026-07-10", Reference: "1", Weight: 30, Meters: 120},
		{Kind: "production", Date: "2026-07-10", Reference: "2", Weight: 40, Meters: 180},
		{Kind: "production", Date: "2026-07-11", Reference: "3", Weight: 50, Meters: 220},
		{Kind: "yarn_in", Date: "2026-07-12", Reference: "4", Weight: 250},
		{Kind: "yarn_out", Date: "2026-07-13", Reference: "5", Weight: 25},
		{Kind: "beam_in", Date: "2026-07-14", Reference: "6", Weight: 170},
		{Kind: "fabric_out", Date: "2026-07-15", Reference: "INV-1", Weight: 30, Meters: 120},
		{Kind: "fabric_out", Date: "2026-07-15", Reference: "INV-1", Weight: 40, Meters: 180},
		{Kind: "fabric_out", Date: "2026-07-16", Reference: "INV-2", Weight: 50, Meters: 220},
		{Kind: "waste", Date: "2026-07-17", Reference: "7", Weight: 5},
		{Kind: "production", Date: "2026-06-30", Reference: "old", Weight: 999, Meters: 999},
	}
	applyOperationalRows(&report, rows, start, end)
	report.OperationalData = true
	if report.ProductionCount != 3 || report.ProductionWeight != 120 ||
		report.ProductionMeters != 520 || report.ActiveDays != 2 {
		t.Fatalf("unexpected production aggregation: %#v", report)
	}
	if report.FabricOutInvoices != 2 || report.FabricOutPieces != 3 ||
		report.FabricOutWeight != 120 || report.FabricOutMeters != 520 {
		t.Fatalf("unexpected fabric output aggregation: %#v", report)
	}
	if report.InputCount != 1 || report.InputWeight != 250 ||
		report.YarnOutCount != 1 || report.YarnOutWeight != 25 ||
		report.BeamInputCount != 1 || report.BeamInputWeight != 170 ||
		report.ScrapWeight != 5 {
		t.Fatalf("unexpected operational movement aggregation: %#v", report)
	}
	text := formatTextileReport(report, "test")
	for _, expected := range []string{
		"گزارش آزمایشی ۳۰ روز اخیر تولید و عملیات",
		"تعداد طاقه تولیدشده: 3",
		"متراژ تولید: 520 متر",
		"فاکتور خروج پارچه: 2",
		"پارچه آماده: 14 طاقه، 2,100 متر، 520 کیلو",
		"کسری محاسبه‌شده نخ: 35 کیلو",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("operational report does not contain %q: %s", expected, text)
		}
	}
}

func TestParseAccountingDateSupportsGregorianAndJalali(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "2026-07-27", want: "2026-07-27"},
		{input: "2026-07-27 13:45:00", want: "2026-07-27"},
		{input: "۱۴۰۳/۰۱/۰۱", want: "2024-03-20"},
	}
	for _, test := range tests {
		got, ok := parseAccountingDate(test.input)
		if !ok || got.Format("2006-01-02") != test.want {
			t.Fatalf("parseAccountingDate(%q) = %s, %v; want %s", test.input, got, ok, test.want)
		}
	}
	if _, ok := parseAccountingDate("تاریخ نامعتبر"); ok {
		t.Fatal("invalid date must be rejected")
	}
}

func TestFinancialAccountingItemsOnlyIncludesOperationalSettlements(t *testing.T) {
	state := map[string]any{
		"invoices": []any{
			map[string]any{"number": "OUT-1", "operationalId": 9, "date": "2026-07-27"},
			map[string]any{"number": "MANUAL-1", "date": "2026-07-27"},
		},
		"incomingInvoices": []any{
			map[string]any{"source_type": "operational_yarn_in", "sourceId": 4, "date": "2026-07-27"},
			map[string]any{"source_type": "manual", "sourceId": 5, "date": "2026-07-27"},
		},
	}
	items := financialAccountingItems(state)
	if len(items) != 2 {
		t.Fatalf("unexpected accounting items: %#v", items)
	}
	if items[0].Key != "operational_out_invoice:OUT-1" ||
		items[1].Key != "operational_yarn_in:4" {
		t.Fatalf("unexpected accounting keys: %#v", items)
	}
}

func TestFormatAccountingPerformance(t *testing.T) {
	report := reportSnapshot{
		Company:     "نساجی آزمایشی",
		PeriodStart: "2026-07-21",
		PeriodEnd:   "2026-07-27",
		Accounting: accountingPerformance{
			SLADays:       2,
			Processed:     10,
			Measurable:    8,
			OnTime:        6,
			AverageDelay:  1.5,
			MaxDelay:      5,
			Pending:       4,
			Overdue:       2,
			OldestPending: 7,
			ByUser: []accountingUserPerformance{
				{UserID: 42, Username: "حسابدار یک", Processed: 7, AverageDelay: 1.2, OnTimeRate: 85.7},
			},
		},
	}
	text := formatAccountingReport(report, "weekly")
	for _, expected := range []string{
		"گزارش هفتگی عملکرد حسابداری",
		"10",
		"میانگین زمان رسیدگی: 1.5 روز",
		"75 درصد",
		"حسابدار یک",
		"اسناد گذشته از مهلت: 2",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("accounting report does not contain %q: %s", expected, text)
		}
	}
	mainReport := formatTextileReport(report, "weekly")
	if strings.Contains(mainReport, "عملکرد حسابداری") {
		t.Fatal("main textile report must remain separate from the accounting report")
	}
	alert := formatAccountingOverdueAlert(report)
	if !strings.Contains(alert, "هشدار تأخیر حسابداری") || !strings.Contains(alert, "2") {
		t.Fatalf("unexpected accounting alert: %s", alert)
	}
}

func TestCalculateAccountingPerformance(t *testing.T) {
	location := time.FixedZone("Tehran", 3*60*60+30*60)
	start := time.Date(2026, 7, 21, 0, 0, 0, 0, location)
	end := time.Date(2026, 7, 27, 20, 0, 0, 0, location)
	operational := []operationalAccountingItem{
		{Key: "operational_out_invoice:A", SourceDate: "2026-07-20"},
		{Key: "operational_yarn_in:2", SourceDate: "2026-07-21"},
		{Key: "operational_expense:3", SourceDate: "2026-07-20"},
		{Key: "operational_expense:future", SourceDate: "2026-07-28"},
	}
	financial := []financialAccountingItem{
		{Key: "operational_out_invoice:A", ProcessedAt: "2026-07-22T08:00:00Z", ProcessedBy: 9},
		{Key: "operational_yarn_in:2", ProcessedAt: "2026-07-26T08:00:00Z", ProcessedBy: 9},
	}
	got := calculateAccountingPerformance(operational, financial, start, end, 2)
	if got.Processed != 2 || got.Measurable != 2 || got.OnTime != 1 {
		t.Fatalf("unexpected processed metrics: %#v", got)
	}
	if got.AverageDelay != 3.5 || got.MaxDelay != 5 {
		t.Fatalf("unexpected delay metrics: %#v", got)
	}
	if got.Pending != 1 || got.Overdue != 1 || got.OldestPending != 7 {
		t.Fatalf("unexpected pending metrics: %#v", got)
	}
	if len(got.ByUser) != 1 || got.ByUser[0].UserID != 9 ||
		got.ByUser[0].Processed != 2 || got.ByUser[0].OnTimeRate != 50 {
		t.Fatalf("unexpected accountant metrics: %#v", got.ByUser)
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

func TestConfiguredDoesNotDependOnTemporaryTelegramReachability(t *testing.T) {
	service := New(nil, Config{Enabled: true, BotToken: "123456789:secret-token-value-for-test", BotUsername: "textile_reports_bot"})
	if !service.Configured() {
		t.Fatal("configured bot must allow pairing while the relay reconnects")
	}
	if service.Available() {
		t.Fatal("service must not report ready before bootstrap succeeds")
	}
}

func TestBootstrapValidatesBotAndRemovesWebhook(t *testing.T) {
	const token = "123456:test-secret-token"
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/bot" + token + "/getMe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"is_bot":   true,
					"username": "textile_reports_bot",
				},
			})
		case "/bot" + token + "/deleteWebhook":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode deleteWebhook body: %v", err)
			}
			if drop, ok := body["drop_pending_updates"].(bool); !ok || drop {
				t.Fatalf("drop_pending_updates must explicitly be false: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New(nil, Config{
		Enabled:  true,
		BotToken: token,
		APIBase:  server.URL,
	})
	if err := service.bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if !service.Available() {
		t.Fatal("service must become available after successful bootstrap")
	}
	if service.cfg.BotUsername != "textile_reports_bot" {
		t.Fatalf("username was not populated from getMe: %q", service.cfg.BotUsername)
	}
	expected := []string{
		http.MethodGet + " /bot" + token + "/getMe",
		http.MethodPost + " /bot" + token + "/deleteWebhook",
	}
	if strings.Join(requests, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected Telegram API calls:\n%v", requests)
	}
}

func TestBootstrapRejectsConfiguredUsernameMismatch(t *testing.T) {
	const token = "123456:mismatch-secret-token"
	deleteWebhookCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot" + token + "/getMe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"is_bot":   true,
					"username": "actual_bot",
				},
			})
		case "/bot" + token + "/deleteWebhook":
			deleteWebhookCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New(nil, Config{
		Enabled:     true,
		BotToken:    token,
		BotUsername: "different_bot",
		APIBase:     server.URL,
	})
	err := service.bootstrap(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected username mismatch, got %v", err)
	}
	if service.Available() {
		t.Fatal("service must remain unavailable after username mismatch")
	}
	if deleteWebhookCalled {
		t.Fatal("webhook must not be changed when bot identity does not match")
	}
}

func TestBootstrapUsesSecureRelayWithoutTokenInRequestURL(t *testing.T) {
	const (
		botToken   = "123456789:relay-secret-bot-token-1234567890"
		relayToken = "relay-access-token"
	)
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RequestURI(), botToken) {
			t.Fatalf("bot token leaked in relay request URL: %s", r.URL.RequestURI())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+relayToken {
			t.Fatalf("unexpected relay authorization: %q", got)
		}
		if got := r.Header.Get("X-Telegram-Bot-Token"); got != botToken {
			t.Fatalf("unexpected bot token header: %q", got)
		}
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/telegram/getMe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"is_bot":   true,
					"username": "textile_reports_bot",
				},
			})
		case "/telegram/deleteWebhook":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New(nil, Config{
		Enabled:     true,
		BotToken:    botToken,
		BotUsername: "textile_reports_bot",
		APIBase:     "https://api.telegram.org",
		RelayURL:    server.URL + "/telegram",
		RelayToken:  relayToken,
	})
	service.client = server.Client()
	if err := service.bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap through relay failed: %v", err)
	}
	if !service.Available() {
		t.Fatal("service must become available after relay bootstrap")
	}
	expected := []string{
		http.MethodGet + " /telegram/getMe",
		http.MethodPost + " /telegram/deleteWebhook",
	}
	if strings.Join(requests, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected relay calls:\n%v", requests)
	}
}

func TestTelegramEndpointRejectsInsecureRelay(t *testing.T) {
	service := New(nil, Config{
		BotToken:   "123456789:test-token",
		RelayURL:   "http://relay.example.test/telegram",
		RelayToken: "relay-access-token",
	})
	if _, err := service.telegramEndpoint("getMe", nil); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected insecure relay URL rejection, got %v", err)
	}
}

func TestPollLoopMarksServiceUnavailableAfterPollingFailure(t *testing.T) {
	const token = "123456789:polling-failure-token"
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot"+token+"/getUpdates" {
			http.NotFound(w, r)
			return
		}
		cancel()
		http.Error(w, "temporary upstream failure", http.StatusBadGateway)
	}))
	defer server.Close()

	service := New(nil, Config{Enabled: true, BotToken: token, APIBase: server.URL})
	service.setAvailable(true)
	service.pollLoop(ctx)
	if service.Available() {
		t.Fatal("service must become unavailable after Telegram polling fails")
	}
}

func TestPollOnceUsesRelayCompatibleTimeout(t *testing.T) {
	const token = "123456789:relay-compatible-polling-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot"+token+"/getUpdates" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("timeout"); got != "5" {
			t.Fatalf("expected short relay-compatible timeout, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}})
	}))
	defer server.Close()

	service := New(nil, Config{Enabled: true, BotToken: token, APIBase: server.URL})
	service.client = server.Client()
	if err := service.pollOnce(context.Background()); err != nil {
		t.Fatalf("poll once failed: %v", err)
	}
}

func TestBootstrapRedactsTokenFromTelegramErrors(t *testing.T) {
	const token = "123456:must-never-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"description": "invalid token " + token,
		})
	}))
	defer server.Close()

	service := New(nil, Config{
		Enabled:  true,
		BotToken: token,
		APIBase:  server.URL,
	})
	err := service.bootstrap(context.Background())
	if err == nil {
		t.Fatal("expected Telegram validation failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked in error: %v", err)
	}
}
